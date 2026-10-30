# plan

living document. update the status column at the end of every step, in the same commit as the work. one step = one reviewable commit (or a small series), never a big bang.

status: `todo` · `in progress` · `done` · `blocked`

| # | step | deliverable | acceptance | status |
|---|---|---|---|---|
| 0 | toolchain | go, postgres, templ installed on the dev machine | `go version`, `psql --version`, `templ version` all print | done |
| 1 | skeleton + deploy | config, router, middleware, `/healthz`, db ping, dockerfile, railway.json, ci, `.env.example` on branch `rewrite/go-htmx` | `go vet ./...` + `go test ./...` green; server boots; `curl /healthz` returns version+commit; staging deploy green; logs are json | in progress |
| 2 | tenancy + auth | `tenants`, `users`, `sessions`, `audit_log`, rls, login/logout, csrf, permissions, first-run owner invite | two tenants on one db cannot see each other's rows (test); login/logout/lockout work; every write produces an audit row | todo |
| 3 | customers + stock | customers crud, vehicles crud, stock movement ledger, chassis uniqueness, statuses | stock cannot go negative (test); duplicate chassis rejected per tenant; movement history matches on-hand | todo |
| 4 | documents + numbering + pdf | quotation, sales cash, sales leasing, advance receipt, proforma; atomic numbering; gotenberg pipeline; pdf archive + hash | numbers are unique per tenant/type/year under concurrent creates (test); proforma pixel-diff vs `BIBILE 01.docx` within tolerance; pdf sha256 stable | todo |
| 5 | payments + day end + reports | payment receipts, allocation, reversal, z report, sales/stock/receivables reports, csv export | overpayment rejected; day-end variance computed and stored; z report immutable after close | todo |
| 6 | settings + approvals + notifications | tenant settings screens, permission editor, audit viewer, approval pins, email + telegram alerts | a non-owner cannot reach settings (test); discount above floor requires approval; alerts fire on void/variance/gap | todo |
| 7 | import + parallel run | csv import of existing invoices and stock, reconciliation against streamlit totals | imported totals match the csv source exactly; one month of parallel running with daily diff | todo |
| 8 | cutover | production domain, backups + restore drill, staff training doc, streamlit kept read-only for a month | restore drill passes; staff issue a real invoice unaided; owner signs off | todo |

## current step

step 1 is built and green locally (go vet, go test, go test -race, server boots, /healthz answers with a real database ping). what remains for step 1 is the first railway staging deploy of branch `rewrite/go-htmx`.

step 2 (tenancy + auth) starts only after that deploy is green and the owner has clicked through `/healthz` on the deployed url.

## out of scope for v1 (see D11)

parts inventory, service jobs, suppliers/purchase orders, multi-branch, subscription billing, passkeys, public api, offline writes, template editor.
