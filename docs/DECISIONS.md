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

## D21 — numbering, documents, and the archived pdf

**date** 2026-10-30 · **status** accepted (supersedes the r2 storage line in section 5 of `ARCHITECTURE.md`, and refines D06–D10)

**context** step 4 turns a quotation into paper and a sale into stock leaving the yard. the live app numbers invoices from one csv with no lock, lets any total be typed, and regenerates every print from whatever the data says today. each of those is a way the books stop matching the paper.

**decision**

- numbering: `document_series (tenant_id, doc_type, year, last_value)`, bumped by `insert … on conflict do update … returning` inside the transaction that writes the document. postgres takes a row lock, so concurrent clerks get consecutive numbers; a rollback returns the number, so there are no gaps. the number is stored as a string of at least 4 digits. prefixes and per type formats come with settings in step 6; issued numbers never change.
- one `documents` table for QUOTATION, SALES_CASH, SALES_LEASING, PROFORMA and ADVANCE. one customer and one vehicle per document. customer and vehicle details are copied into the row at issue, so editing a customer or unit later cannot change what was printed. no line items: a dealership document is one unit.
- statuses: a quotation is OPEN until it becomes an invoice (CONVERTED); everything else is ISSUED. VOID arrives with approvals in step 6. the `documents_immutable` trigger (SQLSTATE DOC01) refuses any update to a printed field; only status and updated_at may move.
- money: price, down and balance are cents, with `balance = price - down` enforced by the database. a cash invoice is paid in full by definition; a leasing sale or proforma must leave a balance; an advance must be a real deposit.
- stock: creating a cash or leasing invoice locks the unit, sets it SOLD and writes the `SELL` movement in the same transaction. an advance reserves an available unit; a quotation or proforma does not move stock. only an AVAILABLE or RESERVED unit can carry a document; a RETURNED unit needs an inspection step before it can be sold again, which is not built yet.
- a quotation converts at most once: the quote row is locked and set CONVERTED, the invoice keeps `quote_id`, and the customer and unit must match the quote.
- permissions: `document.read` for every role, `document.read.all` for OWNER, MANAGER and ACCOUNTS, `document.create` for OWNER, MANAGER and SALES. a sales user sees and prints only documents they created; someone else's document is a 404, not a 403.
- printing: html rendered by gotenberg v8 (`/forms/chromium/convert/html`) on railway's private network, with basic auth. every font and image is embedded as a data url; nothing is fetched while rendering.
- storage: the first successful print is archived in `document_pdfs` (bytea plus sha256), append only. later downloads serve those exact bytes, so a sale never depends on gotenberg being up and a template fix cannot rewrite history. r2 is deferred until size or retention demands it; the table is the seam.
- proforma: fixed 612x792pt letter, geometry derived from `BIBILE 01.docx` (566.5pt grid at -45.25pt, rows and borders from twips), singer wordmark svg and letterhead jpeg extracted from the docx, carlito embedded for calibri metrics. verified by rendering the docx through gotenberg's libreoffice route and comparing text blocks: the table and body sit within about 1pt; the terms wrap and the signature block differ where libreoffice and chromium break a line differently.
- the proforma is singer's paper, so proforma creation is refused for any other finance company until settings can supply another letterhead. every other document uses the dealership's own a4 template.
- `vehicles.vehicle_type` (petrol, electric, …) was added because the proforma prints TYPE. it is captured at intake.

**why** a number is the identity of a sale; a duplicate or a gap is a fraud signal before it is a bug. snapshots and the update trigger mean the paper cannot be rewritten after the customer leaves. storing the first print is cheaper than proving a regenerated one is byte identical.

**cost** pdf bytes live in postgres until r2; around 60kb per invoice is fine at this volume, and moving later is a copy plus a reader change. one more railway service to keep alive, and a network dependency at the first print; afterwards the archive serves. the proforma is tied to singer paper until another letterhead is supplied. the sample `BIBILE 01.docx` stays out of the public repo because it carries a customer's nic and phone; its geometry and extracted assets live in `internal/pdf`.

## D22 — payments, the closed day, and the z

**date** 2026-10-30 · **status** accepted

**context** step 5 puts money against paper. the live app records a payment as a row in a sheet with no link to the invoice, so the same receipt can pay two invoices and nothing says which one it settled; the day end is a typed total with no expectation behind it, and last month's z can be retyped today.

**decision**

- one receipt pays one invoice: `payments.document_id` is not null and there is no unallocated credit. a customer deposit is still an `ADVANCE` document, not a payment.
- the outstanding balance is derived: `documents.balance_cents − Σ posted payments`. the document row stays frozen, and the invoice screen shows "paid since issue" and "outstanding" beside the printed "balance at issue", so the paper and the money can never be made to agree by editing either one.
- an overpayment is refused twice over: `CreatePayment` checks the balance under a lock so the clerk gets a sentence, and the `payments_within_balance` trigger (SQLSTATE `PAY02`) refuses the row whatever wrote it. a refused receipt burns no number, because the series is bumped inside the same transaction, after the check.
- a receipt is inserted once; the only update it accepts is a reversal (`payments_reverse_only`, `PAY03`). the reversal is dated `reversed_on`, the business date the money was handed back, never the day it was taken. the receipt, its reason, its actor and its timestamp stay in the trail.
- money is dated with the server's business date in Colombo, never with a date from the browser. backdating, and with it the approval pin, arrives with step 6.
- a money report reads as at a business date, not as of now: receivables count a receipt from the day it was taken and stop counting it only on the day the money went back, and an invoice dated after the report date is not in it. last month's receivables therefore read as they read last month, and a reversal taken today cannot rewrite a month that was already closed.
- money cannot be dated on a day that is already closed, neither a receipt nor a reversal: `payments_on_open_day` raises `DAY01`. without it, cash taken after the z would sit on a date whose totals are frozen and would appear on no z at all, which is the one way money can leave the drawer without leaving a trace. the receipt screen replaces the form with the reason rather than letting the clerk type an amount that will be refused.
- payment methods are fixed in code and in the database: `CASH`, `CARD`, `BANK`, `CHEQUE`. only cash counts towards the drawer. tenant-editable methods come with settings in step 6.
- day end: one close per tenant per business date (`unique (tenant_id, business_date)`). the server computes `expected = opening float + cash in − cash handed back`, the cashier types only the float and the counted cash, and `variance = counted − expected` is enforced by a check constraint. the float defaults to what the last close counted, and stays editable.
- the z lines are frozen into `day_end_lines` at close time: payments by method, money handed back, documents by type, stock movements. the labels store codes, and the screen supplies the words, so renaming a document type tomorrow does not rewrite what a z said last month.
- a close is insert only. `app_rw` has no update or delete on `day_ends` or `day_end_lines`, and `day_ends_closed` (`DAY01`) refuses even the owner role.
- permissions: `payment.read`, `payment.record`, `payment.reverse`, `dayend.read`, `dayend.close`. OWNER, MANAGER and ACCOUNTS hold the money permissions; SALES holds none of them. reports reuse `report.view`.
- a money report is scoped the way documents are: without `document.read.all` the store is handed the caller's own id, so a salesperson's report counts their invoices and not a colleague's. the page says so. stock cost follows `vehicle.write`, because SALES must not see margin.
- csv export is utf-8 with a BOM so excel reads it, quoted per RFC 4180 through `encoding/csv`, and a cell a spreadsheet would run as a formula is prefixed with an apostrophe unless it is a plain number. money cells are written by `money.Plain`, without grouping, so the spreadsheet reads a number and go never does float arithmetic on money.
- deferred deliberately: the receipt pdf, the z pdf and the report pdfs, the reconciliation view (C11, per day and user), and the variance alert threshold. the z is readable on screen and exportable as csv today; the alert threshold belongs to tenant settings in step 6, and the paper belongs with the letterhead work.

**why** the two rules that keep a dealership honest about money are "a receipt never exceeds the balance" and "a counted day never changes". both are in the database, not in one handler, so no later import script or screen can step around them. dating a correction on the day it happens is what lets the z stay immutable without hiding the correction.

**cost** a wrong receipt cannot be edited; it is reversed and re-taken, which is two rows of history instead of one tidy fix. a cashier who closes the day early cannot take more cash until the date turns over, so the day end has to be the last action of the day; reopening a closed day with approval is step 6. and there is still no customer credit balance, because a payment is always attached to an invoice.

## D23 — the import, and the parallel month

**date** 2026-11-02 · **status** accepted

**context** step 7 has to carry five years of paper out of the streamlit app and into this one, and then prove for a month that the two agree. the old app exports `invoices.csv` from a csv file it treats as the database: one row per paper, numbers from one `SALES` series shared by cash and leasing, money written as a spreadsheet writes it (`1250000.0`), and a hand-kept stock sheet beside it. nothing in the old export can be asked for an explanation, so every rule about what an imported row means has to live here, once, and be refused loudly when the file breaks it.

**decision**

- import is one transaction, all or nothing. a file with one bad row is refused whole, with every problem named on its line, and nothing is written. a silently dropped row is a sale that never reaches the book, which is worse than a file that will not load.
- a run is idempotent. the number the old app printed is the identity of the paper: `InsertImportedDocument` writes it as-is under `documents.imported_at` and `documents.import_run_id`, and a second upload of the same file finds every number already held and writes nothing. that is what makes the same file safe to send every day of the parallel month.
- an imported number is never allocated here. `document_series` is lifted afterwards, above every imported number for the year, so the next paper this app issues continues from the paper book instead of colliding with it. the two sales series (`SALES_CASH`, `SALES_LEASING`) split one legacy series, so both are lifted together to the higher of the pair; otherwise cash would start again at 1 beside imported leasing numbers.
- two passes. the first pass only reads: every number the app already holds is compared with the row that claims it, and a disagreement refuses the file without writing anything. the number is the identity of the paper whatever date it carries and whatever kind it was filed under, so a row whose date moved to another year, or a sale that owed money and now owes none, is the same paper disagreeing by name rather than a second paper printing a number the first one already carries. only then does the second pass create customers, units and papers.
- what an old row means: `SALES` with zero balance is a cash invoice, `SALES` with a balance is a leasing one, `PROFORMA` and `ADVANCE` map straight across. stock effects follow the same reading: a sale writes `RECEIVE` then `SELL`, an advance writes `RECEIVE` then `RESERVED`, a proforma writes `RECEIVE` only. a unit that has already left the yard cannot leave twice, so re-importing a sold unit fails rather than moving stock twice.
- customers are matched by nic, then by name when the file carries no nic; units by chassis. five years of paper must not become five hundred copies of the same person.
- the invoices file carries a model and never a make, and a vehicle row needs a make. the person importing names the default make; a row that names its own make wins. the stock sheet may name a make on every row, in which case the default is not needed.
- acceptance is enforced by the database, not by the screen: `import_runs` stores `source_totals` (what the file claimed) and `stored_totals` (what the database held afterwards, read back in the same transaction), with `check (source_totals = stored_totals)`. a run that does not balance cannot be written down at all, so no page can report an import that did not happen. `import_run_id` is a deferred foreign key because the papers are inserted before the run row they point at, inside the one transaction.
- reconciliation is a second, separate act: upload the same file for a range of days and the store compares it with the papers that came in from a file (`documents.imported_at is not null`) for those days, per document type, keeping papers, gross, down and balance apart because a sale whose price agrees but whose down moved is still a difference. papers this app issued itself are not in the file and are not a difference.
- `reconciliation_lines` stores source, stored and difference, with the difference and `matched` computed by the database, so a line cannot claim a match. a run that finds a difference writes a `RECONCILIATION` alert in the same transaction; a run that agrees writes none. one sentence names the difference, produced by `ReconcileSide.Description()` and used by both the alert and the page, so the two can never word it differently.
- both screens use post/redirect/get: a good run redirects to `/import?run=<id>` or `/reconciliation?run=<id>`, and the page reads the run back from the database. a refusal stays on the form with the typed values kept, because that is the file the person is about to fix.
- permissions: `import.run` is OWNER only. `reconcile.run` is OWNER, MANAGER and ACCOUNTS. the csv holds the whole book, so a salesperson cannot see either page.
- uploaded bytes are read through `http.MaxBytesReader` (10 MB) and hashed with sha256 before parsing; the hash is stored with the run. csrf middleware now parses multipart bodies, because until it did, no form in this app could carry a file.
- the stock template is the header only, no example row: an example row is an invitation to import it.

**why** the old app's numbers are the only link between paper on a shelf and a row in a database, so the import must not invent one, must not renumber one, and must be safe to repeat. putting the acceptance test in a check constraint means it holds for a script, a retry or a future screen, not just for today's handler. comparing the file with only the imported papers is what makes the check meaningful: it asks "is the book we brought in still the book the old app has", not "does the whole dealership agree with one csv".

**cost** a single bad row stops the whole file, so the owner corrects the export and uploads again; that is deliberate, and the page names every line at once so it takes one pass. reconciliation only covers imported papers, so a paper typed into this app during the parallel month is invisible to it and shows up as neither a match nor a difference; the parallel run therefore still needs a human eye on `/documents` for papers this app issued. and the shared legacy sales series means two papers in one year can print the same number until a printed series prefix or one shared sales series is decided (see the open question on D09/D21).

## D24 — the interface: one frame, facts instead of prose

**date** 2026-11-03 · **status** accepted (heading rule refined by D31)

**context** steps 2 to 7 grew fifteen screens one at a time, each written by whoever had the rule in their head that day. the result worked but read like documentation: a tagline under every heading explaining the screen to itself, a `<small>` under every input restating the constraint the server already enforces, empty states that advised instead of informing, and a footer on one page carrying a sign-out link the other fourteen did not. a clerk does not read a dealership's software; they look for the figure they came for. every sentence that is not a figure or a label is one more thing between them and it.

**decision**

- one frame for every screen: `Layout(title, tenant, user, csrf, nav, body)`. the dealership, the destinations this person is allowed to reach, who they are signed in as and the way out live in the header. no page repeats a sign-out form, a "back" link or its own navigation.
- a heading carries no tagline. `h1` names the screen and stops. a page whose purpose is unclear gets its layout fixed, not a sentence added.
- an input carries no helper `<small>`. a constraint belongs in the label, in the abbreviation everyone already uses: "Password (12 characters or more)", not a label plus a paragraph.
- empty states are one line and give no advice: "No receipts yet." A filter that found nothing says so in different words from a book that is empty, and the page knows which it is because it holds the filter values. "No receipts yet." / "No receipt matches that filter."
- rules are not explained in advance. a refusal that already exists server-side says what happened on the attempt; a paragraph above the form saying it will happen is noise that goes stale the moment a limit changes.
- facts are a `<dl class="facts">`, or a table with column headings. if a value cannot be a term and a definition, it does not belong in a facts block; if a set of values cannot be a row, it belongs on another screen.
- search fields say what they search, not what they match against: `placeholder="Search customers"` plus an `aria-label`, never "Name, phone or NIC".
- an alert channel that is not configured reads as a fact, not a warning: `Delivery — this page only`.
- two forms side by side on a wide screen and stacked on a narrow one: `.split`.
- a table wider than the screen scrolls inside its own box with a thin scrollbar, and the page itself never scrolls sideways. money is never wrapped or abbreviated to make it fit; the cell gives up padding first, at `max-width: 45rem`.
- the interface has no dead branches: a view field no template uses is deleted, not kept for a screen that may come. three went with this pass (`HomeData.CanSeeAudit`, `AlertRow.Kind`, the import page's `Attempted`/`Notice`), replaced by flags and rows that are actually drawn.

**why** the screens are read at a counter, in a hurry, by someone whose job is not this software. simplicity here is not a style: a page that states one thing in one place cannot disagree with itself, cannot go stale, and cannot be skimmed wrong. the mixed version did all three: the payments page explained a reversal above the list, the receipt page explained it again, and the day-end page explained expected cash a third time, in three slightly different words.

**cost** the rationale that used to sit on screen now lives in this file, in `BUSINESS-RULES.md` and in doc comments, so someone reading the code has to go one step further to learn why a rule exists. the narrow-screen table rule is a compromise: six columns do not fit a phone, so those screens scroll. and the interface is plainer than a marketing page would be, which is the point, but it does mean a new screen inherits an unwritten obligation to be checked against the rules above rather than a template that enforces them.

## D25 — the stylesheet is revalidated, not cached blind

**date** 2026-11-03 · **status** accepted

**context** the css is embedded in the binary and served from it. a file in an embedded filesystem carries no modification time, so `http.FileServerFS` sends no `Last-Modified` and no `ETag`, and the handler's `Cache-Control: public, max-age=300` left a browser free to paint last week's stylesheet over this week's markup for five minutes with no way to ask whether anything changed. this was found the honest way: a rule was added, measured as having no effect, and the browser turned out to be holding a copy from before the restart.

**decision** `staticHandler` serves with `Cache-Control: no-cache` and an `ETag` built from the sha256 of the file's own bytes, answering `If-None-Match` with `304`. a request costs a header exchange, never a stale rule. the tag is derived from content, so it changes exactly when the file does, which also means it survives a rebuild that changes nothing.

**why** the one thing worse than an uncached stylesheet is a cached one that disagrees with the html it is painting. revalidation is cheap, and it makes "restart the server, reload the page" true again instead of "restart the server, reload, and hope".

**cost** one conditional request per page load instead of zero for five minutes. at this traffic that is nothing, and a 304 carries no body.

## D26 - one sales series, and the IRD serial

**date** 2026-11-13 – **status** accepted (supersedes the numbering format and the per-type series in D09 and D21)

**context** two things were wrong with the numbers. first, cash and leasing were numbered from two counters while the customer reads "SALES INVOICE" on both, and the app this one replaces spent a single SALES number space for both. every read of the counters already had to pretend the pair was one series, and D23 recorded the cost of that pretence: a year of imported paper could hand out two papers in one year carrying the same number. second, the format was four digits with no year or month in it. Sri Lanka's Inland Revenue Department prescribes the serial a tax invoice carries: `YYMMM_QQQQ_XXXXX`, at most 40 characters, no spaces, continuous. changing that later means renumbering paper that is already on a customer's desk, so it is cheaper to print it from the first invoice.

**decision**

- one sales series. `SALES_CASH` and `SALES_LEASING` stay two document types, because a cash sale is paid in full and a leasing sale leaves a balance owed by a finance company, and the two read differently on the screen. they are numbered from one counter, `SALES`, through `SeriesFor`. `ValidSeries` refuses the two raw document types, so a caller that forgets `SeriesFor` is failed instead of quietly opening a second sales counter beside the real one.
- the serial is `YYMMM_QQQQ_XXXXX`, built by `FormatSerial`: year and month of the paper itself, a four character branch segment, then a counter padded to five digits. the counter still runs per tenant, per series, per year, so the digits only go up; a backdated paper carries an earlier month than the one before it and is still unique.
- the branch segment is the tenant's slug reduced to its first four alphanumerics, upper cased and zero padded. it is not a settings field: a dealership runs one counter, and the slug is fixed when the tenant is created (D18), so the number cannot disagree with the tenant it came from.
- migration `0008` merges the two old sales counters into `SALES` at the higher of the pair for each year, then deletes the old rows. it loops tenants with `app.tenant_id` set, because `document_series` is force row level security and a bare delete from a migration would match nothing and report success.
- a number already printed is never rewritten. papers issued before this entry keep the number the old app or the old format gave them; the serial applies from here.
- `serialTail` is the single place that reads a counter out of a stored number, so both the gap scan and the import's lift work on `0041` and on `26NOV_TANG_00041`. a number that does not end in digits is skipped, not guessed at.
- `numbering_gap.go` reads the sales series from the two document types it still covers, because `documents.doc_type` did not change.

**why** the number is the identity of a sale, and it has to agree with the date printed beside it. one series makes the counter match what the customer sees and removes the merge that every read of the counters used to carry. the IRD shape costs nothing today and is expensive to adopt after the first paper goes out.

**cost** the `doc_no` column carries two shapes for a while, the old short form on pre-existing papers and the serial on new ones, until the old ones stop mattering. the branch segment is derived rather than chosen, so a dealer who wants different four characters needs a settings field that does not exist yet. and the serial is necessary but not sufficient for a compliant tax invoice: the TIN of both parties, the place of supply, the mode of payment and the amount in words are separate work on the pdf templates, still not built, and still waiting on the accountant's answer about whether this business is over the VAT threshold at all.

## D27 - the invoice prefix is the owner's to choose

**date** 2026-11-13 – **status** accepted (refines D26)

**context** D26 took the four character branch segment of a serial from the tenant's slug, so a number could never disagree with the tenant it came from and a new tenant printed something sensible before anyone opened a screen. but a slug is a hostname: it was chosen to be unique and short, not to read well on paper, and a dealership that wants `TM01` on its invoices cannot have it. the owner asked for it to be configurable.

**decision** `tenant_settings.series_prefix`, edited on `/settings` beside the company details, validated as blank or exactly four upper case letters or digits. blank is not "keep the old one": it means "work it out from the slug again", which is what a tenant that never set it does, so the column needs no backfill and a mistake is undone by clearing the box. the label of the input prints what a blank box would produce, so the owner never has to read a printed serial to find out what is being printed.

**why** the four characters appear on every paper the business issues for as long as that serial is in use, so they belong with the letterhead and not with the hostname. blank-means-derive keeps two things worth having: a fresh tenant is never broken, and there is always a way back.

**cost** the prefix can now disagree with the tenant, and changing it part way through a year splits that year's serials across two prefixes. the counter is untouched, so no number repeats and none is skipped; what changes is the printed series, which an auditor will notice and a person will have to explain. paper already issued keeps the prefix it was printed with, so the record stays honest even though the series changes shape.

**also recorded** the owner has confirmed the business is neither VAT registered nor advised by an accountant. the rest of the tax invoice field set (both parties' TIN, place of supply, mode of payment, amount in words) is therefore deliberately not built: printing tax fields the business has no registration to support would misrepresent the paper. the serial stays, because it costs nothing and registration is a turnover test he could cross later without warning, at which point the number is already in the shape the department asks for.

## D28 - no parallel month; step 7 closes on the software

**date** 2026-11-13 – **status** accepted (supersedes the acceptance half of D23)

**context** D23 made a month of parallel running the acceptance for the import: the old streamlit app stays the system of record while this one carries the same paper beside it, reconciled daily, so a difference surfaces in a diff instead of at a counter. the owner has decided not to run it. he wants the system finished, deployed, and then tested and adjusted on the live business.

**decision** the parallel month is dropped. step 7's acceptance is the software half only: an imported file's totals equal the totals the database stored, enforced by `check (source_totals = stored_totals)` rather than by a person remembering to compare; a file that does not add up is refused whole; every path is covered by a test. `/reconciliation` stays and works, as a tool he may use on any day he chooses, but it is not a gate and nothing waits on it.

**why** the parallel month costs a month of entering every paper twice at the counter. that is a real cost paid by a real person, and the owner judges the software that would have been verified by it to be constrained already by the database instead. the decision is his to make; the cost of the month is his to refuse.

**cost** the first month of live operation becomes the test. a difference between the old app's book and this one will surface when a customer or a clerk meets it, not in a daily diff on a quiet morning, and by then the paper is already in somebody's hand. two things blunt it, both already built: the import refuses a file that does not balance, so a bad import cannot silently poison the book, and the restore drill can put the database back to a known state. what is left unproven is narrow but real: whether the *right* file was imported, and whether the old app held anything the export does not carry. a dump of the old app's database taken before the first import and kept is the cheapest insurance against both, and costs one command.

## D29 - the nightly dump, and why the drill runs as the owner

**date** 2026-11-13 – **status** accepted (supersedes the backup half of D04 and the retention assumption in D20)

**context** D20 chose neon partly for instant restore, and D04 planned scheduled volume backups and r2. measured on 2026-11-13, the project reports `history_retention_seconds` 21600 on `free_v3`: six hours of point in time restore. that covers a row deleted by mistake ten minutes ago and nothing found the next morning. no dump job has ever run. separately, the restore drill was rehearsed on 2026-11-03, before every table that holds the book had rls forced, and it can no longer dump them as a locally created owner role.

**decision** one nightly logical dump, written by a railway cron service built from `backup/`, kept 14 days on a railway volume. it runs as the **database owner**, because row level security is forced on eighteen tables and `pg_dump` reads past a policy or not at all. it proves each file with `pg_restore --list` before keeping it, writes under a partial name and renames, and exempts the newest file from pruning. `scripts/backup.sh` refuses an empty book and refuses a connection that cannot bypass rls, both before it dumps. r2 is not used: the dumps are already on a different provider from the database, and a bucket would add a credential and a client to a job that fits in one script.

**why** six hours is not a backup for a book that bills a business; the plan that gives more of them is monthly money and still one provider. a dump on railway plus a database on neon means no single provider failure takes both, and a 14 day window means yesterday is always there. the drill is what turns the dumps into a backup, and it needs the owner credential for the same reason the dump does.

**cost** the dump is only as good as railway's disk, and a mistake older than 14 days has no copy. a cron that silently stops looks exactly like one that works, so the sign-off checklist names a dump to open by hand and the drill's output is kept with it. and the owner connection string is now a backup credential as well as a migration one: it must not leak, and it should not be the string on a developer's laptop.

**found while building it** the dump failed with `query would be affected by row-level security policy for table "alerts"`, which is the guards doing their job. it also means the documented drill had quietly stopped working for anyone running it as a locally created owner, and that no local rehearsal is possible without the `postgres` password, which is not in the repository. both scripts now check the role first and say so in one sentence instead of failing inside `pg_dump`.

## D30 - the letterhead is the settings screen, and the paper invents nothing

**date** 2026-11-13 – **status** accepted

**context** `/settings` collects the company name, address, phone and VAT number, and the dealer pdf ignored all four: it printed the constants in `internal/pdf/dealer.go`, including `VAT Reg. No. 124008026-7000`, which is the number on singer's proforma template. the live streamlit app prints no VAT number at all, and its dealer name reads "Gunawardhana Enterprises, Beliatta Road, Tangalle". the tenant row reads "Gunawardana Enterprises - Tangalle". so the new app would have stamped a registration number on a business that is not registered for VAT (D27), with a spelling of its own name that nobody chose.

**decision** the letterhead is the tenant's settings. the name falls back to the tenant's own name, never to a constant. the address and phones fall back to the details the shop printed before the screen existed, because a blank letterhead is worse than a stale one. the VAT line is printed only when a number is set, so a business that is not registered prints none. the proforma is untouched: it is the leasing company's paper, its template places the number itself, and the geometry is pinned to their file.

**why** a settings screen whose values do not reach the paper is a screen that lies, and the owner finds out at the printer with a customer waiting. the VAT number is the sharp edge: an invoice that claims a registration the business does not hold is a problem with the revenue department, not a cosmetic one.

**cost** until `/settings` is saved, a dealer document prints the old address and phones, so the screen has to be filled before the first real paper and the checklist says so. the spelling of the business name is now his to settle in one box: the live app and the tenant row disagree by one letter, and from here only the settings field decides it. two tests hold the line: the letterhead is what the settings say, and a paper with no VAT number set does not claim one.

## D31 - the frame is a sidebar, and a heading may carry one line that says what the heading cannot

**date** 2026-11-18 · **status** accepted (refines D24; supersedes only its "no tagline" bullet)

**context** the owner looked at the running app and called the interface what it is. the destinations sat as thirteen flat links across one header row, wrapping to two lines on a normal screen, ordered the way the tables were built rather than the way a day is worked: a clerk who opens four of those fourteen times a day scrolls past nine they will never touch on every screen they visit. research on admin layouts puts the usable ceiling for a top row at seven to nine items, and past that the answer is a sidebar, not a tighter row. separately, D24 banned a tagline under every `h1`, and the ban was right about what it caught: the screens had grown a sentence restating their own heading, written by whoever had the rule in their head that day. but the industry examples the owner asked to be measured against carry a line under the title, and some of them are generated filler, so copying them would have imported exactly the slop D24 was written to remove. the two have to be told apart by what the line does, not by whether a line exists.

**decision**

- the destinations move to a left sidebar, grouped by task, three groups: **counter** (Documents, Customers, Vehicles, Stock, Payments, Day end), **oversight** (Reports, Alerts, Reconciliation), **admin** (Import, Audit, Settings, Users). permission still decides which links exist, exactly as now; a group whose links are all filtered out does not render, so a role that holds three destinations sees one group and no empty headings.
- a heading may carry **one line, and only when the line says something the heading cannot**: what the page holds that its name does not say ("One receipt, one invoice."), or what is true of it that a reader would otherwise assume wrong. it is never a restatement, never advice, never a warning about a refusal the server already makes, and never a sentence containing efficiently, seamlessly, ensure, empower, comprehensive or robust.
- the line is drawn by one component the frame owns (`views.Note`), placed directly beneath the `h1` by the screen that has something true to say. `Layout` cannot draw it itself: every heading sits inside its own width-constrained `<article>`, and on five pages the browser tab and the heading deliberately read differently ("Payment against INV-5" above "Receipt for Cash INV-5"), so the frame does not own the heading. the component is the frame's, so the styling and the rule live in one place rather than being retyped per screen.
- a screen that has nothing true to say in one line draws none; the absence of a line is not a defect, and one is never added for symmetry. D24's other bullets stand unchanged: no helper text under an input, no paragraph above a form warning that the server will refuse, empty states still one line with no advice.

**why** the sidebar is not a style choice, it is arithmetic: thirteen destinations do not fit a row without wrapping, and a wrapping row hides the active page's own position, which is the one thing navigation exists to show. grouping by task rather than by table follows the same rule the screens were written against: the counter works a day in a particular order, and the nav should read in that order. on the tagline, the test is who the sentence is for. a line that restates the heading is for nobody; a line that tells a clerk the one thing about the screen they would otherwise get wrong is for the person standing at the counter in a hurry, which is the reader D24 named in the first place.

**cost** a sidebar eats width that tables want, so at 360px it has to become something else (a wrapping row above the content, not a drawer: a drawer is a second navigation and D24 already spends the frame's budget on one). the one-line rule is a judgement call rather than a ban, so it can be argued into filler again, and it has to be checked by reading rather than by grep. and a group heading that appears for one role and not another means the sidebar is not identical for every user, which is correct but means a screenshot in the docs is nobody's screen but the owner's.
