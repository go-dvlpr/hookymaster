// Package metaimport parses the Ads Manager CSV export and sums rows per hook.
package metaimport

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrInvalidFile marks problems with the uploaded file itself.
var ErrInvalidFile = errors.New("invalid file")

const (
	maxRows = 100_000
	bom     = "\uFEFF"
)

const (
	colStart     = "Reporting starts"
	colEnd       = "Reporting ends"
	colAdName    = "Ad name"
	colSpend     = "Amount spent (USD)"
	colImpr      = "Impressions"
	colPlays     = "3-second video plays"
	colClicks    = "Link clicks"
	colPurchases = "Purchases"
)

var requiredColumns = []string{colStart, colEnd, colAdName, colSpend, colImpr, colPlays, colClicks, colPurchases}

var adNamePattern = regexp.MustCompile(`^([A-Z]{2})_([A-Z][A-Z0-9]*)_H(\d+)$`)

// AdName is a parsed ad name such as US_O12_H02.
type AdName struct {
	Market   string
	Original string
	Hook     int
}

// ParseAdName normalizes and parses an ad name; ok is false when the name
// does not follow the convention.
func ParseAdName(raw string) (name AdName, ok bool) {
	m := adNamePattern.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(raw)))
	if m == nil {
		return AdName{}, false
	}
	hook, err := strconv.Atoi(m[3])
	if err != nil || hook <= 0 {
		return AdName{}, false
	}
	return AdName{Market: m[1], Original: m[2], Hook: hook}, true
}

// Counts are raw totals that can be summed safely.
type Counts struct {
	Impressions int64
	SpendCents  int64
	Plays3s     int64
	LinkClicks  int64
	Purchases   int64
}

func (c *Counts) add(o Counts) {
	c.Impressions += o.Impressions
	c.SpendCents += o.SpendCents
	c.Plays3s += o.Plays3s
	c.LinkClicks += o.LinkClicks
	c.Purchases += o.Purchases
}

// HookTotals are the summed counts of one hook in one market.
type HookTotals struct {
	AdName
	Counts
	Rows int
}

// BadName is a row whose ad name does not follow the convention.
type BadName struct {
	AdName string
	Counts
}

// Report is the parsed and aggregated file.
type Report struct {
	PeriodStart time.Time
	PeriodEnd   time.Time
	Rows        int
	Hooks       []HookTotals
	BadNames    []BadName
	Normalized  int // names that matched only after trimming or upper-casing
}

// Parse reads the export and sums rows that belong to the same hook.
func Parse(r io.Reader) (Report, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1

	header, err := cr.Read()
	if err != nil {
		return Report{}, fmt.Errorf("%w: read header: %v", ErrInvalidFile, err)
	}
	idx := make(map[string]int, len(header))
	for i, h := range header {
		idx[strings.TrimSpace(strings.TrimPrefix(h, bom))] = i
	}
	for _, c := range requiredColumns {
		if _, ok := idx[c]; !ok {
			return Report{}, fmt.Errorf("%w: missing column %q", ErrInvalidFile, c)
		}
	}

	var rep Report
	byKey := map[AdName]*HookTotals{}
	var order []AdName
	for line := 2; ; line++ {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Report{}, fmt.Errorf("%w: line %d: %v", ErrInvalidFile, line, err)
		}
		rep.Rows++
		if rep.Rows > maxRows {
			return Report{}, fmt.Errorf("%w: more than %d rows", ErrInvalidFile, maxRows)
		}

		get := func(col string) string {
			if i := idx[col]; i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
			return ""
		}
		counts, err := parseCounts(get)
		if err != nil {
			return Report{}, fmt.Errorf("%w: line %d: %v", ErrInvalidFile, line, err)
		}
		if err := rep.addPeriod(get(colStart), get(colEnd)); err != nil {
			return Report{}, fmt.Errorf("%w: line %d: %v", ErrInvalidFile, line, err)
		}

		raw := get(colAdName)
		name, ok := ParseAdName(raw)
		if !ok {
			rep.BadNames = append(rep.BadNames, BadName{AdName: raw, Counts: counts})
			continue
		}
		if raw != strings.ToUpper(strings.TrimSpace(raw)) {
			rep.Normalized++
		}
		t, seen := byKey[name]
		if !seen {
			t = &HookTotals{AdName: name}
			byKey[name] = t
			order = append(order, name)
		}
		t.add(counts)
		t.Rows++
	}
	if rep.Rows == 0 {
		return Report{}, fmt.Errorf("%w: no rows", ErrInvalidFile)
	}
	for _, k := range order {
		rep.Hooks = append(rep.Hooks, *byKey[k])
	}
	return rep, nil
}

func (rep *Report) addPeriod(start, end string) error {
	s, err := time.Parse(time.DateOnly, start)
	if err != nil {
		return fmt.Errorf("reporting starts %q: %v", start, err)
	}
	e, err := time.Parse(time.DateOnly, end)
	if err != nil {
		return fmt.Errorf("reporting ends %q: %v", end, err)
	}
	if rep.PeriodStart.IsZero() || s.Before(rep.PeriodStart) {
		rep.PeriodStart = s
	}
	if e.After(rep.PeriodEnd) {
		rep.PeriodEnd = e
	}
	return nil
}

func parseCounts(get func(string) string) (Counts, error) {
	var c Counts
	var err error
	if c.SpendCents, err = parseCents(get(colSpend)); err != nil {
		return Counts{}, fmt.Errorf("%s: %v", colSpend, err)
	}
	for _, f := range []struct {
		col string
		dst *int64
	}{
		{colImpr, &c.Impressions},
		{colPlays, &c.Plays3s},
		{colClicks, &c.LinkClicks},
		{colPurchases, &c.Purchases},
	} {
		if *f.dst, err = parseInt(get(f.col)); err != nil {
			return Counts{}, fmt.Errorf("%s: %v", f.col, err)
		}
	}
	return c, nil
}

// parseInt reads a count; Meta leaves zero conversions empty.
func parseInt(s string) (int64, error) {
	s = strings.ReplaceAll(s, ",", "")
	if s == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, fmt.Errorf("negative value %d", n)
	}
	return n, nil
}

// parseCents reads a money amount without going through float.
func parseCents(s string) (int64, error) {
	s = strings.ReplaceAll(s, ",", "")
	if s == "" {
		return 0, nil
	}
	whole, frac, _ := strings.Cut(s, ".")
	if len(frac) > 2 {
		frac = frac[:2]
	}
	for len(frac) < 2 {
		frac += "0"
	}
	w, err := parseInt(whole)
	if err != nil {
		return 0, err
	}
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil || f < 0 {
		return 0, fmt.Errorf("bad amount %q", s)
	}
	return w*100 + f, nil
}
