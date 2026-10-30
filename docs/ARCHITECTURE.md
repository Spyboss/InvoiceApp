# architecture

target shape of the v2 rewrite. code follows this; if it cannot, we change this file first.

## 1. runtime

one static go binary. it serves html (templ), static assets (embedded), json only where needed (htmx fragments are html), pdfs (proxied through gotenberg), and exposes `/healthz`. no node, no bundler, no client framework.

```
browser ── https ──> cloudflare (dns, waf, tls)
                          │
                          ▼
                    railway: app (go, :8080)
                          ├── postgres  (private network)
                          └── gotenberg (private network, chromium)
```

tenant resolution: host header → `tenants.domain` (cached in memory, invalidated on change). `localhost` uses `TENANT_DEV` from env. unknown host → 404, never a default tenant.

## 2. folders

```
cmd/server/            main: config, db, router, graceful shutdown
internal/
  config/              env parsing, fail fast
  db/                  pgx pool, tx helper, sqlc generated code
  db/queries/          *.sql (sqlc source of truth)
  auth/                password hashing, sessions, csrf, permissions
  tenant/              resolution, context, rls session vars
  audit/               append-only write log
  money/               cents parsing/formatting
  numbering/           atomic series allocation
  documents/           invoice, quotation, proforma, receipt domain logic
  stock/               vehicles + movement ledger
  payments/            receipts, allocation, reversal
  reports/             day end, sales, stock, receivables
  pdf/                 html templates -> gotenberg client -> archive
  httpx/               middleware: request id, logging, recover, headers
  web/                 handlers, templ views, static assets (embed)
migrations/            goose sql, append-only
docs/                  this folder
```

rules: `internal/web` may import services; services may import `db`, `money`, `audit`; nothing imports `web`. no package named `utils`, `helpers`, `models`, `types`.

## 3. request lifecycle

1. cloudflare → railway → `httpx` middleware chain (request id, real ip, logger, recover, security headers, hsts).
2. tenant resolve → `tenant_id` in context.
3. session load (cookie → `sessions` row) → user + permissions in context.
4. csrf check on any non-GET.
5. permission check in handler.
6. service call inside a transaction: `SET LOCAL app.tenant_id = ...` then queries (rls enforces too).
7. write → audit row in the same transaction.
8. render templ fragment (htmx) or full page.

anything that writes must be one transaction that includes its audit row.

## 4. data

- postgres 18, `uuidv7()` primary keys, `bigint` cents, `timestamptz`.
- every business table: `tenant_id`, `created_at`, `created_by`, `updated_at`, `deleted_at`.
- composite unique indexes always include `tenant_id` (e.g. `(tenant_id, chassis_no)`).
- rls enabled on every tenant table: `using (tenant_id = current_setting('app.tenant_id')::uuid)`. platform admin runs through a separate, audited path that sets `app.is_platform=true`.
- migrations via goose, forward only, one file per step, tested up and down on a scratch db in ci.
- queries via sqlc. no orm, no string-built sql.

## 5. documents and pdf

html template per document type, rendered by go, posted to gotenberg `/forms/chromium/convert/html` with everything inlined (base64 images, embedded font). page geometry is set per template (`@page size` and margins).

- sales/quotation/receipt/report templates: a4, responsive content, brand header, table, totals, signatures.
- proforma: fixed letter page, absolute positions derived from `BIBILE 01.docx` twips; singer wordmark as svg, letterhead as jpeg; calibri metrics via carlito bundled in the gotenberg image.
- output is stored: `documents.pdf_sha256`, pdf bytes in r2 under `tenant/type/number.pdf`. regeneration must produce the same bytes for the same inputs (fonts and version pinned).
- gotenberg is stateless; the app owns storage.

## 6. auth

argon2id (owasp params), server-side sessions table (`token_hash`, user, ip, user agent, idle and absolute expiry, revoked_at). cookie `__Host-session`, httpOnly, secure, samesite=lax. csrf token in a `__Host-csrf` cookie + hidden form field, compared constant time. login rate limited per ip and per account, lockout with backoff. totp for owner/manager. password reset by emailed single-use token (hash stored). every session is listable and revocable by the user and by the owner.

## 7. deploy

- railway service from this repo, branch `rewrite/go-htmx`, dockerfile build.
- environments: `staging` and `production`. staging has its own database. production only gets merged, reviewed code.
- migrations run at boot with a postgres advisory lock; a failed migration aborts the boot (no half-migrated app).
- `/healthz` checks db ping and returns version + commit. railway healthcheck points at it.
- config strictly from env; the app refuses to boot without `DATABASE_URL`, `SESSION_SECRET`, `GOTENBERG_URL`, `TENANT_ROOT_DOMAIN`.

## 8. backups and recovery

- railway scheduled postgres volume backups on.
- nightly logical dump to r2 (`pg_dump -Fc`), 35 day retention, monthly restore drill into a scratch database. a backup that has never been restored is not a backup.
- pdfs live in r2 with versioning on.

## 9. observability

- `slog` json to stdout, one line per request (method, path, status, duration, tenant, user, request id).
- errors: log with stack, return a plain error page with the request id. no stack traces to users.
- `/healthz` for railway, `/metrics` kept private (railway private network only) if we add prometheus later.
- audit log is the business observability layer.

## 10. security baseline

- security headers: hsts, `default-src 'self'`, no inline js (htmx is vendored, not inline), frame-ancestors none.
- all forms csrf protected; all state changes post/put/delete.
- no secrets in repo, `.env` gitignored, railway variables per environment.
- dependency policy: stdlib + a short allowlist (pgx, sqlc, goose, templ, x/crypto, uuid). every addition needs a reason in the pr.
- uploads (letterhead, logos) restricted to png/jpeg, size capped, re-encoded server side, stored in r2.
