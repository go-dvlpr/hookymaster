// Command hookymaster runs the API and web UI, or seeds test data.
//
//	hookymaster            start the server
//	hookymaster seed DIR   load projects, hooks and a Meta export from DIR
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
	"github.com/rs/zerolog"

	"github.com/godvlpr/hookymaster/internal/api"
	"github.com/godvlpr/hookymaster/internal/app"
	"github.com/godvlpr/hookymaster/internal/openai"
	"github.com/godvlpr/hookymaster/internal/store"
)

type config struct {
	databaseURL  string
	addr         string
	webDir       string
	openAIKey    string
	openAIModel  string
	openAIEmbeds string
}

func loadConfig() (config, error) {
	c := config{
		databaseURL:  os.Getenv("DATABASE_URL"),
		addr:         ":" + envOr("PORT", "8080"),
		webDir:       os.Getenv("WEB_DIR"),
		openAIKey:    os.Getenv("OPENAI_API_KEY"),
		openAIModel:  envOr("OPENAI_MODEL", "gpt-4.1-mini"),
		openAIEmbeds: envOr("OPENAI_EMBED_MODEL", "text-embedding-3-small"),
	}
	if c.databaseURL == "" {
		return config{}, errors.New("DATABASE_URL is required")
	}
	return c, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	log := zerolog.New(os.Stdout).With().Timestamp().Logger()
	if err := run(log); err != nil {
		log.Error().Err(err).Msg("exit")
		os.Exit(1)
	}
}

func run(log zerolog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	pool, err := openDB(ctx, cfg.databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	var llm app.LLM
	var emb app.Embedder
	if cfg.openAIKey != "" {
		c := openai.New(cfg.openAIKey, cfg.openAIModel, cfg.openAIEmbeds)
		llm, emb = c, c
		log.Info().Str("model", cfg.openAIModel).Str("embeddings", cfg.openAIEmbeds).Msg("openai enabled")
	} else {
		log.Warn().Msg("OPENAI_API_KEY is not set: generation, labels and vector search are off")
	}
	svc := app.New(pool, llm, emb, log)

	if len(os.Args) > 1 && os.Args[1] == "seed" {
		dir := "testdata"
		if len(os.Args) > 2 {
			dir = os.Args[2]
		}
		return seed(ctx, svc, dir, log)
	}
	return serve(ctx, svc, cfg, log)
}

// openDB applies migrations, then opens a pool that knows the vector type.
func openDB(ctx context.Context, url string) (*pgxpool.Pool, error) {
	boot, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	err = waitForDB(ctx, boot)
	if err == nil {
		err = store.Migrate(boot)
	}
	boot.Close()
	if err != nil {
		return nil, err
	}

	pcfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	pcfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return pgxvec.RegisterTypes(ctx, conn)
	}
	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	return pool, nil
}

// waitForDB gives a starting Postgres container up to 30 seconds.
func waitForDB(ctx context.Context, pool *pgxpool.Pool) error {
	var err error
	for range 30 {
		if err = pool.Ping(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("database is not reachable: %w", err)
}

func serve(ctx context.Context, svc *app.Service, cfg config, log zerolog.Logger) error {
	srv := api.New(svc, log, cfg.webDir)
	errc := make(chan error, 1)
	go func() {
		log.Info().Str("addr", cfg.addr).Msg("listening")
		errc <- srv.Listen(cfg.addr)
	}()

	select {
	case err := <-errc:
		return fmt.Errorf("listen: %w", err)
	case <-ctx.Done():
	}
	log.Info().Msg("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.ShutdownWithContext(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return <-errc
}

func seed(ctx context.Context, svc *app.Service, dir string, log zerolog.Logger) error {
	raw, err := os.ReadFile(filepath.Join(dir, "seed", "projects.json"))
	if err != nil {
		return fmt.Errorf("read seed projects: %w", err)
	}
	var projects []app.CreateProjectInput
	if err := json.Unmarshal(raw, &projects); err != nil {
		return fmt.Errorf("decode seed projects: %w", err)
	}
	for _, p := range projects {
		code, err := svc.CreateProject(ctx, p)
		if errors.Is(err, app.ErrConflict) {
			log.Info().Str("project", p.Code).Msg("project exists, skipped")
			continue
		}
		if err != nil {
			return fmt.Errorf("create project %s: %w", p.Code, err)
		}
		log.Info().Str("project", code).Msg("project seeded")
	}

	hooks, err := os.Open(filepath.Join(dir, "meta_ads", "hooks_import.csv"))
	if err != nil {
		return fmt.Errorf("open hooks file: %w", err)
	}
	defer hooks.Close()
	hres, err := svc.ImportHooks(ctx, hooks)
	if err != nil {
		return fmt.Errorf("import hooks: %w", err)
	}
	log.Info().Int("created", hres.Created).Int("skipped", hres.Skipped).Strs("errors", hres.Errors).Msg("hooks seeded")

	exportPath := filepath.Join(dir, "meta_ads", "ads_manager_export_2026-10.csv")
	for _, p := range projects {
		export, err := os.Open(exportPath)
		if err != nil {
			return fmt.Errorf("open meta export: %w", err)
		}
		imp, err := svc.ImportMeta(ctx, p.Code, filepath.Base(exportPath), export)
		export.Close()
		if err != nil {
			return fmt.Errorf("import meta export into %s: %w", p.Code, err)
		}
		log.Info().Str("project", p.Code).Int("matched", imp.RowsMatched).Int("created", imp.HooksCreated).Msg("meta export seeded")
	}
	return nil
}
