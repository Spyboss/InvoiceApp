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
templ generate -check           # fails if a .templ change was not regenerated
golangci-lint run ./...           (if installed)
```

plus: server boots, the new screen works on a 360px wide viewport, and the step's acceptance test in `docs/PLAN.md` is true.

## commands

```bash
go run ./cmd/server            # local server (needs .env); migrations run at boot
go test ./...                  # all tests (set TEST_DATABASE_URL for the db ones)
templ generate                 # after editing .templ files
psql "$DATABASE_URL"           # inspect; the runtime role is app_rw, admin creds in DATABASE_URL
```

the test database must exist: `createdb invoiceapp_test` and the connecting role needs `createrole` the first time, because migration 0001 creates the `app_rw` role.

## current state

step 6 done: `/settings` and `/users` are the owner's screens (company details, the discount approval percent, the variance threshold, the backdate limit, the alert email and telegram chat; list everyone, change a role, activate or deactivate, reset a password), each refusing anyone without the permission before it loads. a `users_keep_one_owner` trigger (USR01) refuses removing the last active owner and MANAGER no longer carries implicit `user.manage`. approval pins (C8/C9) re-authenticate the approver's own password against the tenant's users and check the effective `document.approve` permission, so a clerk cannot forge a sign-off; both people end up on the document and in the audit log. a void (C4) keeps the paper and its number, adds who/when/why, reverses the sale's stock so the unit returns to the yard, and refuses a closed day or a converted quotation. the `alerts` table is insert-only (the sender may update only status/attempts/last_error/sent_at), RLS forced, delete and truncate revoked; a void, a variance past the threshold, and a numbering gap (C2, scanned across all three number series at close) each write an alert in the same transaction as the event. delivery is email and/or telegram, both optional and off when unconfigured; a failed send stays pending with the error kept until the three-attempt budget runs out. `/alerts` lists them and is gated on `alert.view`.

step 5 done: a receipt pays one named invoice (`payments.document_id`), can never exceed the outstanding balance (store check plus the `payments_within_balance` trigger, `PAY02`), and is never edited; money handed back is a reversal dated the day it happens (`PAY03`). money cannot be dated on a day whose z is closed (`payments_on_open_day`, `DAY01`), and a close is insert only, so a z cannot be changed by anyone (`day_ends_closed`). the z stores its totals and its frozen lines, expected cash is computed by the server, and variance is enforced by a check constraint. `/reports` covers sales by type and user, stock by status and ageing, and receivables with ageing buckets and a finance company split, each scoped to the caller's own documents unless they hold `document.read.all`, each exportable as a safe csv. no receipt or z pdf yet, no reconciliation view: those wait on a decision (D22).

step 4 done: `/documents` issues quotations, cash and leasing invoices, advance receipts and proformas; `document_series` numbers them atomically per tenant/type/year; a sale sets the unit SOLD and writes the `SELL` movement in the same transaction; a quotation converts once; printed fields are frozen by the `documents_immutable` trigger. the first pdf is archived in `document_pdfs` with its sha256 and served from there on. proformas are singer paper only, rebuilt from `BIBILE 01.docx` and compared against a libreoffice render within about 1pt on the table and body. printing uses the gotenberg service (`gotenberg/gotenberg:8`, basic auth) over railway private networking. staging is live at `https://app-staging-895f.up.railway.app` (project `invoiceapp`, branch `rewrite/go-htmx`; app and gotenberg on railway singapore, database on neon `invoiceapp` id `blue-hat-64568041`, `aws-ap-southeast-1`, postgres 18). the next commit is step 7 (import + parallel run) as specified in `docs/PLAN.md`.

## reference material

- `C:\Users\Gunawardana Motors\Downloads\BIBILE 01.docx` — the leasing company's proforma, ground truth for the proforma pdf. kept out of the repo because it carries a customer's nic and phone; the geometry and extracted assets are in `internal/pdf/`.
- `web_app.py` — the live behaviour being replaced (read only, do not change).
- `feat/erp-ts` — earlier attempt; useful for the drizzle schema shape, the transaction guards, and the derived proforma geometry.
