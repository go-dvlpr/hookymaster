package generation

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrBadOutput means the model's answer could not be used.
var ErrBadOutput = errors.New("bad model output")

const (
	maxHookWords   = 15
	maxScreenWords = 8
)

// Candidate is one generated hook after validation.
type Candidate struct {
	Text         string `json:"text"`
	OnScreenText string `json:"on_screen_text"`
	Type         string `json:"type"`
	Pain         string `json:"pain"`
	Emotion      string `json:"emotion"`
	Mode         Mode   `json:"mode"`
	BasedOn      string `json:"based_on"`
	Rationale    string `json:"rationale"`
}

// Rejected is a candidate that failed validation, with the reason.
type Rejected struct {
	Text   string `json:"text"`
	Reason string `json:"reason"`
}

// ParseCandidates decodes the model answer and keeps only valid candidates.
func ParseCandidates(raw []byte) (ok []Candidate, rejected []Rejected, err error) {
	var out struct {
		Candidates []Candidate `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrBadOutput, err)
	}
	for _, c := range out.Candidates {
		c.Text = strings.TrimSpace(c.Text)
		c.OnScreenText = strings.TrimSpace(c.OnScreenText)
		// A label outside the taxonomy says nothing about the hook itself:
		// keep the hook without labels, it is labeled later.
		if !known(Types, c.Type) || !known(Pains, c.Pain) || !known(Emotions, c.Emotion) {
			c.Type, c.Pain, c.Emotion = "", "", ""
		}
		if reason := invalid(c); reason != "" {
			rejected = append(rejected, Rejected{Text: c.Text, Reason: reason})
			continue
		}
		ok = append(ok, c)
	}
	return ok, rejected, nil
}

func invalid(c Candidate) string {
	switch {
	case c.Text == "":
		return "empty hook"
	case len(strings.Fields(c.Text)) > maxHookWords:
		return "hook is too long"
	case len(strings.Fields(c.OnScreenText)) > maxScreenWords:
		return "on-screen text is too long"
	case c.Mode != ModeExploit && c.Mode != ModeExplore:
		return "unknown mode"
	}
	return ""
}

// Labels are taxonomy codes for one hook.
type Labels struct {
	ID      int64  `json:"id"`
	Type    string `json:"type"`
	Pain    string `json:"pain"`
	Emotion string `json:"emotion"`
}

// ParseLabels decodes classification output and drops unknown codes.
func ParseLabels(raw []byte) ([]Labels, error) {
	var out struct {
		Labels []Labels `json:"labels"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadOutput, err)
	}
	var ok []Labels
	for _, l := range out.Labels {
		if known(Types, l.Type) && known(Pains, l.Pain) && known(Emotions, l.Emotion) {
			ok = append(ok, l)
		}
	}
	return ok, nil
}
