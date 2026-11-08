package generation

// Mode says whether a candidate follows a reference or tries a new type.
type Mode string

const (
	ModeExploit Mode = "exploit"
	ModeExplore Mode = "explore"
)

// Slot is a group of candidates the model must write.
type Slot struct {
	Mode    Mode   `json:"mode"`
	BasedOn string `json:"based_on,omitempty"` // reference hook, e.g. H02
	Type    string `json:"type,omitempty"`     // explore type code
	Count   int    `json:"count"`
}

// Plan splits n candidates between references and explore types.
// exploitPct is clamped to 0..100; without explore types everything goes
// to the references, and without references everything goes to explore.
func Plan(n, exploitPct int, refs, exploreTypes []string) []Slot {
	if n <= 0 || (len(refs) == 0 && len(exploreTypes) == 0) {
		return nil
	}
	exploitPct = min(max(exploitPct, 0), 100)
	switch {
	case len(exploreTypes) == 0:
		exploitPct = 100
	case len(refs) == 0:
		exploitPct = 0
	}
	exploitN := (n*exploitPct + 50) / 100

	var slots []Slot
	for i, c := range split(exploitN, len(refs)) {
		if c > 0 {
			slots = append(slots, Slot{Mode: ModeExploit, BasedOn: refs[i], Count: c})
		}
	}
	for i, c := range split(n-exploitN, len(exploreTypes)) {
		if c > 0 {
			slots = append(slots, Slot{Mode: ModeExplore, Type: exploreTypes[i], Count: c})
		}
	}
	return slots
}

// split spreads total over k parts, earlier parts get the remainder.
func split(total, k int) []int {
	if k == 0 {
		return nil
	}
	out := make([]int, k)
	for i := range out {
		out[i] = total / k
		if i < total%k {
			out[i]++
		}
	}
	return out
}

// Chunk splits a plan into parts of at most size candidates, so a large
// batch can be written by several smaller model calls.
func Chunk(slots []Slot, size int) [][]Slot {
	if size <= 0 {
		return nil
	}
	var chunks [][]Slot
	var cur []Slot
	free := size
	for _, s := range slots {
		for s.Count > 0 {
			part := s
			part.Count = min(s.Count, free)
			cur = append(cur, part)
			s.Count -= part.Count
			free -= part.Count
			if free == 0 {
				chunks = append(chunks, cur)
				cur, free = nil, size
			}
		}
	}
	if len(cur) > 0 {
		chunks = append(chunks, cur)
	}
	return chunks
}

// Count returns the number of candidates in a plan.
func Count(slots []Slot) int {
	n := 0
	for _, s := range slots {
		n += s.Count
	}
	return n
}
