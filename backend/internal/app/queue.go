package app

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strings"

	"github.com/godvlpr/hookymaster/internal/generation"
	"github.com/godvlpr/hookymaster/internal/store"
)

// Queue statuses.
const (
	QueueReady   = "ready"   // selected, not exported yet
	QueueWaiting = "waiting" // exported, no results yet
	QueueLive    = "live"    // has results
)

// QueueItem is a hook on its way to or in Ads Manager.
type QueueItem struct {
	ID       int64  `json:"id"`
	AdName   string `json:"adName"`
	Text     string `json:"text"`
	OnScreen string `json:"onScreenText"`
	Type     string `json:"type"`
	Note     string `json:"note"`
}

// QueueView is one tab of the production queue with counts for all tabs.
type QueueView struct {
	Counts map[string]int `json:"counts"`
	Items  []QueueItem    `json:"items"`
}

// Queue returns the hooks of one status.
func (s *Service) Queue(ctx context.Context, status string) (QueueView, error) {
	if status != QueueReady && status != QueueWaiting && status != QueueLive {
		return QueueView{}, invalidf("Unknown status %q", status)
	}
	rows, err := s.q.ListQueueHooks(ctx)
	if err != nil {
		return QueueView{}, fmt.Errorf("list queue: %w", err)
	}
	v := QueueView{Counts: map[string]int{QueueReady: 0, QueueWaiting: 0, QueueLive: 0}, Items: []QueueItem{}}
	for _, r := range rows {
		st := queueStatus(r.Status, r.HasMetrics)
		v.Counts[st]++
		if st != status || r.Number == nil {
			continue
		}
		item := QueueItem{
			ID: r.ID, AdName: queueAdName(r.AdName, r.ProjectMarket, r.ProjectCode, *r.Number), Text: r.Text,
			OnScreen: r.OnScreenText, Type: generation.TypeName(deref(r.HookType)),
		}
		switch st {
		case QueueReady:
			if b := deref(r.BasedOn); b != "" {
				item.Note = "Like " + b
			} else if r.Source == "generated" {
				item.Note = "New type"
			}
		case QueueWaiting:
			if r.ExportedAt != nil {
				item.Note = r.ExportedAt.Format("Jan 2")
			}
		}
		v.Items = append(v.Items, item)
	}
	return v, nil
}

func queueStatus(status string, hasMetrics bool) string {
	switch {
	case hasMetrics:
		return QueueLive
	case status == "selected":
		return QueueReady
	default:
		return QueueWaiting
	}
}

// Export marks selected hooks as exported and returns them as CSV.
func (s *Service) Export(ctx context.Context, ids []int64) ([]byte, error) {
	if len(ids) == 0 || len(ids) > 500 {
		return nil, invalidf("choose 1 to 500 hooks")
	}
	var out []byte
	err := s.inTx(ctx, func(q *store.Queries) error {
		marked, err := q.MarkExported(ctx, ids)
		if err != nil {
			return fmt.Errorf("mark exported: %w", err)
		}
		if len(marked) == 0 {
			return fmt.Errorf("%w: None of these hooks is ready to export", ErrConflict)
		}
		rows, err := q.ListHooksByIDs(ctx, marked)
		if err != nil {
			return fmt.Errorf("list exported hooks: %w", err)
		}
		out, err = exportCSV(rows)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.log.Info().Int("hooks", len(ids)).Msg("hooks exported")
	return out, nil
}

func exportCSV(rows []store.ListHooksByIDsRow) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"ad_name", "hook_text", "on_screen_text", "type"}); err != nil {
		return nil, fmt.Errorf("write csv: %w", err)
	}
	for _, r := range rows {
		if r.Number == nil {
			continue
		}
		rec := []string{
			CSVSafe(queueAdName(r.AdName, r.ProjectMarket, r.ProjectCode, *r.Number)),
			CSVSafe(r.Text), CSVSafe(r.OnScreenText), generation.TypeName(deref(r.HookType)),
		}
		if err := w.Write(rec); err != nil {
			return nil, fmt.Errorf("write csv: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("write csv: %w", err)
	}
	return buf.Bytes(), nil
}

// CSVSafe stops spreadsheets from reading a cell as a formula.
func CSVSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func queueAdName(custom *string, market, code string, number int32) string {
	if custom != nil {
		return *custom
	}
	return AdName(market, code, number)
}
