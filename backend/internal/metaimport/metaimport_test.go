package metaimport

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAdName(t *testing.T) {
	tests := []struct {
		in     string
		want   AdName
		wantOK bool
	}{
		{"US_O12_H02", AdName{"US", "O12", 2}, true},
		{"  us_o15_h03 ", AdName{"US", "O15", 3}, true},
		{"UK_O12_H01", AdName{"UK", "O12", 1}, true},
		{"Socks_magnet_final_v2_NEW", AdName{}, false},
		{"US_O12_H00", AdName{}, false},
		{"USA_O12_H01", AdName{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := ParseAdName(tt.in)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("got %+v %v, want %+v %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestParseTestdataExport(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "..", "testdata", "meta_ads", "ads_manager_export_2026-10.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	rep, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Rows != 51 {
		t.Errorf("rows: got %d, want 51", rep.Rows)
	}
	if len(rep.BadNames) != 1 || rep.BadNames[0].AdName != "Socks_magnet_final_v2_NEW" {
		t.Errorf("bad names: got %+v", rep.BadNames)
	}
	if rep.Normalized != 1 {
		t.Errorf("normalized: got %d, want 1", rep.Normalized)
	}
	if got := rep.PeriodStart.Format("2006-01-02") + ".." + rep.PeriodEnd.Format("2006-01-02"); got != "2026-10-01..2026-10-31" {
		t.Errorf("period: got %s", got)
	}

	byName := map[AdName]HookTotals{}
	for _, h := range rep.Hooks {
		byName[h.AdName] = h
	}
	// O12 H01 runs in two ad sets and must be summed.
	h01 := byName[AdName{"US", "O12", 1}]
	if h01.Rows != 2 || h01.Impressions != 314353 || h01.Purchases != 97 {
		t.Errorf("O12 H01: got %+v", h01)
	}
	// The lower-case row is merged into O15 H03.
	if h := byName[AdName{"US", "O15", 3}]; h.Rows != 2 {
		t.Errorf("O15 H03 rows: got %d, want 2", h.Rows)
	}
	// UK stays separate from US.
	if _, ok := byName[AdName{"UK", "O12", 1}]; !ok {
		t.Error("UK O12 H01 missing")
	}
	// Rows with empty purchases are read as zero.
	if h := byName[AdName{"US", "O12", 5}]; h.Purchases != 0 || h.Impressions == 0 {
		t.Errorf("O12 H05: got %+v", h)
	}
}

func TestParseMissingColumn(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "..", "testdata", "meta_ads", "ads_manager_export_missing_column.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	_, err = Parse(f)
	if !errors.Is(err, ErrInvalidFile) || !strings.Contains(err.Error(), "3-second video plays") {
		t.Errorf("got %v", err)
	}
}

func TestParseInvalid(t *testing.T) {
	header := "Reporting starts,Reporting ends,Ad name,Amount spent (USD),Impressions,3-second video plays,Link clicks,Purchases\n"
	tests := []struct {
		name string
		body string
	}{
		{"empty file", ""},
		{"no rows", header},
		{"bad number", header + "2026-10-01,2026-10-31,US_O12_H01,10.00,abc,1,1,1\n"},
		{"bad date", header + "01/10/2026,2026-10-31,US_O12_H01,10.00,1,1,1,1\n"},
		{"negative", header + "2026-10-01,2026-10-31,US_O12_H01,10.00,-5,1,1,1\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse(strings.NewReader(tt.body)); !errors.Is(err, ErrInvalidFile) {
				t.Errorf("got %v, want ErrInvalidFile", err)
			}
		})
	}
}

func TestParseCents(t *testing.T) {
	tests := map[string]int64{"3000.00": 300000, "25.2": 2520, "1,149.50": 114950, "": 0, "7": 700, "0.999": 99}
	for in, want := range tests {
		got, err := parseCents(in)
		if err != nil || got != want {
			t.Errorf("parseCents(%q): got %d, %v; want %d", in, got, err, want)
		}
	}
}
