package ranking

import (
	"strings"
	"testing"
)

func TestRank(t *testing.T) {
	// O12 from testdata, aggregated over ad sets.
	stats := []Stats{
		{HookID: 1, Impressions: 314353, SpendCents: 360400, Purchases: 97},  // $37.16
		{HookID: 2, Impressions: 242816, SpendCents: 298300, Purchases: 77},  // $38.74
		{HookID: 3, Impressions: 71296, SpendCents: 80600, Purchases: 10},    // early
		{HookID: 4, Impressions: 131723, SpendCents: 158900, Purchases: 34},  // $46.74
		{HookID: 5, Impressions: 2360, SpendCents: 2600, Purchases: 0},       // no data
		{HookID: 6, Impressions: 148679, SpendCents: 188400, Purchases: 29},  // $64.97 anti
		{HookID: 7, Impressions: 187390, SpendCents: 208500, Purchases: 45},  // $46.33
		{HookID: 9, Impressions: 231467, SpendCents: 277300, Purchases: 61},  // $45.46
		{HookID: 13, Impressions: 125663, SpendCents: 166800, Purchases: 29}, // $57.52, too few purchases to be sure
	}

	got := map[int64]Tier{}
	for _, r := range Rank(stats, DefaultRules) {
		got[r.HookID] = r.Tier
	}

	want := map[int64]Tier{
		1: TierReference, 2: TierReference, 9: TierReference,
		7: TierGood, 4: TierGood,
		6: TierAnti,
		// Only one sure anti-example: the two over max CPA with at least
		// 10 purchases fill up to three as likely ones.
		13: TierAnti, 3: TierAnti,
		5: TierNoData,
	}
	for id, tier := range want {
		if got[id] != tier {
			t.Errorf("hook %d: got %q, want %q", id, got[id], tier)
		}
	}
}

func TestRankReferenceShare(t *testing.T) {
	tests := []struct {
		name       string
		underLimit int
		wantRefs   int
	}{
		// Only the hooks with CPA under the project average count: half of them.
		{"minimum of three", 7, 3},
		{"fifteen percent of forty above average", 80, 6},
		{"fewer hooks than the minimum", 2, 2},
		{"capped by the maximum", 200, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stats []Stats
			for i := range tt.underLimit {
				stats = append(stats, Stats{HookID: int64(i), Impressions: 10000, SpendCents: int64(4000+i*10) * 20, Purchases: 20})
			}
			var refs int
			for _, r := range Rank(stats, DefaultRules) {
				if r.Tier == TierReference {
					refs++
				}
			}
			if refs != tt.wantRefs {
				t.Errorf("references: got %d, want %d", refs, tt.wantRefs)
			}
		})
	}
}

func TestRankOrder(t *testing.T) {
	stats := []Stats{
		{HookID: 1, Impressions: 100},
		{HookID: 2, Impressions: 5000, SpendCents: 100000, Purchases: 25},
		{HookID: 3, Impressions: 5000, Purchases: 5},
		{HookID: 4, Impressions: 5000, SpendCents: 50000, Purchases: 25},
	}
	var order []int64
	for _, r := range Rank(stats, DefaultRules) {
		order = append(order, r.HookID)
	}
	want := []int64{4, 2, 3, 1}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order: got %v, want %v", order, want)
		}
	}
}

func TestCPA(t *testing.T) {
	if _, ok := (Stats{SpendCents: 100}).CPACents(); ok {
		t.Error("CPA without purchases must not be ok")
	}
	if cpa, _ := (Stats{SpendCents: 5000, Purchases: 2}).CPACents(); cpa != 2500 {
		t.Errorf("CPA: got %d, want 2500", cpa)
	}
}

func TestRankWeights(t *testing.T) {
	// A sells cheaper, B hooks better; purchases and CTR are equal.
	stats := []Stats{
		{HookID: 1, Impressions: 100000, SpendCents: 200000, Purchases: 50, Plays3s: 20000, LinkClicks: 1000}, // $40, 20%
		{HookID: 2, Impressions: 100000, SpendCents: 210000, Purchases: 50, Plays3s: 40000, LinkClicks: 1000}, // $42, 40%
	}
	tests := []struct {
		name    string
		weights Weights
		first   int64
	}{
		{"default: CPA first", DefaultWeights, 1},
		{"hook rate weighs more", Weights{CPA: 20, HookRate: 80}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := DefaultRules
			rules.Weights = tt.weights
			got := Rank(stats, rules)
			if got[0].HookID != tt.first {
				t.Errorf("first: got hook %d (score %.2f), want %d", got[0].HookID, got[0].Score, tt.first)
			}
		})
	}
}

func TestRankConfidence(t *testing.T) {
	stats := []Stats{
		{HookID: 1, Impressions: 50000, SpendCents: 800000, Purchases: 100, Plays3s: 20000}, // $80, sure
		{HookID: 2, Impressions: 50000, SpendCents: 165300, Purchases: 29, Plays3s: 5000},   // $57, not sure
		{HookID: 3, Impressions: 50000, SpendCents: 200000, Purchases: 25, Plays3s: 4000},   // $80, sure, dull
		{HookID: 4, Impressions: 50000, SpendCents: 200000, Purchases: 50, Plays3s: 9000},   // $40
		{HookID: 5, Impressions: 50000, SpendCents: 900000, Purchases: 100, Plays3s: 3000},  // $90, sure: three sure ones, no fill
	}
	got := map[int64]Result{}
	for _, r := range Rank(stats, DefaultRules) {
		got[r.HookID] = r
	}
	if got[1].Tier != TierAnti || !got[1].AttentionNoSales {
		t.Errorf("hook 1: %+v, want an anti-example that hooks attention", got[1])
	}
	if got[2].Tier != TierEarly {
		t.Errorf("hook 2: got %q, want early", got[2].Tier)
	}
	if got[3].Tier != TierAnti || got[3].AttentionNoSales {
		t.Errorf("hook 3: %+v, want an anti-example without the attention flag", got[3])
	}
	if got[4].Tier != TierReference {
		t.Errorf("hook 4: got %q, want reference", got[4].Tier)
	}
}

func TestRankSpreadsReferencesAcrossTypes(t *testing.T) {
	stats := []Stats{
		{HookID: 1, Impressions: 50000, SpendCents: 60000, Purchases: 20, Group: "question"},
		{HookID: 2, Impressions: 50000, SpendCents: 64000, Purchases: 20, Group: "question"},
		{HookID: 3, Impressions: 50000, SpendCents: 68000, Purchases: 20, Group: "question"},
		{HookID: 4, Impressions: 50000, SpendCents: 72000, Purchases: 20, Group: "pov"},
		// Two weak hooks lift the project average, so the pov hook beats it.
		{HookID: 5, Impressions: 50000, SpendCents: 100000, Purchases: 20, Group: "list"},
		{HookID: 6, Impressions: 50000, SpendCents: 100000, Purchases: 20, Group: "list"},
	}
	got := map[int64]Tier{}
	for _, r := range Rank(stats, DefaultRules) {
		got[r.HookID] = r.Tier
	}
	want := map[int64]Tier{1: TierReference, 2: TierReference, 3: TierGood, 4: TierReference, 5: TierGood, 6: TierGood}
	for id, tier := range want {
		if got[id] != tier {
			t.Errorf("hook %d: got %q, want %q", id, got[id], tier)
		}
	}
}

func TestPlaces(t *testing.T) {
	rs := []Result{{Stats: Stats{Purchases: 10}}, {Stats: Stats{Purchases: 30}}, {Stats: Stats{Purchases: 30}}, {Stats: Stats{Purchases: 5}}}
	got := places(rs, func(s Stats) float64 { return float64(s.Purchases) })
	want := []float64{1.0 / 3, 2.5 / 3, 2.5 / 3, 0}
	for i := range want {
		if d := got[i] - want[i]; d > 1e-9 || d < -1e-9 {
			t.Errorf("place %d: got %.3f, want %.3f", i, got[i], want[i])
		}
	}
}

func TestRankFillsReferencesWithOneType(t *testing.T) {
	var stats []Stats
	for i := range 4 {
		stats = append(stats, Stats{HookID: int64(i), Impressions: 50000, SpendCents: int64(60000 + i*4000), Purchases: 20, Group: "question"})
	}
	var refs int
	for _, r := range Rank(stats, DefaultRules) {
		if r.Tier == TierReference {
			refs++
		}
	}
	if refs != DefaultRules.MinRefs {
		t.Errorf("references: got %d, want %d", refs, DefaultRules.MinRefs)
	}
}

func TestRankFillsAntiExamples(t *testing.T) {
	stats := []Stats{
		{HookID: 1, Impressions: 50000, SpendCents: 80000, Purchases: 40},  // $20, reference
		{HookID: 2, Impressions: 50000, SpendCents: 120000, Purchases: 12}, // $100, likely bad
		{HookID: 3, Impressions: 50000, SpendCents: 84000, Purchases: 12},  // $70, likely bad
		{HookID: 4, Impressions: 50000, SpendCents: 120000, Purchases: 5},  // $240, too few purchases
		{HookID: 5, Impressions: 50000, SpendCents: 30000, Purchases: 12},  // $25, under the limit
	}
	got := map[int64]Result{}
	for _, r := range Rank(stats, DefaultRules) {
		got[r.HookID] = r
	}
	want := map[int64]Tier{1: TierReference, 2: TierAnti, 3: TierAnti, 4: TierEarly, 5: TierEarly}
	for id, tier := range want {
		if got[id].Tier != tier {
			t.Errorf("hook %d: got %q (%s), want %q", id, got[id].Tier, got[id].Reason, tier)
		}
	}
	if !strings.Contains(got[2].Reason, "likely bad") {
		t.Errorf("hook 2 reason: %q", got[2].Reason)
	}
}

func TestRankReferencesBeatProjectAverage(t *testing.T) {
	// All four are under max CPA ($54), but only two beat the project
	// average ($40.50); min references still gives three.
	stats := []Stats{
		{HookID: 1, Impressions: 50000, SpendCents: 120000, Purchases: 40}, // $30
		{HookID: 2, Impressions: 50000, SpendCents: 140000, Purchases: 40}, // $35
		{HookID: 3, Impressions: 50000, SpendCents: 190000, Purchases: 40}, // $47.50
		{HookID: 4, Impressions: 50000, SpendCents: 198000, Purchases: 40}, // $49.50
	}
	got := map[int64]Tier{}
	for _, r := range Rank(stats, DefaultRules) {
		got[r.HookID] = r.Tier
	}
	want := map[int64]Tier{1: TierReference, 2: TierReference, 3: TierReference, 4: TierGood}
	for id, tier := range want {
		if got[id] != tier {
			t.Errorf("hook %d: got %q, want %q", id, got[id], tier)
		}
	}

	rules := DefaultRules
	rules.MinRefs = 1
	refs := 0
	for _, r := range Rank(stats, rules) {
		if r.Tier == TierReference {
			refs++
		}
	}
	if refs != 1 {
		t.Errorf("with min 1: got %d references, want 1 (15%% of the two strong hooks rounds to 0, min is 1)", refs)
	}
}
