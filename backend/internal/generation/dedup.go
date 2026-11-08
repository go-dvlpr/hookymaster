package generation

import (
	"strings"
	"unicode"
)

// DuplicateThreshold is the text similarity above which two hooks count as
// the same idea. Tuned on the test hooks.
const DuplicateThreshold = 0.6

// Duplicate is a candidate dropped because it repeats an existing hook.
type Duplicate struct {
	Text       string  `json:"text"`
	SimilarTo  string  `json:"similar_to"`
	Similarity float64 `json:"similarity"`
}

// Normalize lowercases text and keeps only letters, digits and single spaces.
func Normalize(s string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			space = false
		case !space && b.Len() > 0:
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

// Similarity is the larger of word and character-trigram Jaccard similarity
// of the normalized texts.
func Similarity(a, b string) float64 {
	na, nb := Normalize(a), Normalize(b)
	if na == nb {
		return 1
	}
	return max(jaccard(words(na), words(nb)), jaccard(trigrams(na), trigrams(nb)))
}

// FilterDuplicates drops candidates similar to any existing hook or to a
// candidate kept earlier in the same batch.
func FilterDuplicates(cands []Candidate, existing []string) (kept []Candidate, dropped []Duplicate) {
	seen := append([]string(nil), existing...)
	for _, c := range cands {
		best, bestText := 0.0, ""
		for _, e := range seen {
			if s := Similarity(c.Text, e); s > best {
				best, bestText = s, e
			}
		}
		if best >= DuplicateThreshold {
			dropped = append(dropped, Duplicate{Text: c.Text, SimilarTo: bestText, Similarity: best})
			continue
		}
		kept = append(kept, c)
		seen = append(seen, c.Text)
	}
	return kept, dropped
}

func words(s string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, w := range strings.Fields(s) {
		set[w] = struct{}{}
	}
	return set
}

func trigrams(s string) map[string]struct{} {
	set := map[string]struct{}{}
	r := []rune(" " + s + " ")
	for i := 0; i+3 <= len(r); i++ {
		set[string(r[i:i+3])] = struct{}{}
	}
	return set
}

func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}
	return float64(inter) / float64(len(a)+len(b)-inter)
}
