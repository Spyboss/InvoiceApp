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
| 5 | payments + day end + reports | payment receipts, allocation, reversal, z report, sales/stock/receivables reports, csv export | overpayment rejected; day-end variance computed and stored; z report immutable after close | todo |
| 6 | settings + approvals + notifications | tenant settings screens, permission editor, audit viewer, approval pins, email + telegram alerts | a non-owner cannot reach settings (test); discount above floor requires approval; alerts fire on void/variance/gap | todo |
| 7 | import + parallel run | csv import of existing invoices and stock, reconciliation against streamlit totals | imported totals match the csv source exactly; one month of parallel running with daily diff | todo |
| 8 | cutover | production domain, backups + restore drill, staff training doc, streamlit kept read-only for a month | restore drill passes; staff issue a real invoice unaided; owner signs off | todo |

## current step

step 4 is done. `/documents` issues quotations, cash and leasing invoices, advance receipts and proformas. numbers come from `document_series`, bumped in the same transaction as the document, so concurrent clerks never share one and a rollback leaves no gap. a sale sets the unit sold and writes the `SELL` movement in that transaction; a quotation converts once. printed fields are frozen by a database trigger, and the first pdf is archived with its sha256, so later downloads serve the same bytes. the proforma was compared against a libreoffice render of `BIBILE 01.docx`: the table and body agree within about 1pt.

step 5 (payments + day end + reports) is next: payment receipts and allocation, reversal, the z report, sales/stock/receivables reports and csv export.

## out of scope for v1 (see D11)

parts inventory, service jobs, suppliers/purchase orders, multi-branch, subscription billing, passkeys, public api, offline writes, template editor.
