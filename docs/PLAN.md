# plan

living document. update the status column at the end of every step, in the same commit as the work. one step = one reviewable commit (or a small series), never a big bang.

status: `todo` · `in progress` · `done` · `blocked`

| # | step | deliverable | acceptance | status |
|---|---|---|---|---|
| 0 | toolchain | go, postgres, templ installed on the dev machine | `go version`, `psql --version`, `templ version` all print | done |
| 1 | skeleton + deploy | config, router, middleware, `/healthz`, db ping, dockerfile, railway.json, ci, `.env.example` on branch `rewrite/go-htmx` | `go vet ./...` + `go test ./...` green; server boots; `curl /healthz` returns version+commit; staging deploy green; logs are json | done |
| 2 | tenancy + auth | `tenants`, `users`, `sessions`, `audit_log`, rls forced with a runtime `app_rw` role, login/logout, csrf, permissions, first-run owner setup, audit viewer | two tenants on one db cannot see each other's rows (test); login/logout/lockout work; every write produces an audit row | done |
| 3 | customers + stock | customers crud, vehicles crud, stock movement ledger, chassis uniqueness, statuses | stock cannot go negative (test); duplicate chassis rejected per tenant; movement history matches on-hand | done |
| 4 | documents + numbering + pdf | quotation, sales cash, sales leasing, advance receipt, proforma; atomic numbering; gotenberg pipeline; pdf archive + hash | numbers are unique per tenant/type/year under concurrent creates (test); proforma pixel-diff vs `BIBILE 01.docx` within tolerance; pdf sha256 stable | done |
| 5 | payments + day end + reports | payment receipts, allocation, reversal, z report, sales/stock/receivables reports, csv export | overpayment rejected; day-end variance computed and stored; z report immutable after close | done |
| 6 | settings + approvals + notifications | tenant settings screens, permission editor, audit viewer, approval pins, email + telegram alerts | a non-owner cannot reach settings (test); discount above floor requires approval; alerts fire on void/variance/gap | done |
| 7 | import + parallel run | csv import of existing invoices and stock, reconciliation against streamlit totals | imported totals match the csv source exactly; one month of parallel running with daily diff | todo |
| 8 | cutover | production domain, backups + restore drill, staff training doc, streamlit kept read-only for a month | restore drill passes; staff issue a real invoice unaided; owner signs off | todo |

## current step

step 6 (settings + approvals + notifications) is done. what it shipped, on top of the store layer:

- `/settings`: the owner sets company details, the discount approval percent, the day-end variance alert threshold, the backdate limit, and the alert email and telegram chat (blank = that channel off). a percent outside 0-100 is refused.
- `/users`: the owner lists everyone, changes a role, activates or deactivates, and resets a password. a `users_keep_one_owner` trigger (USR01) refuses removing the last active owner, and MANAGER no longer carries implicit `user.manage`, so widening access is a decision someone owns.
- approval pins (C8/C9): a discount past the tenant's percent, or a date past the backdate limit, brings the form back with the rule that tripped and the pin block open. the pin is the approver's own password, re-authenticated against the tenant's users and then checked against the effective `document.approve` permission, so a clerk cannot forge it. both the clerk and the approver end up on the document and in the audit log.
- void (C4): `/documents/{id}/void` keeps the paper and its number and adds who, when and why; it reverses the sale's stock movement so the unit returns to the yard, releases an advance's reservation without a movement, and refuses a closed day or a converted quotation. printed fields stay frozen.
- alerts (C12): an `alerts` table, insert-only for the app role apart from the sender's own update on status/attempts/last_error/sent_at, RLS forced, delete and truncate revoked. a void, a drawer past the threshold (recorded inside `CloseDay`'s transaction), and a numbering gap (C2, scanned at close across all three number series) each write an alert. delivery is email and/or telegram, both optional and off when unconfigured; with no channel the alert is marked sent because the page has it. a failed send stays pending with the error kept until the store's three-attempt budget runs out. `/alerts` lists them and is gated on `alert.view`.

step 6 acceptance, all covered by tests: a non-owner cannot reach `/settings` or `/users`; a discount above the floor or a backdate beyond the limit needs a pin and a wrong or unauthorised pin is refused; a void keeps the paper, returns the unit, and records an alert; alerts fire on void, variance and numbering gap and are gated on `alert.view`.

step 5, for the record: a receipt pays one named invoice and can never exceed what it still owes, in the store and in a database trigger. a receipt is inserted once; handing money back is a reversal dated the day it happens, so a counted day is never rewritten. money cannot be dated on a closed day, and a close is insert only, so a z cannot be edited by anyone. the z stores its totals and its lines as they were at close time, and exports as csv.

step 4, for the record: `/documents` issues quotations, cash and leasing invoices, advance receipts and proformas. numbers come from `document_series`, bumped in the same transaction as the document, so concurrent clerks never share one and a rollback leaves no gap. a sale sets the unit sold and writes the `SELL` movement in that transaction; a quotation converts once. printed fields are frozen by a database trigger, and the first pdf is archived with its sha256, so later downloads serve the same bytes. the proforma was compared against a libreoffice render of `BIBILE 01.docx`: the table and body agree within about 1pt.

deliberately not in step 5, and waiting on a decision: the receipt pdf, the z pdf and the report pdfs; the reconciliation view per day and user (C11); and the variance alert threshold, which needs tenant settings. see D22.

## out of scope for v1 (see D11)

parts inventory, service jobs, suppliers/purchase orders, multi-branch, subscription billing, passkeys, public api, offline writes, template editor.
