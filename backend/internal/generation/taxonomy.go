// Package generation builds prompts for new hooks, validates the model's
// answer and filters out duplicates. It has no I/O of its own.
package generation

// Label is a code from a fixed list plus a name for people.
type Label struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// Types, Pains and Emotions are the hook taxonomy.
var (
	Types = []Label{
		{"question", "Question"},
		{"pov", "POV"},
		{"statistic", "Statistic"},
		{"challenge", "Challenge"},
		{"demo", "Demo"},
		{"testimonial", "Testimonial"},
		{"before_after", "Before / after"},
		{"myth_busting", "Myth busting"},
		{"curiosity", "Curiosity"},
		{"listicle", "List"},
		{"callout", "Audience callout"},
		{"gift", "Gift"},
		{"offer", "Offer"},
	}
	Pains = []Label{
		{"time", "Takes time"},
		{"money", "Costs money"},
		{"effort", "Effort or hassle"},
		{"health", "Health or comfort"},
		{"looks", "Looks or status"},
		{"relationships", "Family or relationships"},
		{"risk", "Fear or risk"},
		{"none", "None"},
	}
	Emotions = []Label{
		{"frustration", "Frustration"},
		{"humor", "Humor"},
		{"surprise", "Surprise"},
		{"relief", "Relief"},
		{"warmth", "Warmth"},
		{"neutral", "Neutral"},
	}
)

func codes(labels []Label) []string {
	out := make([]string, len(labels))
	for i, l := range labels {
		out[i] = l.Code
	}
	return out
}

func known(labels []Label, code string) bool {
	for _, l := range labels {
		if l.Code == code {
			return true
		}
	}
	return false
}

// TypeName returns the display name of a type code.
func TypeName(code string) string {
	for _, l := range Types {
		if l.Code == code {
			return l.Name
		}
	}
	return code
}
