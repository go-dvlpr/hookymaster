package app

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/godvlpr/hookymaster/internal/metaimport"
	"github.com/godvlpr/hookymaster/internal/store"
)

// HooksImportResult is the outcome of importing known hook texts.
type HooksImportResult struct {
	Created int      `json:"created"`
	Skipped int      `json:"skipped"`
	Errors  []string `json:"errors"`
}

// ImportHooks stores hook texts from a CSV with ad_name, hook_text,
// on_screen_text. Hooks that exist get their text only when it is empty.
// Used by the seed command.
func (s *Service) ImportHooks(ctx context.Context, r io.Reader) (HooksImportResult, error) {
	cr := csv.NewReader(r)
	header, err := cr.Read()
	if err != nil {
		return HooksImportResult{}, invalidf("Cannot read the file header")
	}
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\xef\xbb\xbf")))] = i
	}
	for _, c := range []string{"ad_name", "hook_text"} {
		if _, ok := idx[c]; !ok {
			return HooksImportResult{}, invalidf("The file has no %q column", c)
		}
	}

	res := HooksImportResult{Errors: []string{}}
	var created []store.Hook
	projects := map[string]*store.Project{}
	for line := 2; ; line++ {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return HooksImportResult{}, invalidf("Line %d cannot be read", line)
		}
		get := func(col string) string {
			if i, ok := idx[col]; ok && i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
			return ""
		}
		name, ok := metaimport.ParseAdName(get("ad_name"))
		text := get("hook_text")
		if !ok || text == "" {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: bad ad name or empty hook", line))
			continue
		}
		p, err := cachedProject(ctx, s.q, projects, name.Original)
		if err != nil {
			return HooksImportResult{}, err
		}
		if p == nil {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: project %s does not exist", line, name.Original))
			continue
		}
		num := int32(name.Hook)
		if _, err := s.q.GetHookByNumber(ctx, store.GetHookByNumberParams{ProjectID: p.ID, Number: &num}); err == nil {
			res.Skipped++
			continue
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return HooksImportResult{}, fmt.Errorf("get hook %s H%d: %w", p.Code, num, err)
		}
		h, err := s.q.CreateHook(ctx, store.CreateHookParams{
			ProjectID: p.ID, Number: &num, Text: text, OnScreenText: get("on_screen_text"),
			Source: "imported", Status: "exported", ExportedAt: ptr(time.Now()),
		})
		if err != nil {
			return HooksImportResult{}, fmt.Errorf("create hook %s H%d: %w", p.Code, num, err)
		}
		created = append(created, h)
		res.Created++
	}
	s.log.Info().Int("created", res.Created).Int("skipped", res.Skipped).Msg("hooks imported")
	s.enrich(ctx, created)
	return res, nil
}

func cachedProject(ctx context.Context, q *store.Queries, cache map[string]*store.Project, code string) (*store.Project, error) {
	if p, ok := cache[code]; ok {
		return p, nil
	}
	p, err := q.GetProjectByCode(ctx, code)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		cache[code] = nil
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("get project %s: %w", code, err)
	}
	cache[code] = &p
	return &p, nil
}

// Unrecognized is an ad name from the file that no hook of the project
// matched. Project is set when the name follows the convention of another
// project code.
type Unrecognized struct {
	AdName     string `json:"adName"`
	Project    string `json:"project,omitempty"`
	SpendCents int64  `json:"spendCents"`
}

// ImportView is the result of the latest Meta import of a project.
type ImportView struct {
	ID                   int64          `json:"id"`
	FileName             string         `json:"fileName"`
	PeriodStart          string         `json:"periodStart"`
	PeriodEnd            string         `json:"periodEnd"`
	CreatedAt            string         `json:"createdAt"`
	RowsTotal            int            `json:"rowsTotal"`
	RowsMatched          int            `json:"rowsMatched"`
	HooksUpdated         int            `json:"hooksUpdated"`
	HooksCreated         int            `json:"hooksCreated"`
	SkippedOtherProjects int            `json:"skippedOtherProjects"`
	SkippedOtherMarkets  int            `json:"skippedOtherMarkets"`
	Unrecognized         []Unrecognized `json:"unrecognized"`
}

func importView(imp store.Import) (ImportView, error) {
	v := ImportView{
		ID: imp.ID, FileName: imp.FileName,
		PeriodStart: imp.PeriodStart.Format(time.DateOnly), PeriodEnd: imp.PeriodEnd.Format(time.DateOnly),
		CreatedAt: imp.CreatedAt.Format(time.RFC3339), RowsTotal: int(imp.RowsTotal), RowsMatched: int(imp.RowsMatched),
		HooksUpdated: int(imp.HooksUpdated), HooksCreated: int(imp.HooksCreated),
		SkippedOtherProjects: int(imp.SkippedOtherProjects), SkippedOtherMarkets: int(imp.SkippedOtherMarkets),
	}
	if err := json.Unmarshal(imp.Unrecognized, &v.Unrecognized); err != nil {
		return ImportView{}, fmt.Errorf("decode unrecognized names: %w", err)
	}
	return v, nil
}

// match is the hook a group of file rows belongs to.
type match struct {
	number int32 // used when the hook has to be created
	hook   *store.Hook
	market string
	counts metaimport.Counts
	rows   int
}

// ImportMeta loads an Ads Manager export into one project. A row belongs to
// the project when its ad name equals the ad name set on one of its hooks,
// or when it follows <market>_<project code>_H<number> of an existing hook.
// Other ad names, including numbers the project does not have, are listed so
// they can be assigned to hooks with AssignAdNames. Results of
// a period that is already stored replace the old ones.
func (s *Service) ImportMeta(ctx context.Context, code, fileName string, r io.Reader) (ImportView, error) {
	p, err := s.project(ctx, code)
	if err != nil {
		return ImportView{}, err
	}
	rep, err := metaimport.Parse(r)
	if err != nil {
		if errors.Is(err, metaimport.ErrInvalidFile) {
			return ImportView{}, fmt.Errorf("%w: %s", ErrInvalid, readableParseError(err))
		}
		return ImportView{}, fmt.Errorf("parse export: %w", err)
	}
	var imp store.Import
	err = s.inTx(ctx, func(q *store.Queries) error {
		imp, err = applyReport(ctx, q, p, fileName, rep, 0)
		return err
	})
	if err != nil {
		return ImportView{}, err
	}
	s.log.Info().Str("project", p.Code).Str("file", fileName).Int32("matched", imp.RowsMatched).
		Int32("created", imp.HooksCreated).Msg("meta export imported")
	return importView(imp)
}

// applyReport matches the parsed file to the hooks of the project, stores
// the results and records the import. created counts hooks the caller has
// already made for this file.
func applyReport(ctx context.Context, q *store.Queries, p store.Project, fileName string, rep metaimport.Report, created int32) (store.Import, error) {
	hooks, err := q.ListProjectHooks(ctx, p.ID)
	if err != nil {
		return store.Import{}, fmt.Errorf("list hooks: %w", err)
	}
	custom := map[string]*store.Hook{}
	for i := range hooks {
		if hooks[i].AdName != nil {
			custom[normalName(*hooks[i].AdName)] = &hooks[i]
		}
	}

	params := store.CreateImportParams{
		ProjectID: p.ID, FileName: fileName, PeriodStart: rep.PeriodStart, PeriodEnd: rep.PeriodEnd, RowsTotal: int32(rep.Rows), HooksCreated: created,
	}
	byHook := map[int64]*match{}   // matched by a custom ad name
	byNumber := map[int32]*match{} // matched by the naming convention
	add := func(m *match, market string, c metaimport.Counts, rows int) {
		m.market = market
		m.counts.Impressions += c.Impressions
		m.counts.SpendCents += c.SpendCents
		m.counts.Plays3s += c.Plays3s
		m.counts.LinkClicks += c.LinkClicks
		m.counts.Purchases += c.Purchases
		m.rows += rows
		params.RowsMatched += int32(rows)
	}
	byCustom := func(name string, c metaimport.Counts, rows int) bool {
		h, ok := custom[normalName(name)]
		if !ok {
			return false
		}
		if byHook[h.ID] == nil {
			byHook[h.ID] = &match{hook: h}
		}
		add(byHook[h.ID], p.Market, c, rows)
		return true
	}
	unrecognized := map[string]*Unrecognized{}
	var order []string
	unknown := func(name, project string, spend int64) {
		key := normalName(name)
		if unrecognized[key] == nil {
			unrecognized[key] = &Unrecognized{AdName: strings.TrimSpace(name), Project: project}
			order = append(order, key)
		}
		unrecognized[key].SpendCents += spend
	}

	for _, t := range rep.Hooks {
		name := AdName(t.Market, t.Original, int32(t.Hook))
		if byCustom(name, t.Counts, t.Rows) {
			continue
		}
		switch {
		case t.Original != p.Code:
			params.SkippedOtherProjects += int32(t.Rows)
			unknown(name, t.Original, t.SpendCents)
		case t.Market != p.Market:
			params.SkippedOtherMarkets += int32(t.Rows)
		default:
			num := int32(t.Hook)
			if byNumber[num] == nil {
				byNumber[num] = &match{number: num}
			}
			add(byNumber[num], t.Market, t.Counts, t.Rows)
		}
	}
	for _, b := range rep.BadNames {
		if byCustom(b.AdName, b.Counts, 1) {
			continue
		}
		unknown(b.AdName, "", b.SpendCents)
		params.SkippedBadNames++
	}
	var all []*match
	for _, m := range byHook {
		all = append(all, m)
	}
	for _, m := range byNumber {
		h, err := q.GetHookByNumber(ctx, store.GetHookByNumberParams{ProjectID: p.ID, Number: &m.number})
		if errors.Is(err, pgx.ErrNoRows) || err == nil && h.DeletedAt != nil {
			// No hook with this number, or it was deleted: the user decides
			// what it is.
			params.RowsMatched -= int32(m.rows)
			unknown(AdName(m.market, p.Code, m.number), p.Code, m.counts.SpendCents)
			continue
		}
		if err != nil {
			return store.Import{}, fmt.Errorf("find hook H%d: %w", m.number, err)
		}
		m.hook = &h
		// A hook matched by its custom name and by the convention gets one sum.
		if prev, ok := byHook[h.ID]; ok {
			add(prev, m.market, m.counts, 0)
			continue
		}
		all = append(all, m)
	}
	params.HooksUpdated = int32(len(all))

	list := make([]Unrecognized, 0, len(order))
	for _, k := range order {
		list = append(list, *unrecognized[k])
	}
	if params.Unrecognized, err = json.Marshal(list); err != nil {
		return store.Import{}, fmt.Errorf("marshal unrecognized names: %w", err)
	}
	if params.Report, err = json.Marshal(rep); err != nil {
		return store.Import{}, fmt.Errorf("marshal report: %w", err)
	}

	imp, err := q.CreateImport(ctx, params)
	if err != nil {
		return store.Import{}, fmt.Errorf("create import: %w", err)
	}
	for _, m := range all {
		if err := q.ReplaceSnapshot(ctx, store.ReplaceSnapshotParams{
			HookID: m.hook.ID, Market: m.market, PeriodStart: rep.PeriodStart, PeriodEnd: rep.PeriodEnd,
			Impressions: m.counts.Impressions, SpendCents: m.counts.SpendCents, Plays3s: m.counts.Plays3s,
			LinkClicks: m.counts.LinkClicks, Purchases: m.counts.Purchases, ImportID: &imp.ID,
		}); err != nil {
			return store.Import{}, fmt.Errorf("store metrics of hook %d: %w", m.hook.ID, err)
		}
	}
	return imp, nil
}

func normalName(name string) string {
	return strings.ToUpper(strings.TrimSpace(name))
}

// Assignment gives an unrecognized ad name a hook. HookID 0 creates a new
// hook without text: with the number from the name when the name follows
// this project's convention and the number is free, otherwise with the next
// number and the ad name stored on the hook.
type Assignment struct {
	AdName string `json:"adName"`
	HookID int64  `json:"hookId"`
}

// AssignAdNames stores ad names of the latest import on hooks and applies
// the same file again, so the results show up without a new upload.
func (s *Service) AssignAdNames(ctx context.Context, code string, items []Assignment) (ImportView, error) {
	if len(items) == 0 {
		return ImportView{}, invalidf("Choose a hook for at least one ad name")
	}
	p, err := s.project(ctx, code)
	if err != nil {
		return ImportView{}, err
	}
	var imp store.Import
	created := 0
	err = s.inTx(ctx, func(q *store.Queries) error {
		last, err := q.LatestImport(ctx, p.ID)
		if err != nil {
			return notFound(err, "import")
		}
		var listed []Unrecognized
		if err := json.Unmarshal(last.Unrecognized, &listed); err != nil {
			return fmt.Errorf("decode unrecognized names: %w", err)
		}
		var rep metaimport.Report
		if err := json.Unmarshal(last.Report, &rep); err != nil || rep.Rows == 0 {
			return invalidf("Upload the file again, this import cannot be reopened")
		}
		names := map[string]string{} // normalized -> as in the file
		for _, u := range listed {
			names[normalName(u.AdName)] = u.AdName
		}

		// New hooks must not take a number the file itself uses for this
		// project, or that hook would get both results.
		next, err := q.MaxHookNumber(ctx, p.ID)
		if err != nil {
			return fmt.Errorf("max hook number: %w", err)
		}
		for _, t := range rep.Hooks {
			if t.Original == p.Code {
				next = max(next, int32(t.Hook))
			}
		}

		seenName := map[string]bool{}
		seenHook := map[int64]bool{}
		for _, it := range items {
			key := normalName(it.AdName)
			name, ok := names[key]
			if !ok {
				return invalidf("%s is not among the unrecognized ad names of the last file", it.AdName)
			}
			if seenName[key] {
				return invalidf("%s is assigned twice", name)
			}
			seenName[key] = true

			if it.HookID == 0 {
				if num, ok := ownFreeNumber(ctx, q, p, name); ok {
					if _, err := q.CreateHook(ctx, store.CreateHookParams{
						ProjectID: p.ID, Number: &num, Source: "file", Status: "exported", ExportedAt: ptr(time.Now()),
					}); err != nil {
						return fmt.Errorf("create hook H%d: %w", num, err)
					}
					created++
					continue
				}
				next++
				h, err := q.CreateHook(ctx, store.CreateHookParams{
					ProjectID: p.ID, Number: ptr(next), Source: "file", Status: "exported", ExportedAt: ptr(time.Now()),
				})
				if err != nil {
					return fmt.Errorf("create hook: %w", err)
				}
				if err := q.SetHookAdName(ctx, store.SetHookAdNameParams{ID: h.ID, AdName: &name}); err != nil {
					return fmt.Errorf("set ad name: %w", err)
				}
				created++
				continue
			}

			if seenHook[it.HookID] {
				return invalidf("One hook can take only one ad name")
			}
			seenHook[it.HookID] = true
			h, err := q.GetHook(ctx, it.HookID)
			if err != nil || h.ProjectID != p.ID || h.Number == nil {
				return invalidf("The chosen hook is not a hook of this project")
			}
			if h.AdName != nil && normalName(*h.AdName) != key {
				return fmt.Errorf("%w: %s already uses the ad name %s. Clear it on the hook first", ErrConflict, hookLabel(h.Number), *h.AdName)
			}
			if err := q.SetHookAdName(ctx, store.SetHookAdNameParams{ID: h.ID, AdName: &name}); err != nil {
				var pgErr *pgconn.PgError
				if errors.As(err, &pgErr) && pgErr.Code == "23505" {
					return fmt.Errorf("%w: Another hook already uses the ad name %s", ErrConflict, name)
				}
				return fmt.Errorf("set ad name: %w", err)
			}
		}

		imp, err = applyReport(ctx, q, p, last.FileName, rep, int32(created))
		return err
	})
	if err != nil {
		return ImportView{}, err
	}
	s.log.Info().Str("project", p.Code).Int("assigned", len(items)).Int("created", created).Msg("ad names assigned")
	return importView(imp)
}

// ownFreeNumber returns the hook number of an ad name that follows the
// convention of this project when no hook has it and it was never deleted.
func ownFreeNumber(ctx context.Context, q *store.Queries, p store.Project, name string) (int32, bool) {
	n, ok := metaimport.ParseAdName(name)
	if !ok || n.Original != p.Code || n.Market != p.Market {
		return 0, false
	}
	num := int32(n.Hook)
	if num <= p.MaxDeletedNumber {
		return 0, false
	}
	_, err := q.GetHookByNumber(ctx, store.GetHookByNumberParams{ProjectID: p.ID, Number: &num})
	return num, errors.Is(err, pgx.ErrNoRows)
}

// readableParseError turns a parser error into a sentence for people.
func readableParseError(err error) string {
	msg := strings.TrimPrefix(err.Error(), metaimport.ErrInvalidFile.Error()+": ")
	switch {
	case strings.HasPrefix(msg, "missing column"):
		return "The file has no " + strings.TrimPrefix(msg, "missing column ") + " column. Export it from Ads Manager with English column names"
	case msg == "no rows":
		return "The file has no data rows"
	case strings.HasPrefix(msg, "read header"):
		return "This does not look like a CSV file"
	default:
		return "The file cannot be read: " + msg
	}
}

// LatestImport returns the most recent Meta import of a project.
func (s *Service) LatestImport(ctx context.Context, code string) (ImportView, error) {
	p, err := s.project(ctx, code)
	if err != nil {
		return ImportView{}, err
	}
	imp, err := s.q.LatestImport(ctx, p.ID)
	if err != nil {
		return ImportView{}, notFound(err, "import")
	}
	return importView(imp)
}
