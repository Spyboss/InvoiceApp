# decisions

append-only decision ledger for the v2 rewrite.

rules:
- never edit or delete an existing entry. to change a decision, add a new entry and mark the old one `superseded by Dnn`.
- every entry states the decision, why, and what it costs us.
- if code and this file disagree, this file wins until a new entry says otherwise.

---

## D01 — `main` stays the production streamlit app

**date** 2026-10-30 · **status** accepted

**decision** `main` is frozen as the running product: `web_app.py` on streamlit cloud, plus `invoice_app.py` and the fastapi/vercel experiment. no rewrite code lands on `main`. the rewrite lives on `rewrite/go-htmx`.

**why** the business bills on it daily. a half-finished rewrite merged into `main` would break revenue.

**cost** two codebases until cutover. we accept that and keep the old one untouched, not "improved".

## D02 — `feat/erp-ts` is archived, not merged

**date** 2026-10-30 · **status** accepted

**decision** the bun/hono/drizzle rewrite stays on its branch as reference only. we port knowledge (schema shape, transaction guards, proforma geometry, money-as-string idea) but no code.

**why** the target architecture changed (single binary go, multi-tenant, editable config, pos-grade controls). keeping two live rewrites guarantees neither ships.

## D03 — stack: go + templ + htmx + pico css, no node toolchain

**date** 2026-10-30 · **status** accepted (owner delegated the call)

**decision** go (latest stable), `net/http` with go 1.22+ method patterns, `templ` for html, htmx 2 vendored, pico css v2 + one hand-written `app.css`. no npm, no tailwind, no bundler. templates and assets embedded with `embed.FS`, so the build is one static binary.

**why** the build pipeline is the thing that rots first in solo projects. removing node removes whole classes of breakage. pico is mobile-first and semantic, which is exactly the ui we need for a shop floor phone.

**cost** less design freedom than tailwind. if we outgrow pico we swap the css layer later; templates and handlers do not change.

## D04 — hosting: railway for compute + postgres, cloudflare only as edge

**date** 2026-10-30 · **status** accepted · postgres part superseded by D20 (2026-10-30) · verified against vendor docs 2026-10-30

**decision** go app, postgres and gotenberg all run on railway. cloudflare provides dns, waf/rate limiting, turnstile and r2 backups. no application code runs on cloudflare.

**why**
- cloudflare workers cannot run normal go: only a tinygo/wasm bridge, no goroutines, limited stdlib, explicitly experimental. wrong tool for sessions + sql.
- cloudflare containers exist but are built around durable-object sandboxes and per-instance lifecycle (rolling updates, sigterm then 15 min kill, cold-ish starts). wrong shape for a steady erp serving 3-8 users.
- railway runs a normal dockerfile, gives private networking between app/postgres/gotenberg, scheduled postgres volume backups, and one-click domains. cloudflare proxying in front of railway is documented and supported (ssl mode must be **full**, not full-strict).

**cost** ~$5 hobby base + usage. expected $20-30/month for app + postgres + gotenberg at this scale (rates: ~$20/vcpu/mo, ~$10/gb ram/mo, $0.15/gb volume, $0.05/gb egress).

## D05 — multi-tenant from day one

**date** 2026-10-30 · **status** accepted

**decision** one database, shared schema, `tenant_id uuid not null` on every business table. `tenants` table with subdomain, branding, settings, plan. postgres row level security enabled on every tenant table with `current_setting('app.tenant_id')`; application queries also always scope by tenant (belt and braces). tenant is resolved from the host header and cached.

**why** the product will be sold to other dealerships. retrofitting tenancy is roughly two weeks of surgery on every query, index and unique constraint; doing it now is roughly two days.

**cost** every query, index and unique constraint carries a tenant column. every test must prove isolation. platform admin needs a deliberate, audited path across tenants.

## D06 — roles: fixed set, permissions as data

**date** 2026-10-30 · **status** accepted

**decision** tenant roles `OWNER`, `MANAGER`, `SALES`, `ACCOUNTS`, `SERVICE`, plus a platform-level `PLATFORM_ADMIN` that exists outside any tenant. permissions are `resource.action` rows (`invoice.create`, `invoice.void`, `stock.adjust`, `payment.record`, `settings.write`, `report.view`, `audit.view`, ...) granted to roles, with per-user overrides. handlers check permissions; templates hide what the user cannot do.

**why** "sales guy bills and adds stock, admin sees everything" is the real requirement, and it will drift as staff change. hardcoded `if role == "admin"` checks rot instantly.

**cost** a permission table and a middleware. worth it.

## D07 — money, ids, deletes

**date** 2026-10-30 · **status** accepted

**decision** money is `bigint` cents everywhere (db, json, go). no floats, no decimal strings. ids are uuidv7 generated by postgres. all business tables have `created_at`, `created_by`, `updated_at`, `deleted_at`. nothing is hard-deleted; issued documents cannot be edited at all.

**why** float money bugs are how invoicing apps lose trust. uuidv7 keeps b-tree inserts sequential.

**cost** every boundary converts (form string → cents), so one shared parser and tests.

## D08 — anti-cheat is a product feature, not a policy

**date** 2026-10-30 · **status** accepted

**decision** the system is designed so a salesperson cannot quietly under-bill, hide a sale, or move stock without a trace. controls C1..C14 are specified in `BUSINESS-RULES.md` and are part of v1, not "later hardening".

**why** the owner's words: everything tracked, cannot be cheated. that is the actual buying reason for replacing paper.

**cost** friction: voids need a reason, discounts over a floor need approval, day-end needs counting cash. that friction is the feature.

## D09 — document set and numbering

**date** 2026-10-30 · **status** accepted

**decision** v1 documents: quotation, sales invoice (cash), sales invoice (leasing), proforma, advance payment receipt, payment receipt, day-end (z) report. numbering is per tenant, per document type, per year, zero-padded 4 digits, allocated inside the same transaction as the document from a locked counter row. numbers are never reused and never editable.

**why** gaps and duplicates are the fraud signal. atomic allocation is the only way to guarantee both.

**cost** a counter row per tenant/type/year and a lock per issue. negligible at this volume.

## D10 — pdf: gotenberg, redesigned sales documents, byte-faithful proforma

**date** 2026-10-30 · **status** accepted

**decision** all pdfs are html rendered by a gotenberg container (chromium), called over railway private networking. sales invoice, quotation, receipts and reports are redesigned to look clean and modern (a4). the **proforma must match `BIBILE 01.docx` exactly**: letter page 612x792pt, geometry derived from the docx twips, singer wordmark and letterhead as supplied, calibri metrics via carlito (bundled in the gotenberg image). every generated pdf is stored with a sha256 and can be reproduced from the database.

**why** the proforma goes to the leasing company and must look like the document they expect. the rest is our own paper and currently looks dated.

**cost** a second railway service; font substitution has to be verified once with a pixel diff.

## D11 — scope split

**date** 2026-10-30 · **status** accepted

**v1 (replaces streamlit, cannot cheat)** tenancy, users/roles/permissions, audit, customers, vehicle stock + ledger, quotations, invoices (cash/leasing), proforma, advance receipt, payments/receipts, day-end close, reports (sales/stock/receivables/day-end), settings, csv import of history, mobile ui.

**v1.5** notifications (email + telegram for admin alerts), dashboard trends, approval workflow ui, supplier/purchase basics, customer statements.

**v2** parts inventory, service jobs, multi-branch, subscription billing for other dealers, passkeys, public api/webhooks.

**never** double-entry accounting, payroll, offline write sync, user-editable html templates.

**why** "full erp" scoped honestly is a year. v1 is the part that replaces the current app and closes the fraud holes.

## D12 — local dev needs no docker

**date** 2026-10-30 · **status** accepted (owner delegated the call)

**in place** go 1.27.0, postgresql 18.6 (service `postgresql-x64-18`), templ 0.3.1100. local role `invoice_app`, databases `invoiceapp_dev` and `invoiceapp_test`. `psql` and `~/go/bin` are on the user PATH.

**decision** local dev = native postgres on windows + `go run ./cmd/server`, pointing at the railway gotenberg service for pdfs. docker is only needed if we later want fully offline pdf rendering.

**why** docker desktop on windows 10 is a heavy dependency for one developer. native postgres is one installer.

**cost** pdf rendering in dev depends on the network. acceptable.

## D13 — engineering standards (anti-slop)

**date** 2026-10-30 · **status** accepted

**decision** no code without a caller. no `utils` package. no interface with a single implementation. no TODO/fixme. no dead flags. handler → service → sqlc query, one direction. errors wrapped with context. structured `slog` logs. migrations append-only. every step ends green on `go vet ./...`, `go test ./...`, `templ generate -fail` and a manual click-through.

**why** the previous two attempts died of scope and half-finished layers, not of missing features.

## D14 — notifications

**date** 2026-10-30 · **status** accepted (v1.5)

**decision** in-app + email (resend) for account events, and telegram bot messages to the owner for security/business alerts (void, discount override, day-end variance, missing number, after-hours activity). no sms/whatsapp api in v1.

**why** email for users, telegram for the owner is the cheapest reliable channel in sri lanka and needs no per-message billing.

## D15 — analytics

**date** 2026-10-30 · **status** accepted

**decision** a dashboard with six numbers (today sales, month sales, cash vs leasing, gross margin, stock value, receivables) and two trends (daily sales 30d, monthly sales 12m), plus a per-user sales table. no bi tool, no warehouse.

**why** the owner needs steering signals, not dashboards nobody reads.

## D16 — explicit non-goals

**date** 2026-10-30 · **status** accepted

**decision** no offline write mode, no custom template editor, no double-entry ledger, no native mobile app, no microservices. if a future requirement needs one of these, it gets a new decision entry first.

## D17 — no sqlc, no goose: hand written pgx + a small migration runner

**date** 2026-10-30 · **status** accepted (supersedes the sqlc and goose lines in D13 and `ARCHITECTURE.md`)

**context** `go install sqlc` spent more than ten minutes compiling `pg_query_go` on the dev machine and never finished. goose would be a second binary and a second config for a job that is, here, about 90 lines: forward only, one database, one writer.

**decision** queries are hand written pgx v5 in `internal/store`, one file per aggregate. migrations are embedded `.sql` files in `internal/db/migrations`, applied at boot by `internal/db/migrate.go` under `pg_advisory_lock`, recorded in `schema_migrations`. forward only. an applied migration is never edited; a fix is a new file.

**why** one toolchain (`go` + `templ`), no codegen step in the edit loop or in ci, no generated code to review, no second binary. the sql sits next to the method that runs it.

**cost** no compile time column checking. paid down by integration tests against real postgres for money, numbering, permissions and tenant isolation, which the rules require anyway. if the schema passes ~40 tables and drift becomes real, sqlc can come back through a new entry.

## D18 — first run setup closes after the first tenant

**date** 2026-10-30 · **status** accepted

**decision** `/setup` can create a tenant and its owner only while the installation has zero tenants. after that, a hostname with no tenant row is a 404 and `/setup` refuses. adding a dealership later is an operator action: insert the tenant, then that host's `/setup` creates its owner.

**why** an open signup screen on a public hostname is free resource use and a support burden. selling to other dealers (v2) needs billing and an admin console, which is a different feature with its own step.

**cost** onboarding a second dealer today needs one database insert. acceptable until v2.

## D19 - the stock ledger, and who may set vehicle money

**date** 2026-10-30 · **status** accepted

**context** step 3 introduces vehicles and stock. the live app keeps stock in one editable sheet, so a unit can be sold without anything recording that it left the yard, and a sold unit can be typed back to available.

**decision**

- `stock_movements` is append only: `RECEIVE (+1)` on intake, `SELL (-1)` when an invoice takes the unit, `RETURN (+1)` when a sold unit comes back. `app_rw` may insert and select only; update and delete are revoked, the same as the audit log.
- a `before insert` trigger takes a per vehicle advisory lock and refuses any row that would take that vehicle's running total outside {0,1}. the invariant belongs to the database, not to one go query, so no future code path can sell one unit twice or receive it twice.
- reserve and release are vehicle status changes, not ledger rows: the unit never left the yard. both are audited (`stock.reserve`, `stock.release`) with the reason typed by the clerk.
- on hand is `AVAILABLE + RESERVED + RETURNED`, because a returned unit is physically here waiting for inspection. the stock screen compares that with the ledger total and warns when the two disagree.
- intake and edit carry cost and price, so they require `vehicle.write`, held by OWNER and MANAGER only. SALES holds `customer.write`, `vehicle.read`, `stock.read` and `stock.write`: a salesperson can add a customer and reserve or release a unit, but cannot see or set what the dealership paid.

**why** stock is the second place a dealership is cheated: a unit leaves the yard and the books never say so. a database invariant survives refactors. hiding cost from sales is the smallest permission split that keeps the margin private while the counter keeps working.

**cost** a damaged or written off unit has no movement kind yet; it needs its own entry when the business asks for it. until then the only way a unit leaves the yard is a sale.

## D20 - the online database is neon, not railway postgres

**date** 2026-10-30 · **status** accepted (supersedes the postgres part of D04 and section 6a of `ARCHITECTURE.md`)

**context** railway postgres works, but its free volume has no branching and no point-in-time restore, and a restore drill means copying a volume. neon gives a copy-on-write branch in seconds, instant restore, and a free tier that fits staging and one dealership. the rewrite is still early enough that no production data exists.

**decision**

- the app and gotenberg stay on railway, singapore. the database is neon project `invoiceapp` (`blue-hat-64568041`, `aws-ap-southeast-1`, postgres 18, database `invoiceapp`).
- the app connects to the direct (non pooled) endpoint with `sslmode=require`. migrations and `pg_advisory_lock` need a session, and tenant scoping is already transaction local, so the pgbouncer pooler adds a failure mode without a benefit.
- `app_rw` is still created by migration 0001, exactly as on any other postgres. neon's owner role has `CREATEROLE` (checked before the cutover) and becomes the role's admin, so runtime role separation, forced RLS and the stock trigger are unchanged.
- the full test suite is run against a neon branch before any cutover. a branch is thrown away afterwards.
- staging data was not migrated. it held one tenant and one owner created an hour earlier, and reaching it would have needed a temporary public TCP proxy on the database. the staging host runs `/setup` once instead. production data at cutover (step 8) is a separate, reviewed import.
- the railway postgres service is removed only after the neon setup is verified from a signed in session.

**why** a branch means a preview environment, a restore drill or a risky migration is a copy-on-write operation measured in seconds, not a volume copy. instant restore covers the "someone deleted a day" case that a volume backup only partly covers. at this scale both are free.

**cost** the free tier scales compute to zero, so the first request after idle pays a cold start (measured 0.4 to 1.1 s during the cutover). if it is felt at the counter, the paid tier keeps a warm floor. also one more vendor to hold credentials for; the api key and database password live in railway variables and the local neon profile only.
