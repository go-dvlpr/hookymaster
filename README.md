# Hookymaster

A tool for motion designers and UA teams that make video ads. A project is one ad concept: its hooks,
their Meta Ads results and the next batches. Hookymaster ranks hooks by CPA,
generates new hooks on the best ones plus fresh ones, and hands the selected
hooks to production with ready ad names.

Test report: [docs/learning-tests.pdf](docs/learning-tests.pdf).

## Stack

| Part | Choice |
|---|---|
| Backend | Go, Fiber v3 |
| Database | PostgreSQL 17 + pgvector, sqlc, goose migrations |
| Frontend | React 19, Vite, TypeScript |
| Models | OpenAI: chat model for generation and labels, embeddings for duplicate search |
| Runtime | Docker Compose |

## Run

```bash
cp .env.example .env        # put your OPENAI_API_KEY there
docker compose up --build -d
docker compose run --rm app seed /app/testdata   # optional: three test projects with results
```

Open http://localhost:8080.

Without `OPENAI_API_KEY` everything works except generation, hook labels and
vector duplicate search.

## Develop

```bash
docker compose up -d db
export DATABASE_URL='postgres://hookymaster:hookymaster@127.0.0.1:5434/hookymaster?sslmode=disable'
cd backend
go run ./cmd/hookymaster seed ../testdata
go run ./cmd/hookymaster                    # API on :8080
cd ../frontend && npm install && npm run dev # UI on :5173, proxies /api to :8080
```

After changing SQL in `backend/internal/store/queries` run `sqlc generate` in `backend`.

## Tests

```bash
cd backend && go test ./...
```

Database tests run when `TEST_DATABASE_URL` points to an empty database:

```bash
docker compose exec db psql -U hookymaster -c 'CREATE DATABASE hookymaster_test'
TEST_DATABASE_URL='postgres://hookymaster:hookymaster@127.0.0.1:5434/hookymaster_test?sslmode=disable' go test ./...   # in backend
```

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `DATABASE_URL` | — | Postgres connection string, required |
| `PORT` | `8080` | HTTP port |
| `WEB_DIR` | empty | Folder with the built UI; empty serves the API only |
| `OPENAI_API_KEY` | empty | Turns on generation, labels and vector search |
| `OPENAI_MODEL` | `gpt-4.1-mini` | Chat model for generation and labels |
| `OPENAI_EMBED_MODEL` | `text-embedding-3-small` | Embedding model, 1536 dimensions |

## Layout

```
backend/                       Go module: API server and seed command
  cmd/hookymaster              entry point
  internal/api                 Fiber handlers and error mapping
  internal/app                 use cases: projects, imports, generation, review, queue
  internal/ranking             tiers by CPA (pure)
  internal/metaimport          Ads Manager CSV parsing and aggregation (pure)
  internal/generation          plan, prompts, output validation, text duplicates (pure)
  internal/openai              minimal OpenAI client
  internal/store               sqlc code, queries and migrations
frontend/                      React UI
docs/                          test report
testdata/                      synthetic projects, hooks and Meta export
Dockerfile, docker-compose.yml one image with the API and the built UI
```

## Flow

1. Create a project: name and description. It gets a code, e.g. `O12`.
2. Add the hooks you already run (Hooks tab). Each gets an H-number and an ad
   name such as `US_O12_H03`. Use that name for the ad in Meta Ads Manager.
3. Upload the Ads Manager export in the project's Import tab. Rows go to the
   hook with the same H-number. Other ad names, including numbers the project
   does not have yet, are listed after the import: pick a hook or New hook
   for each. Rows of other projects are collapsed and skipped unless you pick.
4. If an ad is named differently in Meta, set that name on the hook and upload
   the file again; unrecognized names are listed after each import.
5. Settings tab: thresholds and how many top hooks become references.
6. Generate: batch size, the split between hooks built on existing ones and
   new ones, and an optional share per hook type.
7. Review: select (gets the next H-number and ad name) or reject.
8. Production queue: download the CSV for designers. Exported hooks wait for
   results and move to Live after the next import.
