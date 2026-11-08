package app

import (
	"errors"
	"strings"
	"testing"
)

func TestCSVSafe(t *testing.T) {
	tests := map[string]string{
		"=HYPERLINK(\"x\")": "'=HYPERLINK(\"x\")",
		"+1 sock":           "'+1 sock",
		"-5 pairs":          "'-5 pairs",
		"@here":             "'@here",
		"POV: one sock":     "POV: one sock",
		"":                  "",
	}
	for in, want := range tests {
		if got := CSVSafe(in); got != want {
			t.Errorf("CSVSafe(%q): got %q, want %q", in, got, want)
		}
	}
}

func TestAdName(t *testing.T) {
	if got := AdName("US", "O12", 7); got != "US_O12_H07" {
		t.Errorf("got %s", got)
	}
}

func TestNextCode(t *testing.T) {
	tests := []struct {
		in   []string
		want string
	}{
		{nil, "O1"},
		{[]string{"O12", "O18", "O15"}, "O19"},
		{[]string{"O9", "custom"}, "O10"},
	}
	for _, tt := range tests {
		if got := NextCode(tt.in); got != tt.want {
			t.Errorf("NextCode(%v): got %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestCreateProjectValidate(t *testing.T) {
	tests := []struct {
		name    string
		in      CreateProjectInput
		wantErr bool
	}{
		{"valid", CreateProjectInput{Name: "  Gym bag ", Description: "Socks for the gym"}, false},
		{"name only", CreateProjectInput{Name: "Gym bag"}, false},
		{"missing name", CreateProjectInput{Name: "  "}, true},
		{"bad code", CreateProjectInput{Name: "x", Code: "12"}, true},
		{"explicit code", CreateProjectInput{Name: "x", Code: "o19"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := tt.in
			err := in.validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("got %v", err)
			}
			if err != nil && !errors.Is(err, ErrInvalid) {
				t.Errorf("error must wrap ErrInvalid: %v", err)
			}
			if err == nil && in.Name != strings.TrimSpace(tt.in.Name) {
				t.Errorf("name is not trimmed: %q", in.Name)
			}
		})
	}
}

func TestSettingsValidate(t *testing.T) {
	valid := Settings{Code: "O12", Name: "x", MinImpressions: 3000, MinPurchases: 20, MaxCPACents: 5400, RefSharePct: 15, MinRefs: 3, MaxRefs: 10, MaxAnti: 5,
		WeightCPA: 60, WeightHookRate: 25, WeightPurchases: 10, WeightCTR: 5}
	tests := []struct {
		name    string
		mod     func(*Settings)
		wantErr bool
	}{
		{"valid", func(*Settings) {}, false},
		{"min refs above max", func(s *Settings) { s.MinRefs = 11 }, true},
		{"zero share", func(s *Settings) { s.RefSharePct = 0 }, true},
		{"no max cpa", func(s *Settings) { s.MaxCPACents = 0 }, true},
		{"negative minimum", func(s *Settings) { s.MinPurchases = -1 }, true},
		{"CPA only", func(s *Settings) { s.WeightCPA, s.WeightHookRate, s.WeightPurchases, s.WeightCTR = 100, 0, 0, 0 }, false},
		{"weights under 100", func(s *Settings) { s.WeightCTR = 0 }, true},
		{"negative weight", func(s *Settings) { s.WeightCPA, s.WeightCTR = 70, -5 }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := valid
			tt.mod(&st)
			if err := st.validate(); (err != nil) != tt.wantErr {
				t.Errorf("got %v", err)
			}
		})
	}
}

func TestGenerateInputValidate(t *testing.T) {
	tests := []struct {
		name    string
		in      GenerateInput
		wantErr bool
	}{
		{"valid", GenerateInput{N: 20, ExploitPct: 70}, false},
		{"type shares", GenerateInput{N: 20, ExploitPct: 70, TypeShares: map[string]int{"pov": 40, "gift": 30}}, false},
		{"shares over 100", GenerateInput{N: 20, TypeShares: map[string]int{"pov": 60, "gift": 50}}, true},
		{"unknown type", GenerateInput{N: 20, TypeShares: map[string]int{"meme": 10}}, true},
		{"too many", GenerateInput{N: 101}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := tt.in
			if err := in.validate(); (err != nil) != tt.wantErr {
				t.Errorf("got %v", err)
			}
		})
	}
}

func TestQueueStatus(t *testing.T) {
	tests := []struct {
		status     string
		hasMetrics bool
		want       string
	}{
		{"selected", false, QueueReady},
		{"exported", false, QueueWaiting},
		{"exported", true, QueueLive},
	}
	for _, tt := range tests {
		if got := queueStatus(tt.status, tt.hasMetrics); got != tt.want {
			t.Errorf("queueStatus(%s, %v): got %s, want %s", tt.status, tt.hasMetrics, got, tt.want)
		}
	}
}

func TestScaleTargets(t *testing.T) {
	targets := map[string]int{"pov": 20, "gift": 2}
	got := scaleTargets(targets, 25, 100)
	if got["pov"] != 5 || got["gift"] != 1 {
		t.Errorf("got %v, want pov 5, gift 1", got)
	}
	if got := scaleTargets(targets, 10, 10); got["pov"] != 20 {
		t.Errorf("one chunk must keep the targets, got %v", got)
	}
}
