package app

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
	"github.com/rs/zerolog"

	"github.com/godvlpr/hookymaster/internal/generation"
	"github.com/godvlpr/hookymaster/internal/store"
)

// These tests need Postgres with pgvector, for example:
//
//	TEST_DATABASE_URL=postgres://hookymaster:hookymaster@127.0.0.1:5434/hookymaster_test?sslmode=disable go test ./internal/app/
func testService(t *testing.T, llm LLM, emb Embedder) *Service {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()

	boot, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(boot); err != nil {
		t.Fatal(err)
	}
	truncate := func() {
		if _, err := boot.Exec(ctx, "TRUNCATE projects RESTART IDENTITY CASCADE"); err != nil {
			t.Fatal(err)
		}
	}
	truncate()
	// Leave the test database empty for the next run.
	t.Cleanup(func() { truncate(); boot.Close() })

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error { return pgxvec.RegisterTypes(ctx, conn) }
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return New(pool, llm, emb, zerolog.Nop())
}

// fakeLLM answers classification with fixed labels and generation with a
// fixed batch.
type fakeLLM struct{ batch []generation.Candidate }

func (fakeLLM) Model() string { return "fake" }

func (f fakeLLM) JSON(_ context.Context, system, user string) ([]byte, error) {
	if system == generateSystemPrompt() {
		return json.Marshal(map[string]any{"candidates": f.batch})
	}
	var in struct {
		Hooks []generation.HookText `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(user), &in); err != nil {
		return nil, err
	}
	labels := make([]generation.Labels, len(in.Hooks))
	for i, h := range in.Hooks {
		labels[i] = generation.Labels{ID: h.ID, Type: "question", Pain: "time", Emotion: "frustration"}
	}
	return json.Marshal(map[string]any{"labels": labels})
}

func generateSystemPrompt() string {
	s, _, _ := generation.BuildPrompt(generation.Input{})
	return s
}

// fakeEmbedder maps equal texts to equal vectors, so pgvector finds exact
// repeats and nothing else.
type fakeEmbedder struct{}

func (fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		h := fnv.New32a()
		_, _ = h.Write([]byte(generation.Normalize(t)))
		v := make([]float32, 1536)
		v[h.Sum32()%1536] = 1
		out[i] = v
	}
	return out, nil
}

func openTestdata(t *testing.T, parts ...string) *os.File {
	t.Helper()
	f, err := os.Open(filepath.Join(append([]string{"..", "..", "..", "testdata"}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

const exportFile = "ads_manager_export_2026-10.csv"

func seedTestdata(t *testing.T, s *Service) {
	t.Helper()
	ctx := context.Background()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "seed", "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	var projects []CreateProjectInput
	if err := json.Unmarshal(raw, &projects); err != nil {
		t.Fatal(err)
	}
	for _, p := range projects {
		if _, err := s.CreateProject(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ImportHooks(ctx, openTestdata(t, "meta_ads", "hooks_import.csv")); err != nil {
		t.Fatal(err)
	}
	for _, p := range projects {
		if _, err := s.ImportMeta(ctx, p.Code, exportFile, openTestdata(t, "meta_ads", exportFile)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestImportListsUnknownNumbersAndFillIn(t *testing.T) {
	s := testService(t, nil, nil)
	ctx := context.Background()
	if _, err := s.CreateProject(ctx, CreateProjectInput{Code: "O12", Name: "Lost in the wash"}); err != nil {
		t.Fatal(err)
	}

	// A file before any hook exists: no hook is created by itself, the
	// project's own ad names are listed for review.
	imp, err := s.ImportMeta(ctx, "O12", exportFile, openTestdata(t, "meta_ads", exportFile))
	if err != nil {
		t.Fatal(err)
	}
	var own []Assignment
	for _, u := range imp.Unrecognized {
		if u.Project == "O12" {
			own = append(own, Assignment{AdName: u.AdName})
		}
	}
	if imp.HooksCreated != 0 || imp.RowsMatched != 0 || len(own) != 15 || imp.SkippedOtherMarkets != 2 || len(badNames(imp)) != 1 {
		t.Errorf("import: %+v", imp)
	}
	if imp.SkippedOtherProjects == 0 {
		t.Error("rows of other projects must be skipped")
	}

	// New hooks for the project's own names keep the number from the name.
	imp, err = s.AssignAdNames(ctx, "O12", own)
	if err != nil {
		t.Fatal(err)
	}
	if imp.HooksCreated != 15 || imp.HooksUpdated != 15 || len(badNames(imp)) != 1 {
		t.Errorf("after assign: %+v", imp)
	}

	p, err := s.Project(ctx, "O12")
	if err != nil {
		t.Fatal(err)
	}
	var h01 HookView
	missing := 0
	for _, h := range p.Hooks {
		if h.Text == "" {
			missing++
		}
		if h.Number == "H01" {
			h01 = h
		}
		if want := "US_O12_" + h.Number; h.AdName != want {
			t.Errorf("%s: ad name %s, want %s from its number", h.Number, h.AdName, want)
		}
	}
	if missing != 15 || h01.Tier != "reference" {
		t.Fatalf("missing text %d, H01 %+v", missing, h01)
	}

	// Fill in the text afterwards.
	if err := s.UpdateHook(ctx, h01.ID, HookInput{Text: "Where do your socks actually go in the wash?"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateHook(ctx, h01.ID, HookInput{Text: " "}); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty text: got %v", err)
	}

	// The ad named differently in Meta: give a hook that name and import again.
	var h05 HookView
	for _, h := range p.Hooks {
		if h.Number == "H05" {
			h05 = h
		}
	}
	if err := s.UpdateHook(ctx, h05.ID, HookInput{Text: "I haven't lost a sock in 365 days", AdName: "Socks_magnet_final_v2_NEW"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateHook(ctx, h01.ID, HookInput{Text: "x", AdName: "socks_magnet_final_v2_new"}); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate ad name: got %v, want ErrConflict", err)
	}
	again, err := s.ImportMeta(ctx, "O12", exportFile, openTestdata(t, "meta_ads", exportFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(badNames(again)) != 0 || again.HooksCreated != 0 {
		t.Errorf("re-import: %+v", again)
	}
	p, err = s.Project(ctx, "O12")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range p.Hooks {
		if h.Number == "H05" && (h.AdName != "Socks_magnet_final_v2_NEW" || h.Impressions != 2360+60000) {
			t.Errorf("H05 after custom name: %+v", h)
		}
	}

	// A hook added by hand takes the number after the highest one.
	adName, err := s.AddHook(ctx, "O12", HookInput{Text: "Bet you lose a sock this week"})
	if err != nil {
		t.Fatal(err)
	}
	if adName != "US_O12_H100" {
		t.Errorf("ad name: got %s", adName)
	}

}

// badNames returns the unrecognized names that follow no convention.
func badNames(v ImportView) []Unrecognized {
	var out []Unrecognized
	for _, u := range v.Unrecognized {
		if u.Project == "" {
			out = append(out, u)
		}
	}
	return out
}

func TestAssignAdNamesAfterUpload(t *testing.T) {
	s := testService(t, nil, nil)
	ctx := context.Background()
	code, err := s.CreateProject(ctx, CreateProjectInput{Name: "Fresh"})
	if err != nil {
		t.Fatal(err)
	}
	manual, err := s.AddHook(ctx, code, HookInput{Text: "Where do your socks actually go in the wash?"})
	if err != nil {
		t.Fatal(err)
	}

	// No row follows this project's names: the file is kept, every name is listed.
	imp, err := s.ImportMeta(ctx, code, exportFile, openTestdata(t, "meta_ads", exportFile))
	if err != nil {
		t.Fatal(err)
	}
	if imp.RowsMatched != 0 || len(imp.Unrecognized) == 0 {
		t.Fatalf("import: %+v", imp)
	}
	p, err := s.Project(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	h01 := p.Hooks[0]

	_, err = s.AssignAdNames(ctx, code, []Assignment{{AdName: "Nope_ad", HookID: 0}})
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("unknown name: got %v, want ErrInvalid", err)
	}
	_, err = s.AssignAdNames(ctx, code, []Assignment{{AdName: "US_O12_H01", HookID: h01.ID}, {AdName: "US_O12_H02", HookID: h01.ID}})
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("one hook, two names: got %v, want ErrInvalid", err)
	}

	got, err := s.AssignAdNames(ctx, code, []Assignment{
		{AdName: "us_o12_h01", HookID: h01.ID},
		{AdName: "US_O12_H02"},
		{AdName: "Socks_magnet_final_v2_NEW"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.HooksCreated != 2 || got.HooksUpdated != 3 || got.RowsMatched == 0 {
		t.Errorf("after assign: %+v", got)
	}
	for _, u := range got.Unrecognized {
		if u.AdName == "US_O12_H01" || u.AdName == "Socks_magnet_final_v2_NEW" {
			t.Errorf("%s is still unrecognized", u.AdName)
		}
	}
	p, err = s.Project(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Hooks) != 3 || p.Hooks[0].AdName != "US_O12_H01" || p.Hooks[0].Impressions == 0 {
		t.Errorf("hooks after assign: %+v", p.Hooks)
	}
	if manual != AdName("US", code, 1) {
		t.Errorf("manual hook ad name: %s", manual)
	}
}

func TestProjectCodeAndDelete(t *testing.T) {
	s := testService(t, nil, nil)
	ctx := context.Background()
	code, err := s.CreateProject(ctx, CreateProjectInput{Name: "Rename me"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject(ctx, CreateProjectInput{Code: "o12", Name: "Taken"}); err != nil {
		t.Fatal(err)
	}

	p, err := s.Project(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	st := p.Settings
	st.Code = "o12"
	if _, err := s.UpdateSettings(ctx, code, st); !errors.Is(err, ErrConflict) {
		t.Errorf("taken code: got %v, want ErrConflict", err)
	}
	st.Code = "bad_code"
	if _, err := s.UpdateSettings(ctx, code, st); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad code: got %v, want ErrInvalid", err)
	}
	st.Code = "sock7"
	saved, err := s.UpdateSettings(ctx, code, st)
	if err != nil || saved.Code != "SOCK7" {
		t.Fatalf("rename: %+v, %v", saved, err)
	}

	// A deleted number is never given out again.
	for range 2 {
		if _, err := s.AddHook(ctx, "SOCK7", HookInput{Text: "hook"}); err != nil {
			t.Fatal(err)
		}
	}
	p, err = s.Project(ctx, "SOCK7")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range p.Hooks {
		if h.Number == "H02" {
			if err := s.DeleteHook(ctx, h.ID); err != nil {
				t.Fatal(err)
			}
			if err := s.DeleteHook(ctx, h.ID); !errors.Is(err, ErrNotFound) {
				t.Errorf("second delete: got %v, want ErrNotFound", err)
			}
		}
	}
	next, err := s.AddHook(ctx, "SOCK7", HookInput{Text: "another"})
	if err != nil || next != "US_SOCK7_H03" {
		t.Errorf("next after delete: %s, %v", next, err)
	}

	// Bulk delete: a hook of another project rolls back the whole call.
	other, err := s.AddHook(ctx, "O12", HookInput{Text: "other project"})
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.Project(ctx, "SOCK7")
	if err != nil {
		t.Fatal(err)
	}
	o12, err := s.Project(ctx, "O12")
	if err != nil {
		t.Fatal(err)
	}
	ids := []int64{p.Hooks[0].ID, p.Hooks[1].ID}
	if err := s.DeleteHooks(ctx, "SOCK7", append(ids, o12.Hooks[0].ID)); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign hook %s: got %v, want ErrNotFound", other, err)
	}
	if err := s.DeleteHooks(ctx, "SOCK7", nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("no ids: got %v, want ErrInvalid", err)
	}
	if err := s.DeleteHooks(ctx, "SOCK7", ids); err != nil {
		t.Fatal(err)
	}
	p, err = s.Project(ctx, "SOCK7")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Hooks) != 0 {
		t.Errorf("hooks after bulk delete: %+v", p.Hooks)
	}

	if err := s.DeleteProject(ctx, "SOCK7"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Project(ctx, "SOCK7"); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted project: got %v, want ErrNotFound", err)
	}
}

func TestRankingFollowsSettings(t *testing.T) {
	s := testService(t, nil, nil)
	seedTestdata(t, s)
	ctx := context.Background()

	p, err := s.Project(ctx, "O12")
	if err != nil {
		t.Fatal(err)
	}
	tiers := map[string]string{}
	for _, h := range p.Hooks {
		tiers[h.Number] = h.Tier
	}
	want := map[string]string{"H01": "reference", "H02": "reference", "H09": "reference", "H06": "anti", "H03": "anti", "H05": "nodata", "H14": "pending"}
	for n, tier := range want {
		if tiers[n] != tier {
			t.Errorf("%s: got %q, want %q", n, tiers[n], tier)
		}
	}

	// Re-importing the same period replaces results instead of adding them.
	if _, err := s.ImportMeta(ctx, "O12", "again.csv", openTestdata(t, "meta_ads", exportFile)); err != nil {
		t.Fatal(err)
	}
	again, err := s.Project(ctx, "O12")
	if err != nil {
		t.Fatal(err)
	}
	if again.SpendCents != p.SpendCents {
		t.Errorf("spend after re-import: got %d, want %d", again.SpendCents, p.SpendCents)
	}

	// Stricter settings: a lower CPA limit leaves fewer references.
	st := p.Settings
	st.MaxCPACents, st.RefSharePct, st.MinRefs, st.MaxRefs = 4000, 100, 1, 10
	if _, err := s.UpdateSettings(ctx, "O12", st); err != nil {
		t.Fatal(err)
	}
	strict, err := s.Project(ctx, "O12")
	if err != nil {
		t.Fatal(err)
	}
	refs := 0
	for _, h := range strict.Hooks {
		if h.Tier == "reference" {
			refs++
		}
	}
	if refs != 2 {
		t.Errorf("references with max CPA $40 and share 100%%: got %d, want 2", refs)
	}
}

func TestGenerateSelectExport(t *testing.T) {
	batch := []generation.Candidate{
		{Text: "POV: the dryer just finished and one sock is missing", OnScreenText: "1 in, 1 out", Type: "pov", Pain: "time", Emotion: "frustration", Mode: "exploit", BasedOn: "H02"},
		{Text: "Where do your socks actually go in the wash?", Type: "question", Pain: "time", Emotion: "frustration", Mode: "exploit", BasedOn: "H01"},
		{Text: "Parents of three, this one is for you", Type: "callout", Pain: "relationships", Emotion: "warmth", Mode: "explore"},
		{Text: "", Type: "pov", Pain: "none", Emotion: "humor", Mode: "exploit"},
	}
	s := testService(t, fakeLLM{batch: batch}, fakeEmbedder{})
	seedTestdata(t, s)
	ctx := context.Background()

	opts, err := s.GenerateOptions(ctx, "O12")
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.References) != 3 || opts.References[0].Number != "H01" {
		t.Errorf("references: got %+v", opts.References)
	}

	in := GenerateInput{N: 4, ExploitPct: 70, TypeShares: map[string]int{"callout": 25}}
	v, err := s.Generate(ctx, "O12", in)
	if err != nil {
		t.Fatal(err)
	}
	// The empty hook is invalid and the copy of H01 is a duplicate; the top-up
	// gets the same answer from the fake, so it only adds duplicates.
	if len(v.Items) != 2 || len(v.Dropped) == 0 {
		t.Fatalf("got %d items, %d dropped: %+v", len(v.Items), len(v.Dropped), v)
	}
	if v.Items[0].BasedOn != "H02" || v.Items[0].BasedOnCPA == nil || v.NextNumber != "H17" {
		t.Errorf("first candidate: %+v, next %s", v.Items[0], v.NextNumber)
	}

	// H99 is only in the export, not in the hooks file, so it is no hook yet.

	// The same batch again: everything is now a duplicate thanks to pgvector.
	again, err := s.Generate(ctx, "O12", in)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Items) != 0 {
		t.Errorf("second batch must be all duplicates, got %+v", again.Items)
	}

	first, second := v.Items[0].ID, v.Items[1].ID
	if err := s.SelectCandidate(ctx, first, HookInput{Text: "POV: the dryer just finished and one sock is missing", OnScreenText: "1 in, 1 out"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SelectCandidate(ctx, first, HookInput{Text: "x"}); !errors.Is(err, ErrConflict) {
		t.Errorf("second select: got %v, want ErrConflict", err)
	}
	if err := s.RejectCandidate(ctx, second); err != nil {
		t.Fatal(err)
	}

	q, err := s.Queue(ctx, QueueReady)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Items) != 1 || q.Items[0].AdName != "US_O12_H17" {
		t.Fatalf("queue: %+v", q.Items)
	}
	csv, err := s.Export(ctx, []int64{q.Items[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if want := "US_O12_H17,POV: the dryer just finished and one sock is missing,"; !strings.Contains(string(csv), want) {
		t.Errorf("csv: %s", csv)
	}
	if _, err := s.Export(ctx, []int64{q.Items[0].ID}); !errors.Is(err, ErrConflict) {
		t.Errorf("export twice: got %v, want ErrConflict", err)
	}
}

func TestGenerateWithoutModel(t *testing.T) {
	s := &Service{}
	if _, err := s.Generate(context.Background(), "O12", GenerateInput{N: 5}); !errors.Is(err, ErrNoModel) {
		t.Errorf("got %v, want ErrNoModel", err)
	}
}

func TestGenerateEmptyProjectNeedsBrief(t *testing.T) {
	batch := []generation.Candidate{
		{Text: "Dinner in 15 minutes, even on a Tuesday", Type: "offer", Pain: "time", Emotion: "relief", Mode: "explore"},
	}
	s := testService(t, fakeLLM{batch: batch}, fakeEmbedder{})
	ctx := context.Background()
	code, err := s.CreateProject(ctx, CreateProjectInput{Name: "Meal kit"})
	if err != nil {
		t.Fatal(err)
	}

	opts, err := s.GenerateOptions(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.NeedsBrief {
		t.Error("an empty project must ask for a brief")
	}
	if _, err := s.Generate(ctx, code, GenerateInput{N: 1, Brief: "  "}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("without brief: got %v, want ErrInvalid", err)
	}
	v, err := s.Generate(ctx, code, GenerateInput{N: 1, Brief: "Meal kits for busy parents"})
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Items) != 1 {
		t.Errorf("got %d candidates, want 1", len(v.Items))
	}
}

func TestGenerateEmptyProjectWithDescription(t *testing.T) {
	batch := []generation.Candidate{
		{Text: "Dinner in 15 minutes, even on a Tuesday", Type: "offer", Pain: "time", Emotion: "relief", Mode: "explore"},
	}
	s := testService(t, fakeLLM{batch: batch}, fakeEmbedder{})
	ctx := context.Background()
	code, err := s.CreateProject(ctx, CreateProjectInput{Name: "Meal kit", Description: "Meal kits for busy parents"})
	if err != nil {
		t.Fatal(err)
	}

	opts, err := s.GenerateOptions(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	if opts.NeedsBrief {
		t.Error("a project with a description must not ask for a brief")
	}
	v, err := s.Generate(ctx, code, GenerateInput{N: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Items) != 1 {
		t.Errorf("got %d candidates, want 1", len(v.Items))
	}
}

func TestDeletedHookStillBlocksDuplicates(t *testing.T) {
	text := "Dinner in 15 minutes, even on a Tuesday"
	batch := []generation.Candidate{
		{Text: text, Type: "offer", Pain: "time", Emotion: "relief", Mode: "explore"},
	}
	s := testService(t, fakeLLM{batch: batch}, fakeEmbedder{})
	ctx := context.Background()
	code, err := s.CreateProject(ctx, CreateProjectInput{Name: "Meal kit", Description: "Meal kits for busy parents"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddHook(ctx, code, HookInput{Text: text}); err != nil {
		t.Fatal(err)
	}
	p, err := s.Project(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	hookID := p.Hooks[0].ID
	if err := s.DeleteHook(ctx, hookID); err != nil {
		t.Fatal(err)
	}

	// Hidden everywhere.
	p, err = s.Project(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Hooks) != 0 {
		t.Errorf("deleted hook is still listed: %+v", p.Hooks)
	}
	if err := s.UpdateHook(ctx, hookID, HookInput{Text: "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("edit deleted hook: got %v", err)
	}

	// Still known to the generator: the same text comes back as a duplicate.
	v, err := s.Generate(ctx, code, GenerateInput{N: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Items) != 0 || len(v.Dropped) == 0 {
		t.Errorf("got %d items, %d dropped, want the copy of the deleted hook dropped", len(v.Items), len(v.Dropped))
	}

	// Its number stays taken.
	next, err := s.AddHook(ctx, code, HookInput{Text: "Another hook"})
	if err != nil || next != AdName("US", code, 2) {
		t.Errorf("next after delete: %s, %v", next, err)
	}
}

// seqLLM answers each generation request with the next batch and records how
// many hooks each request asked for.
type seqLLM struct {
	mu      sync.Mutex
	batches [][]generation.Candidate
	asked   []int
}

func (*seqLLM) Model() string { return "fake" }

func (f *seqLLM) JSON(_ context.Context, system, user string) ([]byte, error) {
	if system != generateSystemPrompt() {
		return fakeLLM{}.JSON(context.Background(), system, user)
	}
	var in struct {
		Plan []generation.Slot `json:"plan"`
	}
	if err := json.Unmarshal([]byte(user), &in); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, generation.Count(in.Plan))
	batch := f.batches[0]
	f.batches = f.batches[1:]
	return json.Marshal(map[string]any{"candidates": batch})
}

func TestGenerateTopsUpShortBatch(t *testing.T) {
	ok := func(text string) generation.Candidate {
		return generation.Candidate{Text: text, Type: "pov", Pain: "time", Emotion: "relief", Mode: "explore"}
	}
	llm := &seqLLM{batches: [][]generation.Candidate{
		{ok("Dinner in 15 minutes, even on a Tuesday"), ok("one two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen sixteen")},
		{{Text: "My kids asked for seconds of the broccoli", Type: "pov", Pain: "time", Emotion: "excitement", Mode: "explore"}},
	}}
	s := testService(t, llm, nil)
	ctx := context.Background()
	code, err := s.CreateProject(ctx, CreateProjectInput{Name: "Meal kit", Description: "Meal kits", Buyer: "Parents who cook after work"})
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.Generate(ctx, code, GenerateInput{N: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Items) != 2 {
		t.Errorf("got %d candidates, want 2 after the top-up", len(v.Items))
	}
	if len(llm.asked) != 2 || llm.asked[0] != 2 || llm.asked[1] != 1 {
		t.Errorf("requests asked for %v, want [2 1]", llm.asked)
	}
	// "excitement" is not in the taxonomy: the hook stays and is labeled after.
	for _, it := range v.Items {
		if strings.HasPrefix(it.Text, "My kids") && it.Type != generation.TypeName("question") {
			t.Errorf("relabeled hook: got type %q", it.Type)
		}
	}
	p, err := s.Project(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	if p.Settings.Buyer != "Parents who cook after work" {
		t.Errorf("buyer: got %q", p.Settings.Buyer)
	}
}

// The sidebar and overview counters must match what the screens show: the
// Candidates screen lists only the latest batch, and the Ready tab of the
// queue leaves out hooks that already have results.
func TestCountersMatchScreens(t *testing.T) {
	explore := func(text string) generation.Candidate {
		return generation.Candidate{Text: text, Type: "pov", Pain: "time", Emotion: "relief", Mode: "explore"}
	}
	llm := &seqLLM{batches: [][]generation.Candidate{
		{explore("Dinner in 15 minutes, even on a Tuesday"), explore("The fridge is empty and it is 7pm"), explore("My kids ate the green beans first")},
		{explore("No chopping, no shopping list tonight"), explore("Sunday prep took me ten minutes")},
	}}
	s := testService(t, llm, nil)
	ctx := context.Background()
	code, err := s.CreateProject(ctx, CreateProjectInput{Name: "Meal kit", Description: "Meal kits for busy parents"})
	if err != nil {
		t.Fatal(err)
	}

	first, err := s.Generate(ctx, code, GenerateInput{N: 3})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SelectCandidate(ctx, first.Items[0].ID, HookInput{Text: first.Items[0].Text}); err != nil {
		t.Fatal(err)
	}
	// Two candidates of the first batch stay open, but the screen moves on to
	// the second batch.
	second, err := s.Generate(ctx, code, GenerateInput{N: 2})
	if err != nil {
		t.Fatal(err)
	}
	checkCounters(t, s, code, len(second.Items), 1)

	// The selected hook goes live in Meta before it is exported.
	export := "Reporting starts,Reporting ends,Campaign name,Ad set name,Ad name,Delivery,Amount spent (USD),Impressions,Reach," +
		"\"CPM (cost per 1,000 impressions) (USD)\",3-second video plays,ThruPlays,Link clicks,CTR (link click-through rate)," +
		"CPC (cost per link click) (USD),Purchases,Cost per purchase (USD)\n" +
		"2026-10-01,2026-10-31,C,S," + AdName("US", code, 1) + ",active,500.00,40000,30000,12.50,10000,4000,500,1.25,1.00,10,50.00\n"
	if _, err := s.ImportMeta(ctx, code, "live.csv", strings.NewReader(export)); err != nil {
		t.Fatal(err)
	}
	checkCounters(t, s, code, len(second.Items), 0)
}

func checkCounters(t *testing.T, s *Service, code string, wantOpen, wantReady int) {
	t.Helper()
	ctx := context.Background()
	cands, err := s.Candidates(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	onScreen := 0
	for _, c := range cands.Items {
		if c.Status == "candidate" {
			onScreen++
		}
	}
	nav, err := s.Nav(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ov, err := s.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Project(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	q, err := s.Queue(ctx, QueueReady)
	if err != nil {
		t.Fatal(err)
	}
	if onScreen != wantOpen || nav.Projects[0].OpenCandidates != wantOpen || ov.OpenCandidates != wantOpen || p.OpenCandidates != wantOpen {
		t.Errorf("open candidates: screen %d, nav %d, overview %d, project %d, want %d",
			onScreen, nav.Projects[0].OpenCandidates, ov.OpenCandidates, p.OpenCandidates, wantOpen)
	}
	if len(q.Items) != wantReady || q.Counts[QueueReady] != wantReady || nav.ReadyToExport != wantReady || ov.ReadyToExport != wantReady {
		t.Errorf("ready to export: queue %d (count %d), nav %d, overview %d, want %d",
			len(q.Items), q.Counts[QueueReady], nav.ReadyToExport, ov.ReadyToExport, wantReady)
	}
}
