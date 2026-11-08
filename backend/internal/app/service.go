// Package app holds the use cases of Hookymaster: projects, imports,
// generation, review and the production queue.
package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	"github.com/rs/zerolog"

	"github.com/godvlpr/hookymaster/internal/generation"
	"github.com/godvlpr/hookymaster/internal/ranking"
	"github.com/godvlpr/hookymaster/internal/store"
)

var (
	// ErrNotFound means the requested entity does not exist.
	ErrNotFound = errors.New("not found")
	// ErrInvalid means the input breaks a rule; the message is safe to show.
	ErrInvalid = errors.New("invalid input")
	// ErrConflict means the entity is not in a state that allows the action.
	ErrConflict = errors.New("conflict")
	// ErrNoModel means generation was requested without an LLM configured.
	ErrNoModel = errors.New("language model is not configured")
)

// LLM returns a JSON object for a system and a user message.
type LLM interface {
	JSON(ctx context.Context, system, user string) ([]byte, error)
	Model() string
}

// Embedder turns texts into vectors for duplicate search.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Service implements the use cases. LLM and Embedder are optional.
type Service struct {
	pool  *pgxpool.Pool
	q     *store.Queries
	llm   LLM
	emb   Embedder
	log   zerolog.Logger
	rules ranking.Rules
}

// New creates a Service. llm and emb may be nil.
func New(pool *pgxpool.Pool, llm LLM, emb Embedder, log zerolog.Logger) *Service {
	return &Service{pool: pool, q: store.New(pool), llm: llm, emb: emb, log: log, rules: ranking.DefaultRules}
}

func (s *Service) inTx(ctx context.Context, fn func(q *store.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func notFound(err error, what string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s was not found", ErrNotFound, what)
	}
	return fmt.Errorf("get %s: %w", what, err)
}

// AdName builds the ad name UA uses in Ads Manager.
func AdName(market, code string, number int32) string {
	return fmt.Sprintf("%s_%s_H%02d", market, code, number)
}

func hookLabel(number *int32) string {
	if number == nil {
		return ""
	}
	return fmt.Sprintf("H%02d", *number)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func ptr[T any](v T) *T { return &v }

// enrich labels hooks without labels and stores embeddings. Failures are
// logged and do not fail the caller: both are improvements, not data.
func (s *Service) enrich(ctx context.Context, hooks []store.Hook) {
	if len(hooks) == 0 {
		return
	}
	if s.llm != nil {
		var todo []generation.HookText
		for _, h := range hooks {
			if h.HookType == nil {
				todo = append(todo, generation.HookText{ID: h.ID, Text: h.Text})
			}
		}
		if err := s.classify(ctx, todo); err != nil {
			s.log.Warn().Err(err).Int("hooks", len(todo)).Msg("classify hooks")
		}
	}
	if s.emb != nil {
		if err := s.embed(ctx, hooks); err != nil {
			s.log.Warn().Err(err).Int("hooks", len(hooks)).Msg("embed hooks")
		}
	}
}

func (s *Service) classify(ctx context.Context, hooks []generation.HookText) error {
	const chunk = 50
	for len(hooks) > 0 {
		part := hooks[:min(chunk, len(hooks))]
		hooks = hooks[len(part):]

		system, user, err := generation.BuildClassifyPrompt(part)
		if err != nil {
			return err
		}
		raw, err := s.llm.JSON(ctx, system, user)
		if err != nil {
			return fmt.Errorf("classify: %w", err)
		}
		labels, err := generation.ParseLabels(raw)
		if err != nil {
			return fmt.Errorf("classify: %w", err)
		}
		wanted := make(map[int64]bool, len(part))
		for _, h := range part {
			wanted[h.ID] = true
		}
		for _, l := range labels {
			if !wanted[l.ID] {
				continue // the model must not label hooks it was not given
			}
			if err := s.q.SetHookLabels(ctx, store.SetHookLabelsParams{
				ID: l.ID, HookType: ptr(l.Type), Pain: ptr(l.Pain), Emotion: ptr(l.Emotion),
			}); err != nil {
				return fmt.Errorf("set labels of hook %d: %w", l.ID, err)
			}
		}
	}
	return nil
}

func (s *Service) embed(ctx context.Context, hooks []store.Hook) error {
	texts := make([]string, len(hooks))
	for i, h := range hooks {
		texts[i] = embedText(h.Text, h.OnScreenText)
	}
	vecs, err := s.emb.Embed(ctx, texts)
	if err != nil {
		return err
	}
	for i, h := range hooks {
		v := pgvector.NewVector(vecs[i])
		if err := s.q.SetHookEmbedding(ctx, store.SetHookEmbeddingParams{ID: h.ID, Embedding: &v}); err != nil {
			return fmt.Errorf("set embedding of hook %d: %w", h.ID, err)
		}
	}
	return nil
}

func embedText(text, onScreen string) string {
	return strings.TrimSpace(text + "\n" + onScreen)
}
