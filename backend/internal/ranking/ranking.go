// Package ranking splits hooks into tiers by their Meta Ads results.
package ranking

import (
	"fmt"
	"math"
	"sort"
)

// Tier is the role a hook plays in the next generation.
type Tier string

const (
	TierReference Tier = "reference"
	TierGood      Tier = "good"
	TierAnti      Tier = "anti"
	TierEarly     Tier = "early"
	TierNoData    Tier = "nodata"
)

// Rules are the ranking thresholds.
type Rules struct {
	MinImpressions int64
	MinPurchases   int64
	MaxCPACents    int64
	RefShare       float64 // share of hooks under MaxCPACents used as references
	MinRefs        int
	MaxRefs        int
	Weights        Weights
}

// Weights say how much each metric counts in the score of a hook. Only their
// ratio matters.
type Weights struct {
	CPA       int
	HookRate  int
	Purchases int
	CTR       int
}

// DefaultWeights put CPA first, then hook rate, purchases and CTR. Hook rate
// weighs little: it tracks attention far more than sales.
var DefaultWeights = Weights{CPA: 70, HookRate: 15, Purchases: 10, CTR: 5}

// DefaultRules keep ROMI at 125% with LTV $68.
var DefaultRules = Rules{
	MinImpressions: 3000,
	MinPurchases:   20,
	MaxCPACents:    5400,
	RefShare:       0.15,
	MinRefs:        3,
	MaxRefs:        10,
	Weights:        DefaultWeights,
}

// Stats are raw counts for one hook, already summed over rows and periods.
type Stats struct {
	HookID      int64
	Impressions int64
	SpendCents  int64
	Plays3s     int64
	LinkClicks  int64
	Purchases   int64
	Group       string // hook type; references are spread across groups
}

// HookRate is 3-second plays per impression.
func (s Stats) HookRate() float64 { return ratio(s.Plays3s, s.Impressions) }

// CTR is link clicks per impression.
func (s Stats) CTR() float64 { return ratio(s.LinkClicks, s.Impressions) }

// CPACents is spend per purchase; ok is false without purchases.
func (s Stats) CPACents() (cents int64, ok bool) {
	if s.Purchases == 0 {
		return 0, false
	}
	return s.SpendCents / s.Purchases, true
}

func ratio(a, b int64) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

// Result is the tier of one hook and a short reason for it.
type Result struct {
	Stats
	Tier   Tier
	Reason string
	Score  float64 // 0–1, set for ranked hooks
	// AttentionNoSales marks an anti-example whose hook rate is at least the
	// median: it stops the scroll but does not sell.
	AttentionNoSales bool
}

// maxRefsPerGroup keeps references varied: at most this many share a hook
// type, so one lucky angle does not fill the whole next batch. When the
// project has too few types, the best of the rest fill the remaining places.
const maxRefsPerGroup = 2

// Rank assigns a tier to every hook. Ranked hooks come first, best score
// first; the rest follow in a stable order.
//
// Each ranked hook gets a score: its place among the ranked hooks of the
// project (0 worst, 1 best) on CPA, hook rate, purchases and CTR, mixed by
// the weights. References are the best scores among hooks whose CPA beats
// both max CPA and the project average.
//
// A CPA from few purchases is unreliable, so an anti-example needs a CPA over
// the limit even with one standard deviation more purchases (Poisson:
// spend / (p + √p)). Over the limit but not sure stays Early.
func Rank(stats []Stats, r Rules) []Result {
	var confident, rest []Result
	for _, s := range stats {
		switch {
		case s.Impressions < r.MinImpressions:
			rest = append(rest, Result{Stats: s, Tier: TierNoData,
				Reason: fmt.Sprintf("%d impressions, needs %d", s.Impressions, r.MinImpressions)})
		case s.Purchases < r.MinPurchases:
			rest = append(rest, Result{Stats: s, Tier: TierEarly,
				Reason: fmt.Sprintf("%d purchases, needs %d", s.Purchases, r.MinPurchases)})
		default:
			confident = append(confident, Result{Stats: s})
		}
	}

	score(confident, r.Weights)
	sort.SliceStable(confident, func(i, j int) bool { return confident[i].Score > confident[j].Score })

	// References must beat both max CPA and the project's own average CPA,
	// so they do not fill up with average hooks as the project grows.
	bar := min(r.MaxCPACents, averageCPA(confident))
	var strong int
	for _, c := range confident {
		if cpa, _ := c.CPACents(); cpa <= bar {
			strong++
		}
	}
	refs := min(max(int(float64(strong)*r.RefShare+0.5), r.MinRefs), r.MaxRefs)
	var early []Result // hooks with enough impressions but few purchases
	for _, e := range rest {
		if e.Tier == TierEarly {
			early = append(early, e)
		}
	}
	medianRate := medianHookRate(append(append([]Result(nil), confident...), early...))
	perGroup := map[string]int{}
	var given int
	for i := range confident {
		c := &confident[i]
		cpa, _ := c.CPACents()
		switch {
		case c.optimisticCPA() > float64(r.MaxCPACents):
			c.Tier = TierAnti
			c.Reason = fmt.Sprintf("CPA over $%d with %d purchases", r.MaxCPACents/100, c.Purchases)
			if c.HookRate() >= medianRate {
				c.AttentionNoSales = true
				c.Reason += "; hooks attention but does not sell"
			}
		case cpa > r.MaxCPACents:
			c.Tier = TierEarly
			c.Reason = fmt.Sprintf("CPA over $%d, but %d purchases are too few to be sure", r.MaxCPACents/100, c.Purchases)
		case given < refs && cpa <= bar && (c.Group == "" || perGroup[c.Group] < maxRefsPerGroup):
			c.Tier = TierReference
			c.Reason = fmt.Sprintf("Score %.2f, top %d, CPA under the project average $%d", c.Score, refs, bar/100)
			perGroup[c.Group]++
			given++
		default:
			c.Tier = TierGood
			c.Reason = fmt.Sprintf("Score %.2f, CPA under $%d", c.Score, r.MaxCPACents/100)
		}
	}
	// Fill up: first strong hooks beyond the per-type limit, then, only up to
	// min references, the best of the rest under max CPA.
	for _, pass := range []struct {
		limit int64
		upTo  int
	}{{bar, refs}, {r.MaxCPACents, min(refs, r.MinRefs)}} {
		for i := range confident {
			if given >= pass.upTo {
				break
			}
			c := &confident[i]
			if cpa, _ := c.CPACents(); c.Tier == TierGood && cpa <= pass.limit {
				c.Tier = TierReference
				c.Reason = fmt.Sprintf("Score %.2f, top %d", c.Score, refs)
				given++
			}
		}
	}
	fillAnti(confident, rest, r, medianRate)
	sort.SliceStable(rest, func(i, j int) bool { return rest[i].Tier == TierEarly && rest[j].Tier == TierNoData })
	return append(confident, rest...)
}

// averageCPA is the CPA of all given hooks together: total spend over total
// purchases.
func averageCPA(rs []Result) int64 {
	var spend, purchases int64
	for _, r := range rs {
		spend += r.SpendCents
		purchases += r.Purchases
	}
	if purchases == 0 {
		return 0
	}
	return spend / purchases
}

// minAnti is how many anti-examples the generator should see when the data
// has them.
const minAnti = 3

// fillAnti adds likely anti-examples when there are fewer than minAnti sure
// ones. Bad hooks are often switched off before they reach min purchases, so
// without this the generator never learns what to avoid. Candidates are
// hooks over max CPA with at least half of min purchases, most expensive
// first.
func fillAnti(confident, rest []Result, r Rules, medianRate float64) {
	var have int
	var likely []*Result
	floor := max((r.MinPurchases+1)/2, 1)
	for _, list := range [][]Result{confident, rest} {
		for i := range list {
			c := &list[i]
			cpa, ok := c.CPACents()
			switch {
			case c.Tier == TierAnti:
				have++
			case c.Tier == TierEarly && ok && cpa > r.MaxCPACents && c.Purchases >= floor:
				likely = append(likely, c)
			}
		}
	}
	sort.SliceStable(likely, func(i, j int) bool {
		a, _ := likely[i].CPACents()
		b, _ := likely[j].CPACents()
		return a > b
	})
	for _, c := range likely {
		if have >= minAnti {
			return
		}
		cpa, _ := c.CPACents()
		c.Tier = TierAnti
		c.Reason = fmt.Sprintf("CPA $%d over $%d with only %d purchases: likely bad", cpa/100, r.MaxCPACents/100, c.Purchases)
		if c.HookRate() >= medianRate {
			c.AttentionNoSales = true
			c.Reason += "; hooks attention but does not sell"
		}
		have++
	}
}

// score sets Score on every result: the weighted mean of its places on the
// four metrics.
func score(rs []Result, w Weights) {
	total := float64(w.CPA + w.HookRate + w.Purchases + w.CTR)
	if len(rs) == 0 || total == 0 {
		return
	}
	cpa := places(rs, func(s Stats) float64 { c, _ := s.CPACents(); return -float64(c) })
	rate := places(rs, Stats.HookRate)
	purchases := places(rs, func(s Stats) float64 { return float64(s.Purchases) })
	ctr := places(rs, Stats.CTR)
	for i := range rs {
		rs[i].Score = (float64(w.CPA)*cpa[i] + float64(w.HookRate)*rate[i] +
			float64(w.Purchases)*purchases[i] + float64(w.CTR)*ctr[i]) / total
	}
}

// places returns the place of each result by value, from 0 (lowest) to 1
// (highest). Equal values share the mean of their places, so a metric that
// does not differ between hooks does not change the order.
func places(rs []Result, value func(Stats) float64) []float64 {
	out := make([]float64, len(rs))
	if len(rs) == 1 {
		out[0] = 1
		return out
	}
	for i := range rs {
		vi := value(rs[i].Stats)
		var below, equal int
		for j := range rs {
			switch vj := value(rs[j].Stats); {
			case vj < vi:
				below++
			case vj == vi && j != i:
				equal++
			}
		}
		out[i] = (float64(below) + float64(equal)/2) / float64(len(rs)-1)
	}
	return out
}

// optimisticCPA is the CPA if the hook got one standard deviation more
// purchases.
func (s Stats) optimisticCPA() float64 {
	p := float64(s.Purchases)
	return float64(s.SpendCents) / (p + math.Sqrt(p))
}

func medianHookRate(rs []Result) float64 {
	if len(rs) == 0 {
		return 0
	}
	rates := make([]float64, len(rs))
	for i, r := range rs {
		rates[i] = r.HookRate()
	}
	sort.Float64s(rates)
	return rates[len(rates)/2]
}
