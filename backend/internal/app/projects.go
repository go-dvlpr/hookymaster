package app

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/godvlpr/hookymaster/internal/generation"
	"github.com/godvlpr/hookymaster/internal/ranking"
	"github.com/godvlpr/hookymaster/internal/store"
)

// codePattern is the middle part of ad names, e.g. O12 in US_O12_H02.
var codePattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{0,11}$`)

const codeRule = "Code must be 1 to 12 letters or digits, starting with a letter, e.g. O12"

// rulesOf turns the project settings into ranking rules.
func rulesOf(p store.Project) ranking.Rules {
	return ranking.Rules{
		MinImpressions: p.MinImpressions,
		MinPurchases:   p.MinPurchases,
		MaxCPACents:    p.MaxCpaCents,
		RefShare:       float64(p.RefSharePct) / 100,
		MinRefs:        int(p.MinRefs),
		MaxRefs:        int(p.MaxRefs),
		Weights: ranking.Weights{
			CPA: int(p.WeightCpa), HookRate: int(p.WeightHookRate), Purchases: int(p.WeightPurchases), CTR: int(p.WeightCtr),
		},
	}
}

// NavProject is a project in the sidebar.
type NavProject struct {
	Code           string `json:"code"`
	Name           string `json:"name"`
	OpenCandidates int    `json:"openCandidates"`
}

// Nav holds the sidebar counters.
type Nav struct {
	ReadyToExport int          `json:"readyToExport"`
	Projects      []NavProject `json:"projects"`
}

// Nav returns the sidebar counters.
func (s *Service) Nav(ctx context.Context) (Nav, error) {
	projects, err := s.q.ListProjects(ctx)
	if err != nil {
		return Nav{}, fmt.Errorf("list projects: %w", err)
	}
	open, err := s.openCandidates(ctx)
	if err != nil {
		return Nav{}, err
	}
	ready, err := s.q.CountSelected(ctx)
	if err != nil {
		return Nav{}, fmt.Errorf("count selected hooks: %w", err)
	}
	nav := Nav{ReadyToExport: int(ready), Projects: []NavProject{}}
	for _, p := range projects {
		nav.Projects = append(nav.Projects, NavProject{Code: p.Code, Name: p.Name, OpenCandidates: open[p.ID]})
	}
	return nav, nil
}

func (s *Service) openCandidates(ctx context.Context) (map[int64]int, error) {
	rows, err := s.q.CountOpenCandidates(ctx)
	if err != nil {
		return nil, fmt.Errorf("count candidates: %w", err)
	}
	out := make(map[int64]int, len(rows))
	for _, r := range rows {
		out[r.ProjectID] = int(r.Open)
	}
	return out, nil
}

func (s *Service) missingText(ctx context.Context) (map[int64]int, error) {
	rows, err := s.q.CountHooksWithoutText(ctx)
	if err != nil {
		return nil, fmt.Errorf("count hooks without text: %w", err)
	}
	out := make(map[int64]int, len(rows))
	for _, r := range rows {
		out[r.ProjectID] = int(r.Missing)
	}
	return out, nil
}

// Tiers counts hooks per tier.
type Tiers struct {
	Reference int `json:"reference"`
	Good      int `json:"good"`
	Anti      int `json:"anti"`
	Early     int `json:"early"`
	NoData    int `json:"nodata"`
}

func (t *Tiers) add(tier ranking.Tier) {
	switch tier {
	case ranking.TierReference:
		t.Reference++
	case ranking.TierGood:
		t.Good++
	case ranking.TierAnti:
		t.Anti++
	case ranking.TierEarly:
		t.Early++
	case ranking.TierNoData:
		t.NoData++
	}
}

func isRanked(t ranking.Tier) bool {
	return t == ranking.TierReference || t == ranking.TierGood || t == ranking.TierAnti
}

// ProjectSummary is a row of the overview table.
type ProjectSummary struct {
	Code           string  `json:"code"`
	Name           string  `json:"name"`
	Tiers          Tiers   `json:"tiers"`
	Hooks          int     `json:"hooks"`
	BestCPACents   *int64  `json:"bestCpaCents"`
	AvgCPACents    *int64  `json:"avgCpaCents"`
	HookRate       float64 `json:"hookRate"`
	OpenCandidates int     `json:"openCandidates"`
	MissingText    int     `json:"missingText"`
}

// BestHook is the hook with the lowest CPA among ranked hooks.
type BestHook struct {
	Code     string `json:"code"`
	Number   string `json:"number"`
	CPACents int64  `json:"cpaCents"`
}

// Overview is the home screen.
type Overview struct {
	OpenCandidates int              `json:"openCandidates"`
	MissingText    int              `json:"missingText"`
	ReadyToExport  int              `json:"readyToExport"`
	SpendCents     int64            `json:"spendCents"`
	AvgCPACents    *int64           `json:"avgCpaCents"`
	References     int              `json:"references"`
	BestHook       *BestHook        `json:"bestHook"`
	Projects       []ProjectSummary `json:"projects"`
}

// Overview returns totals and a summary per project.
func (s *Service) Overview(ctx context.Context) (Overview, error) {
	projects, err := s.q.ListProjects(ctx)
	if err != nil {
		return Overview{}, fmt.Errorf("list projects: %w", err)
	}
	open, err := s.openCandidates(ctx)
	if err != nil {
		return Overview{}, err
	}
	missing, err := s.missingText(ctx)
	if err != nil {
		return Overview{}, err
	}
	ready, err := s.q.CountSelected(ctx)
	if err != nil {
		return Overview{}, fmt.Errorf("count selected hooks: %w", err)
	}

	ov := Overview{ReadyToExport: int(ready), Projects: []ProjectSummary{}}
	var total ranking.Stats
	for _, p := range projects {
		hooks, ranked, err := s.rankProject(ctx, p)
		if err != nil {
			return Overview{}, err
		}
		sum := ProjectSummary{Code: p.Code, Name: p.Name, OpenCandidates: open[p.ID], MissingText: missing[p.ID]}
		for _, h := range hooks {
			if h.Number != nil {
				sum.Hooks++
			}
		}
		var ps ranking.Stats
		for _, r := range ranked {
			sum.Tiers.add(r.Tier)
			addStats(&ps, r.Stats)
			if r.Tier == ranking.TierReference {
				ov.References++
			}
			if !isRanked(r.Tier) {
				continue
			}
			cpa := cpaOf(r)
			if sum.BestCPACents == nil || cpa < *sum.BestCPACents {
				sum.BestCPACents = ptr(cpa)
			}
			if ov.BestHook == nil || cpa < ov.BestHook.CPACents {
				ov.BestHook = &BestHook{Code: p.Code, Number: r.number, CPACents: cpa}
			}
		}
		if cpa, ok := ps.CPACents(); ok {
			sum.AvgCPACents = ptr(cpa)
		}
		sum.HookRate = ps.HookRate()
		ov.OpenCandidates += sum.OpenCandidates
		ov.MissingText += sum.MissingText
		addStats(&total, ps)
		ov.Projects = append(ov.Projects, sum)
	}
	ov.SpendCents = total.SpendCents
	if cpa, ok := total.CPACents(); ok {
		ov.AvgCPACents = ptr(cpa)
	}
	return ov, nil
}

func addStats(dst *ranking.Stats, s ranking.Stats) {
	dst.Impressions += s.Impressions
	dst.SpendCents += s.SpendCents
	dst.Plays3s += s.Plays3s
	dst.LinkClicks += s.LinkClicks
	dst.Purchases += s.Purchases
}

// rankedHook is a ranking result with the hook it belongs to.
type rankedHook struct {
	ranking.Result
	hook   store.Hook
	number string
}

func cpaOf(r rankedHook) int64 {
	cpa, _ := r.CPACents()
	return cpa
}

// rankProject returns all hooks of a project and the ranking of the ones
// with results in its market.
func (s *Service) rankProject(ctx context.Context, p store.Project) ([]store.Hook, []rankedHook, error) {
	hooks, err := s.q.ListProjectHooks(ctx, p.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("list hooks of %s: %w", p.Code, err)
	}
	metrics, err := s.q.ProjectHookMetrics(ctx, store.ProjectHookMetricsParams{ProjectID: p.ID, Market: p.Market})
	if err != nil {
		return nil, nil, fmt.Errorf("load metrics of %s: %w", p.Code, err)
	}
	byID := make(map[int64]store.Hook, len(hooks))
	for _, h := range hooks {
		byID[h.ID] = h
	}
	stats := make([]ranking.Stats, 0, len(metrics))
	for _, m := range metrics {
		stats = append(stats, ranking.Stats{
			HookID: m.HookID, Impressions: m.Impressions, SpendCents: m.SpendCents,
			Plays3s: m.Plays3s, LinkClicks: m.LinkClicks, Purchases: m.Purchases,
			Group: deref(byID[m.HookID].HookType),
		})
	}
	results := ranking.Rank(stats, rulesOf(p))
	out := make([]rankedHook, len(results))
	for i, r := range results {
		h := byID[r.HookID]
		out[i] = rankedHook{Result: r, hook: h, number: hookLabel(h.Number)}
	}
	return hooks, out, nil
}

func (s *Service) project(ctx context.Context, code string) (store.Project, error) {
	p, err := s.q.GetProjectByCode(ctx, strings.ToUpper(strings.TrimSpace(code)))
	if err != nil {
		return store.Project{}, notFound(err, "project "+code)
	}
	return p, nil
}

// CreateProjectInput is a new project. Code is optional; the next free one
// is used when empty.
type CreateProjectInput struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Buyer       string `json:"buyer"`
}

// maxBuyer limits the buyer text of a project.
const maxBuyer = 1000

func (in *CreateProjectInput) validate() error {
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.Buyer = strings.TrimSpace(in.Buyer)
	if in.Name == "" {
		return invalidf("Enter a project name")
	}
	if utf8.RuneCountInString(in.Name) > 80 {
		return invalidf("Project name must be at most 80 characters")
	}
	if utf8.RuneCountInString(in.Description) > 3000 {
		return invalidf("Description must be at most 3000 characters")
	}
	if utf8.RuneCountInString(in.Buyer) > maxBuyer {
		return invalidf("Buyer must be at most %d characters", maxBuyer)
	}
	if in.Code != "" && !codePattern.MatchString(in.Code) {
		return invalidf("%s", codeRule)
	}
	return nil
}

// NextCode returns the code after the highest existing one.
func NextCode(existing []string) string {
	highest := 0
	for _, c := range existing {
		if n, err := strconv.Atoi(strings.TrimPrefix(c, "O")); err == nil && n > highest {
			highest = n
		}
	}
	return fmt.Sprintf("O%d", highest+1)
}

// CreateProject stores a project and returns its code.
func (s *Service) CreateProject(ctx context.Context, in CreateProjectInput) (string, error) {
	if err := in.validate(); err != nil {
		return "", err
	}
	err := s.inTx(ctx, func(q *store.Queries) error {
		if in.Code == "" {
			projects, err := q.ListProjects(ctx)
			if err != nil {
				return fmt.Errorf("list projects: %w", err)
			}
			codes := make([]string, len(projects))
			for i, p := range projects {
				codes[i] = p.Code
			}
			in.Code = NextCode(codes)
		} else if _, err := q.GetProjectByCode(ctx, in.Code); err == nil {
			return fmt.Errorf("%w: Project %s already exists", ErrConflict, in.Code)
		}
		if _, err := q.CreateProject(ctx, store.CreateProjectParams{Code: in.Code, Name: in.Name, Description: in.Description, Buyer: in.Buyer}); err != nil {
			return fmt.Errorf("create project: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	s.log.Info().Str("project", in.Code).Msg("project created")
	return in.Code, nil
}

// Settings are the editable fields of a project.
type Settings struct {
	Code           string `json:"code"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Buyer          string `json:"buyer"`
	MinImpressions int64  `json:"minImpressions"`
	MinPurchases   int64  `json:"minPurchases"`
	MaxCPACents    int64  `json:"maxCpaCents"`
	RefSharePct    int    `json:"refSharePct"`
	MinRefs        int    `json:"minRefs"`
	MaxRefs        int    `json:"maxRefs"`
	MaxAnti        int    `json:"maxAnti"`
	// Weights of the reference score in percent, adding up to 100.
	WeightCPA       int `json:"weightCpa"`
	WeightHookRate  int `json:"weightHookRate"`
	WeightPurchases int `json:"weightPurchases"`
	WeightCTR       int `json:"weightCtr"`
}

func settingsOf(p store.Project) Settings {
	return Settings{
		Code: p.Code, Name: p.Name, Description: p.Description, Buyer: p.Buyer, MinImpressions: p.MinImpressions, MinPurchases: p.MinPurchases,
		MaxCPACents: p.MaxCpaCents, RefSharePct: int(p.RefSharePct), MinRefs: int(p.MinRefs),
		MaxRefs: int(p.MaxRefs), MaxAnti: int(p.MaxAnti),
		WeightCPA: int(p.WeightCpa), WeightHookRate: int(p.WeightHookRate),
		WeightPurchases: int(p.WeightPurchases), WeightCTR: int(p.WeightCtr),
	}
}

func (st *Settings) validate() error {
	st.Code = strings.ToUpper(strings.TrimSpace(st.Code))
	st.Name = strings.TrimSpace(st.Name)
	st.Description = strings.TrimSpace(st.Description)
	st.Buyer = strings.TrimSpace(st.Buyer)
	switch {
	case !codePattern.MatchString(st.Code):
		return invalidf("%s", codeRule)
	case st.Name == "" || utf8.RuneCountInString(st.Name) > 80:
		return invalidf("Project name must be 1 to 80 characters")
	case utf8.RuneCountInString(st.Description) > 3000:
		return invalidf("Description must be at most 3000 characters")
	case utf8.RuneCountInString(st.Buyer) > maxBuyer:
		return invalidf("Buyer must be at most %d characters", maxBuyer)
	case st.MinImpressions < 0 || st.MinPurchases < 0:
		return invalidf("Minimums cannot be negative")
	case st.MaxCPACents <= 0:
		return invalidf("Max CPA must be above $0")
	case st.RefSharePct < 1 || st.RefSharePct > 100:
		return invalidf("Reference share must be between 1%% and 100%%")
	case st.MinRefs < 0 || st.MaxRefs < 1 || st.MinRefs > st.MaxRefs:
		return invalidf("Min references must be at most max references, and max at least 1")
	case st.MaxRefs > 50 || st.MaxAnti < 0 || st.MaxAnti > 20:
		return invalidf("Use at most 50 references and 20 anti-examples")
	case min(st.WeightCPA, st.WeightHookRate, st.WeightPurchases, st.WeightCTR) < 0:
		return invalidf("Weights cannot be negative")
	case st.WeightCPA+st.WeightHookRate+st.WeightPurchases+st.WeightCTR != 100:
		return invalidf("Weights add up to %d%%, they must add up to 100%%", st.WeightCPA+st.WeightHookRate+st.WeightPurchases+st.WeightCTR)
	}
	return nil
}

// UpdateSettings saves the project settings.
func (s *Service) UpdateSettings(ctx context.Context, code string, st Settings) (Settings, error) {
	if err := st.validate(); err != nil {
		return Settings{}, err
	}
	p, err := s.project(ctx, code)
	if err != nil {
		return Settings{}, err
	}
	p, err = s.q.UpdateProjectSettings(ctx, store.UpdateProjectSettingsParams{
		ID: p.ID, Name: st.Name, Description: st.Description, MinImpressions: st.MinImpressions,
		MinPurchases: st.MinPurchases, MaxCpaCents: st.MaxCPACents, RefSharePct: int32(st.RefSharePct),
		MinRefs: int32(st.MinRefs), MaxRefs: int32(st.MaxRefs), MaxAnti: int32(st.MaxAnti), Code: st.Code,
		WeightCpa: int32(st.WeightCPA), WeightHookRate: int32(st.WeightHookRate),
		WeightPurchases: int32(st.WeightPurchases), WeightCtr: int32(st.WeightCTR), Buyer: st.Buyer,
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return Settings{}, fmt.Errorf("%w: Project %s already exists", ErrConflict, st.Code)
	}
	if err != nil {
		return Settings{}, fmt.Errorf("update settings: %w", err)
	}
	s.log.Info().Str("project", p.Code).Msg("settings updated")
	return settingsOf(p), nil
}

// HookView is a row of the project hooks table.
type HookView struct {
	ID          int64   `json:"id"`
	Number      string  `json:"number"`
	AdName      string  `json:"adName"`
	Text        string  `json:"text"`
	OnScreen    string  `json:"onScreenText"`
	Type        string  `json:"type"`
	Tier        string  `json:"tier"` // "pending" for hooks without results
	Reason      string  `json:"reason"`
	Impressions int64   `json:"impressions"`
	HookRate    float64 `json:"hookRate"`
	Purchases   int64   `json:"purchases"`
	CPACents    *int64  `json:"cpaCents"`
}

// TypeStat is CPA of one hook type over ranked hooks.
type TypeStat struct {
	Type     string `json:"type"`
	CPACents int64  `json:"cpaCents"`
}

// ProjectView is the project screen.
type ProjectView struct {
	Code           string     `json:"code"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	Market         string     `json:"market"`
	AdNamePattern  string     `json:"adNamePattern"`
	Settings       Settings   `json:"settings"`
	SpendCents     int64      `json:"spendCents"`
	AvgCPACents    *int64     `json:"avgCpaCents"`
	HookRate       float64    `json:"hookRate"`
	Ranked         int        `json:"ranked"`
	Total          int        `json:"total"`
	OpenCandidates int        `json:"openCandidates"`
	Hooks          []HookView `json:"hooks"`
	Types          []TypeStat `json:"types"`
}

// Project returns the hooks screen of a project.
func (s *Service) Project(ctx context.Context, code string) (ProjectView, error) {
	p, err := s.project(ctx, code)
	if err != nil {
		return ProjectView{}, err
	}
	hooks, ranked, err := s.rankProject(ctx, p)
	if err != nil {
		return ProjectView{}, err
	}
	open, err := s.openCandidates(ctx)
	if err != nil {
		return ProjectView{}, err
	}

	v := ProjectView{
		Code: p.Code, Name: p.Name, Description: p.Description, Market: p.Market,
		AdNamePattern: fmt.Sprintf("%s_%s_H01", p.Market, p.Code), Settings: settingsOf(p),
		OpenCandidates: open[p.ID], Hooks: []HookView{}, Types: []TypeStat{},
	}
	var total ranking.Stats
	byType := map[string]*ranking.Stats{}
	withResults := map[int64]bool{}
	for _, r := range ranked {
		withResults[r.HookID] = true
		addStats(&total, r.Stats)
		row := HookView{
			ID: r.HookID, Number: r.number, AdName: adNameOf(r.hook, p), Text: r.hook.Text, OnScreen: r.hook.OnScreenText,
			Type: generation.TypeName(deref(r.hook.HookType)), Tier: string(r.Tier), Reason: r.Reason,
			Impressions: r.Impressions, HookRate: r.HookRate(), Purchases: r.Purchases,
		}
		if cpa, ok := r.CPACents(); ok {
			row.CPACents = ptr(cpa)
		}
		v.Hooks = append(v.Hooks, row)
		if !isRanked(r.Tier) {
			continue
		}
		v.Ranked++
		if t := deref(r.hook.HookType); t != "" {
			if byType[t] == nil {
				byType[t] = &ranking.Stats{}
			}
			addStats(byType[t], r.Stats)
		}
	}
	// Hooks with an ad name but no results yet go last.
	sort.SliceStable(hooks, func(i, j int) bool { return numberOf(hooks[i]) < numberOf(hooks[j]) })
	for _, h := range hooks {
		if h.Number == nil || withResults[h.ID] {
			continue
		}
		v.Hooks = append(v.Hooks, HookView{
			ID: h.ID, Number: hookLabel(h.Number), AdName: adNameOf(h, p), Text: h.Text, OnScreen: h.OnScreenText,
			Type: generation.TypeName(deref(h.HookType)), Tier: "pending", Reason: "No results yet",
		})
	}
	v.Total = len(v.Hooks)
	v.SpendCents = total.SpendCents
	v.HookRate = total.HookRate()
	if cpa, ok := total.CPACents(); ok {
		v.AvgCPACents = ptr(cpa)
	}
	for t, st := range byType {
		cpa, _ := st.CPACents()
		v.Types = append(v.Types, TypeStat{Type: generation.TypeName(t), CPACents: cpa})
	}
	sort.Slice(v.Types, func(i, j int) bool { return v.Types[i].CPACents < v.Types[j].CPACents })
	return v, nil
}

func numberOf(h store.Hook) int32 {
	if h.Number == nil {
		return 0
	}
	return *h.Number
}

// HookInput is the text of a hook added or edited by hand.
type HookInput struct {
	Text         string `json:"text"`
	OnScreenText string `json:"onScreenText"`
	AdName       string `json:"adName"` // optional: the name of the ad in Meta when it differs
}

func (in *HookInput) validate() error {
	in.Text = strings.TrimSpace(in.Text)
	in.OnScreenText = strings.TrimSpace(in.OnScreenText)
	switch {
	case in.Text == "":
		return invalidf("Enter the hook text")
	case utf8.RuneCountInString(in.Text) > 300:
		return invalidf("Hook must be at most 300 characters")
	case utf8.RuneCountInString(in.OnScreenText) > 120:
		return invalidf("On-screen text must be at most 120 characters")
	}
	in.AdName = strings.TrimSpace(in.AdName)
	if utf8.RuneCountInString(in.AdName) > 200 {
		return invalidf("Ad name must be at most 200 characters")
	}
	return nil
}

// adNameOf is the name of the hook's ad in Meta.
func adNameOf(h store.Hook, p store.Project) string {
	if h.AdName != nil {
		return *h.AdName
	}
	if h.Number == nil {
		return ""
	}
	return AdName(p.Market, p.Code, *h.Number)
}

// AddHook stores a hook that already runs in Meta under the next H-number.
func (s *Service) AddHook(ctx context.Context, code string, in HookInput) (string, error) {
	if err := in.validate(); err != nil {
		return "", err
	}
	p, err := s.project(ctx, code)
	if err != nil {
		return "", err
	}
	var h store.Hook
	err = s.inTx(ctx, func(q *store.Queries) error {
		maxNum, err := q.MaxHookNumber(ctx, p.ID)
		if err != nil {
			return fmt.Errorf("max hook number: %w", err)
		}
		h, err = q.CreateHook(ctx, store.CreateHookParams{
			ProjectID: p.ID, Number: ptr(maxNum + 1), Text: in.Text, OnScreenText: in.OnScreenText,
			Source: "manual", Status: "exported", ExportedAt: ptr(time.Now()),
		})
		if err != nil {
			return fmt.Errorf("create hook: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	s.enrich(ctx, []store.Hook{h})
	return AdName(p.Market, p.Code, *h.Number), nil
}

// UpdateHook changes the text and, if needed, the Meta ad name of a hook.
// An ad name equal to the standard one is stored as no override.
func (s *Service) UpdateHook(ctx context.Context, id int64, in HookInput) error {
	if err := in.validate(); err != nil {
		return err
	}
	h, err := s.q.GetHook(ctx, id)
	if err != nil {
		return notFound(err, "hook")
	}
	p, err := s.q.GetProject(ctx, h.ProjectID)
	if err != nil {
		return notFound(err, "project")
	}
	var custom *string
	if h.Number != nil && in.AdName != "" && !strings.EqualFold(in.AdName, AdName(p.Market, p.Code, *h.Number)) {
		custom = &in.AdName
	}
	h, err = s.q.UpdateHookText(ctx, store.UpdateHookTextParams{ID: id, Text: in.Text, OnScreenText: in.OnScreenText, AdName: custom})
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("%w: hook was not found", ErrNotFound)
	case errors.As(err, &pgErr) && pgErr.Code == "23505":
		return fmt.Errorf("%w: Another hook in this project already uses the ad name %s", ErrConflict, in.AdName)
	case err != nil:
		return fmt.Errorf("update hook: %w", err)
	}
	s.enrich(ctx, []store.Hook{h})
	return nil
}

// DeleteProject removes a project with its hooks, batches, imports and
// results.
func (s *Service) DeleteProject(ctx context.Context, code string) error {
	p, err := s.project(ctx, code)
	if err != nil {
		return err
	}
	if _, err := s.q.DeleteProject(ctx, p.ID); err != nil {
		return fmt.Errorf("delete project %s: %w", p.Code, err)
	}
	s.log.Info().Str("project", p.Code).Msg("project deleted")
	return nil
}

// DeleteHook hides a hook with its results. The hook stays in the database,
// so new generations do not repeat it, and its H-number is never given out
// again.
func (s *Service) DeleteHook(ctx context.Context, id int64) error {
	err := s.inTx(ctx, func(q *store.Queries) error {
		_, err := deleteHook(ctx, q, id)
		return err
	})
	if err != nil {
		return err
	}
	s.log.Info().Int64("hook", id).Msg("hook deleted")
	return nil
}

// maxDeleteHooks caps one bulk delete.
const maxDeleteHooks = 500

// DeleteHooks deletes several hooks of one project at once: all of them or
// none.
func (s *Service) DeleteHooks(ctx context.Context, code string, ids []int64) error {
	if len(ids) == 0 {
		return invalidf("Choose at least one hook")
	}
	if len(ids) > maxDeleteHooks {
		return invalidf("Delete at most %d hooks at once", maxDeleteHooks)
	}
	p, err := s.project(ctx, code)
	if err != nil {
		return err
	}
	err = s.inTx(ctx, func(q *store.Queries) error {
		for _, id := range ids {
			projectID, err := deleteHook(ctx, q, id)
			if err != nil {
				return err
			}
			if projectID != p.ID {
				return fmt.Errorf("%w: hook was not found", ErrNotFound)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.log.Info().Str("project", p.Code).Int("hooks", len(ids)).Msg("hooks deleted")
	return nil
}

// deleteHook marks one hook as deleted and keeps its number from being given
// out again. It returns the project of the hook.
func deleteHook(ctx context.Context, q *store.Queries, id int64) (int64, error) {
	h, err := q.DeleteHook(ctx, id)
	if err != nil {
		return 0, notFound(err, "hook")
	}
	if h.Number == nil {
		return h.ProjectID, nil
	}
	if err := q.RememberDeletedNumber(ctx, store.RememberDeletedNumberParams{ID: h.ProjectID, MaxDeletedNumber: *h.Number}); err != nil {
		return 0, fmt.Errorf("remember deleted number: %w", err)
	}
	return h.ProjectID, nil
}
