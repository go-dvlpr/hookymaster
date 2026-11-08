package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pgvector/pgvector-go"
	"golang.org/x/sync/errgroup"

	"github.com/godvlpr/hookymaster/internal/generation"
	"github.com/godvlpr/hookymaster/internal/ranking"
	"github.com/godvlpr/hookymaster/internal/store"
)

const (
	maxBatch          = 100
	generateRounds    = 2   // the first request plus one top-up for a short batch
	chunkSize         = 25  // candidates per model call; bigger answers get slow
	vectorDupMinScore = 0.9 // cosine similarity that counts as the same idea
)

// GenerateInput are the batch settings from the Generate screen.
type GenerateInput struct {
	N          int            `json:"n"`
	ExploitPct int            `json:"exploitPct"` // share built on existing hooks; the rest is new
	TypeShares map[string]int `json:"typeShares"` // optional percent of the batch per type code
	Brief      string         `json:"brief"`      // what the batch should be about; required without example hooks
}

func (in *GenerateInput) validate() error {
	if in.N < 1 || in.N > maxBatch {
		return invalidf("Batch size must be between 1 and %d", maxBatch)
	}
	if in.ExploitPct < 0 || in.ExploitPct > 100 {
		return invalidf("The split must be between 0%% and 100%%")
	}
	total := 0
	for t, pct := range in.TypeShares {
		if generation.TypeName(t) == t {
			return invalidf("Unknown hook type %q", t)
		}
		if pct < 0 || pct > 100 {
			return invalidf("Type shares must be between 0%% and 100%%")
		}
		total += pct
	}
	if total > 100 {
		return invalidf("Type shares add up to %d%%, they must be 100%% or less", total)
	}
	in.Brief = strings.TrimSpace(in.Brief)
	if len(in.Brief) > 1000 {
		return invalidf("The brief must be at most 1000 characters")
	}
	return nil
}

// Reference is a hook the next batch builds on or avoids.
type Reference struct {
	Number   string `json:"number"`
	Text     string `json:"text"`
	CPACents int64  `json:"cpaCents"`
}

// GenerateOptions is what the Generate screen shows before the user clicks.
type GenerateOptions struct {
	References   []Reference        `json:"references"`
	AntiExamples []Reference        `json:"antiExamples"`
	Types        []generation.Label `json:"types"`
	Untried      []string           `json:"untried"`
	Settings     Settings           `json:"settings"`
	LLMReady     bool               `json:"llmReady"`
	NeedsBrief   bool               `json:"needsBrief"` // no hooks with text to build on yet
}

// GenerateOptions returns what the next batch will be built on.
func (s *Service) GenerateOptions(ctx context.Context, code string) (GenerateOptions, error) {
	p, err := s.project(ctx, code)
	if err != nil {
		return GenerateOptions{}, err
	}
	ex, err := s.examples(ctx, p)
	if err != nil {
		return GenerateOptions{}, err
	}
	opts := GenerateOptions{
		References: []Reference{}, AntiExamples: []Reference{}, Types: generation.Types,
		Untried: ex.untried, Settings: settingsOf(p), LLMReady: s.llm != nil, NeedsBrief: needsBrief(p, ex),
	}
	for _, r := range ex.refs {
		opts.References = append(opts.References, Reference{Number: r.ID, Text: r.Text, CPACents: r.cpaCents})
	}
	for _, a := range ex.antis {
		opts.AntiExamples = append(opts.AntiExamples, Reference{Number: a.ID, Text: a.Text, CPACents: a.cpaCents})
	}
	return opts, nil
}

type example struct {
	generation.Example
	cpaCents int64
}

type examples struct {
	refs, antis []example
	untried     []string
	existing    []string
}

// needsBrief reports whether the model has nothing to go on without a brief:
// no project description and no hooks with text.
func needsBrief(p store.Project, ex examples) bool {
	return p.Description == "" && len(ex.refs) == 0
}

// examples picks references and anti-examples by the project settings.
// Hooks without text cannot guide the model and are skipped. Without ranked
// hooks, any hooks with text serve as references.
func (s *Service) examples(ctx context.Context, p store.Project) (examples, error) {
	hooks, ranked, err := s.rankProject(ctx, p)
	if err != nil {
		return examples{}, err
	}
	toExample := func(r rankedHook) example {
		return example{Example: generation.Example{
			ID: r.number, Text: r.hook.Text, OnScreen: r.hook.OnScreenText, Type: deref(r.hook.HookType),
			CPA: fmt.Sprintf("$%.2f", float64(cpaOf(r))/100), HookRate: fmt.Sprintf("%.1f%%", r.HookRate()*100),
		}, cpaCents: cpaOf(r)}
	}

	var ex examples
	tried := map[string]bool{}
	var antis []rankedHook
	for _, r := range ranked {
		if r.hook.Text == "" {
			continue
		}
		switch r.Tier {
		case ranking.TierReference:
			ex.refs = append(ex.refs, toExample(r))
		case ranking.TierAnti:
			antis = append(antis, r)
		}
		if isRanked(r.Tier) && r.hook.HookType != nil {
			tried[*r.hook.HookType] = true
		}
	}
	sort.Slice(antis, func(i, j int) bool { return cpaOf(antis[i]) > cpaOf(antis[j]) })
	for i, a := range antis {
		if i >= int(p.MaxAnti) {
			break
		}
		e := toExample(a)
		if a.AttentionNoSales {
			e.Why = "stops the scroll (high hook rate) but does not sell: attention bait"
		}
		ex.antis = append(ex.antis, e)
	}

	for _, h := range hooks {
		if h.Text != "" {
			ex.existing = append(ex.existing, h.Text)
		}
	}
	// Deleted hooks are hidden but still must not come back.
	deleted, err := s.q.ListDeletedHookTexts(ctx, p.ID)
	if err != nil {
		return examples{}, fmt.Errorf("list deleted hooks: %w", err)
	}
	ex.existing = append(ex.existing, deleted...)
	if len(ex.refs) == 0 {
		// Cold start: no ranked hooks yet, build on the hooks we know.
		for _, h := range hooks {
			if h.Number != nil && h.Text != "" && len(ex.refs) < int(p.MaxRefs) {
				ex.refs = append(ex.refs, example{Example: generation.Example{
					ID: hookLabel(h.Number), Text: h.Text, OnScreen: h.OnScreenText, Type: deref(h.HookType),
				}})
			}
		}
	}
	ex.untried = []string{}
	for _, t := range generation.Types {
		if !tried[t.Code] {
			ex.untried = append(ex.untried, t.Code)
		}
	}
	return ex, nil
}

// Generate asks the model for a batch of candidates and stores the ones
// that pass validation and the duplicate filter.
func (s *Service) Generate(ctx context.Context, code string, in GenerateInput) (CandidatesView, error) {
	if s.llm == nil {
		return CandidatesView{}, ErrNoModel
	}
	if err := in.validate(); err != nil {
		return CandidatesView{}, err
	}
	p, err := s.project(ctx, code)
	if err != nil {
		return CandidatesView{}, err
	}
	ex, err := s.examples(ctx, p)
	if err != nil {
		return CandidatesView{}, err
	}
	if needsBrief(p, ex) && in.Brief == "" {
		return CandidatesView{}, invalidf("This project has no description and no hooks with text yet. Write a brief: what you sell, to whom and which angle the hooks should take")
	}

	input := generation.Input{
		Project:  generation.Project{Name: p.Name, Description: p.Description, Buyer: p.Buyer},
		Existing: ex.existing,
		Brief:    in.Brief,
	}
	var refIDs []string
	for _, r := range ex.refs {
		input.References = append(input.References, r.Example)
		refIDs = append(refIDs, r.ID)
	}
	for _, a := range ex.antis {
		input.AntiExamples = append(input.AntiExamples, a.Example)
	}

	// New hooks use the types the user asked for, else the untried ones.
	exploreTypes := ex.untried
	if len(in.TypeShares) > 0 {
		input.TypeTargets = map[string]int{}
		exploreTypes = nil
		for _, t := range generation.Types {
			if pct := in.TypeShares[t.Code]; pct > 0 {
				input.TypeTargets[t.Code] = (in.N*pct + 50) / 100
				exploreTypes = append(exploreTypes, t.Code)
			}
		}
	}
	if len(exploreTypes) == 0 {
		exploreTypes = codesOf(generation.Types)
	}

	// Too long hooks and duplicates make a batch come back short; one more
	// request asks for the missing ones.
	start := time.Now()
	var (
		kept     []generation.Candidate
		vecs     [][]float32 // per kept candidate; nil without an embedding
		dropped  []generation.Duplicate
		rejected []generation.Rejected
	)
	existing := ex.existing
	for round := 0; round < generateRounds && len(kept) < in.N; round++ {
		missing := in.N - len(kept)
		input.Slots = generation.Plan(missing, in.ExploitPct, refIDs, exploreTypes)
		input.Existing = existing
		cands, rej, err := s.writeCandidates(ctx, input, missing)
		if err != nil {
			return CandidatesView{}, err
		}
		rejected = append(rejected, rej...)
		fresh, dup := generation.FilterDuplicates(cands, existing)
		fresh, freshVecs, similar := s.dropSimilar(ctx, p.ID, fresh)
		dropped = append(append(dropped, dup...), similar...)
		for i, c := range fresh {
			kept = append(kept, c)
			existing = append(existing, c.Text)
			if freshVecs != nil {
				vecs = append(vecs, freshVecs[i])
			} else {
				vecs = append(vecs, nil)
			}
		}
	}
	kept, vecs = kept[:min(len(kept), in.N)], vecs[:min(len(vecs), in.N)]

	reasons := map[string]int{}
	for _, r := range rejected {
		reasons[r.Reason]++
	}
	s.log.Info().Str("project", p.Code).Int("requested", in.N).Int("kept", len(kept)).
		Int("duplicates", len(dropped)).Int("invalid", len(rejected)).Interface("invalid_reasons", reasons).
		Dur("took", time.Since(start)).Msg("batch generated")

	if dropped == nil {
		dropped = []generation.Duplicate{}
	}
	droppedJSON, err := json.Marshal(dropped)
	if err != nil {
		return CandidatesView{}, fmt.Errorf("marshal dropped: %w", err)
	}
	types := make([]string, 0, len(exploreTypes))
	types = append(types, exploreTypes...)
	var unlabeled []generation.HookText // candidates whose labels the model got wrong
	err = s.inTx(ctx, func(q *store.Queries) error {
		b, err := q.CreateBatch(ctx, store.CreateBatchParams{
			ProjectID: p.ID, Requested: int32(in.N), ExploitShare: int32(in.ExploitPct),
			ExploreTypes: types, Dropped: droppedJSON, Model: s.llm.Model(),
		})
		if err != nil {
			return fmt.Errorf("create batch: %w", err)
		}
		for i, c := range kept {
			params := store.CreateHookParams{
				ProjectID: p.ID, Text: c.Text, OnScreenText: c.OnScreenText, Source: "generated", Status: "candidate",
				HookType: label(c.Type), Pain: label(c.Pain), Emotion: label(c.Emotion), Mode: ptr(string(c.Mode)),
				Rationale: c.Rationale, BatchID: &b.ID,
			}
			if c.Mode == generation.ModeExploit && c.BasedOn != "" {
				params.BasedOn = ptr(c.BasedOn)
			}
			h, err := q.CreateHook(ctx, params)
			if err != nil {
				return fmt.Errorf("store candidate: %w", err)
			}
			if h.HookType == nil {
				unlabeled = append(unlabeled, generation.HookText{ID: h.ID, Text: h.Text})
			}
			if vecs[i] != nil {
				v := pgvector.NewVector(vecs[i])
				if err := q.SetHookEmbedding(ctx, store.SetHookEmbeddingParams{ID: h.ID, Embedding: &v}); err != nil {
					return fmt.Errorf("store candidate embedding: %w", err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return CandidatesView{}, err
	}
	if err := s.classify(ctx, unlabeled); err != nil {
		s.log.Warn().Err(err).Int("hooks", len(unlabeled)).Msg("label candidates")
	}
	return s.Candidates(ctx, p.Code)
}

// label stores an empty label as NULL, so the hook is labeled later.
func label(code string) *string {
	if code == "" {
		return nil
	}
	return &code
}

// writeCandidates asks the model for the planned candidates. A large plan is
// split into chunks that run in parallel; a failed chunk fails the batch.
func (s *Service) writeCandidates(ctx context.Context, input generation.Input, n int) ([]generation.Candidate, []generation.Rejected, error) {
	chunks := generation.Chunk(input.Slots, chunkSize)
	results := make([][]generation.Candidate, len(chunks))
	rejects := make([][]generation.Rejected, len(chunks))
	g, gctx := errgroup.WithContext(ctx)
	for i, slots := range chunks {
		part := input
		part.Slots = slots
		part.TypeTargets = scaleTargets(input.TypeTargets, generation.Count(slots), n)
		g.Go(func() error {
			system, user, err := generation.BuildPrompt(part)
			if err != nil {
				return err
			}
			raw, err := s.llm.JSON(gctx, system, user)
			if err != nil {
				return fmt.Errorf("generate hooks: %w", err)
			}
			results[i], rejects[i], err = generation.ParseCandidates(raw)
			if err != nil {
				return fmt.Errorf("generate hooks: %w", err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}
	var cands []generation.Candidate
	var rejected []generation.Rejected
	for i := range chunks {
		cands = append(cands, results[i]...)
		rejected = append(rejected, rejects[i]...)
	}
	return cands, rejected, nil
}

// scaleTargets gives a chunk of part candidates its share of the per-type
// targets of the whole batch of n.
func scaleTargets(targets map[string]int, part, n int) map[string]int {
	if len(targets) == 0 || part == n {
		return targets
	}
	out := make(map[string]int, len(targets))
	for t, c := range targets {
		if v := (c*part + n/2) / n; v > 0 {
			out[t] = v
		}
	}
	return out
}

func codesOf(labels []generation.Label) []string {
	out := make([]string, len(labels))
	for i, l := range labels {
		out[i] = l.Code
	}
	return out
}

// dropSimilar removes candidates whose meaning is close to a stored hook.
// Without an embedder, or when it fails, candidates pass unchanged.
func (s *Service) dropSimilar(ctx context.Context, projectID int64, cands []generation.Candidate) ([]generation.Candidate, [][]float32, []generation.Duplicate) {
	if s.emb == nil || len(cands) == 0 {
		return cands, nil, nil
	}
	texts := make([]string, len(cands))
	for i, c := range cands {
		texts[i] = embedText(c.Text, c.OnScreenText)
	}
	vecs, err := s.emb.Embed(ctx, texts)
	if err != nil {
		s.log.Warn().Err(err).Msg("embed candidates")
		return cands, nil, nil
	}
	var kept []generation.Candidate
	var keptVecs [][]float32
	var dropped []generation.Duplicate
	for i, c := range cands {
		v := pgvector.NewVector(vecs[i])
		near, err := s.q.SimilarHooks(ctx, store.SimilarHooksParams{ProjectID: projectID, Embedding: &v, Limit: 1})
		if err != nil {
			s.log.Warn().Err(err).Msg("search similar hooks")
			return cands, nil, nil
		}
		if len(near) > 0 && near[0].Similarity >= vectorDupMinScore {
			dropped = append(dropped, generation.Duplicate{Text: c.Text, SimilarTo: near[0].Text, Similarity: near[0].Similarity})
			continue
		}
		kept = append(kept, c)
		keptVecs = append(keptVecs, vecs[i])
	}
	return kept, keptVecs, dropped
}

// CandidateView is a candidate on the review screen.
type CandidateView struct {
	ID           int64  `json:"id"`
	Text         string `json:"text"`
	OnScreenText string `json:"onScreenText"`
	Type         string `json:"type"`
	Mode         string `json:"mode"`
	BasedOn      string `json:"basedOn"`
	BasedOnCPA   *int64 `json:"basedOnCpaCents"`
	Rationale    string `json:"rationale"`
	Status       string `json:"status"`
	Number       string `json:"number"`
	AdName       string `json:"adName"`
}

// CandidatesView is the latest batch of a project.
type CandidatesView struct {
	Code       string                 `json:"code"`
	Name       string                 `json:"name"`
	BatchID    int64                  `json:"batchId"`
	CreatedAt  string                 `json:"createdAt"`
	Requested  int                    `json:"requested"`
	Dropped    []generation.Duplicate `json:"dropped"`
	NextNumber string                 `json:"nextNumber"`
	Items      []CandidateView        `json:"items"`
}

// Candidates returns the latest batch of a project.
func (s *Service) Candidates(ctx context.Context, code string) (CandidatesView, error) {
	p, err := s.project(ctx, code)
	if err != nil {
		return CandidatesView{}, err
	}
	maxNum, err := s.q.MaxHookNumber(ctx, p.ID)
	if err != nil {
		return CandidatesView{}, fmt.Errorf("max hook number: %w", err)
	}
	v := CandidatesView{Code: p.Code, Name: p.Name, NextNumber: hookLabel(ptr(maxNum + 1)), Items: []CandidateView{}, Dropped: []generation.Duplicate{}}

	b, err := s.q.LatestBatch(ctx, p.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, nil
	}
	if err != nil {
		return CandidatesView{}, fmt.Errorf("latest batch: %w", err)
	}
	v.BatchID, v.Requested, v.CreatedAt = b.ID, int(b.Requested), b.CreatedAt.Format(time.RFC3339)
	if err := json.Unmarshal(b.Dropped, &v.Dropped); err != nil {
		return CandidatesView{}, fmt.Errorf("decode dropped: %w", err)
	}

	_, ranked, err := s.rankProject(ctx, p)
	if err != nil {
		return CandidatesView{}, err
	}
	cpaByNumber := map[string]int64{}
	for _, r := range ranked {
		if cpa, ok := r.CPACents(); ok {
			cpaByNumber[r.number] = cpa
		}
	}
	hooks, err := s.q.ListBatchHooks(ctx, &b.ID)
	if err != nil {
		return CandidatesView{}, fmt.Errorf("list batch hooks: %w", err)
	}
	for _, h := range hooks {
		c := CandidateView{
			ID: h.ID, Text: h.Text, OnScreenText: h.OnScreenText, Type: generation.TypeName(deref(h.HookType)),
			Mode: deref(h.Mode), BasedOn: deref(h.BasedOn), Rationale: h.Rationale, Status: h.Status, Number: hookLabel(h.Number),
		}
		if cpa, ok := cpaByNumber[c.BasedOn]; ok {
			c.BasedOnCPA = ptr(cpa)
		}
		c.AdName = adNameOf(h, p)
		v.Items = append(v.Items, c)
	}
	return v, nil
}

// SelectCandidate gives a candidate the next H-number. The text may be
// edited at this point.
func (s *Service) SelectCandidate(ctx context.Context, id int64, in HookInput) error {
	if err := in.validate(); err != nil {
		return err
	}
	in.AdName = "" // a new hook always starts with the standard ad name
	var selected store.Hook
	var edited bool
	err := s.inTx(ctx, func(q *store.Queries) error {
		h, err := q.GetHook(ctx, id)
		if err != nil {
			return notFound(err, "candidate")
		}
		maxNum, err := q.MaxHookNumber(ctx, h.ProjectID)
		if err != nil {
			return fmt.Errorf("max hook number: %w", err)
		}
		selected, err = q.SelectHook(ctx, store.SelectHookParams{ID: id, Number: ptr(maxNum + 1), Text: in.Text, OnScreenText: in.OnScreenText})
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: This candidate was already reviewed", ErrConflict)
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("%w: The hook number was just taken, try again", ErrConflict)
		}
		if err != nil {
			return fmt.Errorf("select candidate: %w", err)
		}
		edited = selected.Text != h.Text || selected.OnScreenText != h.OnScreenText
		if edited {
			// The stored vector belongs to the old text.
			if err := q.SetHookEmbedding(ctx, store.SetHookEmbeddingParams{ID: id}); err != nil {
				return fmt.Errorf("clear embedding: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if edited || selected.Embedding == nil {
		selected.HookType = nil // re-label the edited text
		s.enrich(ctx, []store.Hook{selected})
	}
	return nil
}

// RejectCandidate keeps the candidate in history so it is never shown again.
func (s *Service) RejectCandidate(ctx context.Context, id int64) error {
	_, err := s.q.RejectHook(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: This candidate was already reviewed", ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("reject candidate: %w", err)
	}
	return nil
}

// UndoCandidate reopens a selected or rejected candidate.
func (s *Service) UndoCandidate(ctx context.Context, id int64) error {
	_, err := s.q.UndoHook(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: This candidate can no longer be reopened", ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("undo candidate: %w", err)
	}
	return nil
}
