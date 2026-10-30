# agents.md — rules for anyone (human or ai) working in this repo

this file is loaded automatically. read it before touching code.

## read first, in this order

1. `docs/DECISIONS.md` — what was decided and why. append-only.
2. `docs/BUSINESS-RULES.md` — how the dealership actually works, and the anti-cheat controls.
3. `docs/ARCHITECTURE.md` — target shape: folders, tenancy, auth, pdf, deploy.
4. `docs/PLAN.md` — the current step and its acceptance criteria.

if the task is not covered by the current step, say so instead of inventing scope.

## hard rules

- **`main` is production.** it runs the streamlit app the business bills on. do not modify `web_app.py`, `invoice_app.py`, `api/`, `client/` on this branch, and do not merge this branch into `main` before cutover.
- **the rewrite lives on `rewrite/go-htmx`.** `feat/erp-ts` is archived reference only, never merged.
- **no code without a caller.** no `utils`, `helpers`, `models`, `types` dumping grounds. no interface with one implementation. no TODO/FIXME. no dead flags or placeholder screens.
- **one step at a time.** a step ends only when its acceptance test in `docs/PLAN.md` passes. update the status in the same commit.
- **decisions are written, not remembered.** if a choice changes, append a new entry to `docs/DECISIONS.md` and mark the old one superseded. never edit history there.
- **money is `int64` cents.** never float, never decimal strings in go. parse at the edge, format at the edge.
- **every tenant table has `tenant_id`** and every query is scoped by it. every new table gets rls.
- **every write is audited in the same transaction.** no exceptions, including "small" edits.
- **nothing is deleted.** soft delete, void with reason, reversal with reason.
- **tests for money, numbering, permissions, tenant isolation and pdf output** are mandatory; they are the four places where a bug costs the business money.
- **no secrets in the repo.** env only. `.env` is gitignored.
- **no new dependency** without a one-line reason in the commit message. stdlib first.

## definition of done for any step

```
go vet ./...
go test ./...
templ generate -fail -f *.templ   (or: templ generate -fail ./...)
golangci-lint run ./...           (if installed)
```

plus: server boots, the new screen works on a 360px wide viewport, and the step's acceptance test in `docs/PLAN.md` is true.

## commands

```bash
go run ./cmd/server            # local server (needs .env)
go test ./...                  # all tests
templ generate                 # after editing .templ files
go run github.com/pressly/goose/v3/cmd/goose -dir migrations postgres "$DATABASE_URL" up
sqlc generate                  # after editing queries
```

## current state

step 1: the skeleton exists (`cmd/server`, `internal/config`, `internal/db`, `internal/httpx`, `internal/web`) and is green locally. the next action is the railway staging deploy, then step 2 as specified in `docs/PLAN.md`.

## reference material

- `C:\Users\Gunawardana Motors\Downloads\BIBILE 01.docx` — the leasing company's proforma, ground truth for the proforma pdf (copy into `internal/pdf/testdata/` at step 4).
- `web_app.py` — the live behaviour being replaced (read only, do not change).
- `feat/erp-ts` — earlier attempt; useful for the drizzle schema shape, the transaction guards, and the derived proforma geometry.
