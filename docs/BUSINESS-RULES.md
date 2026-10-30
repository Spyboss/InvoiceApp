# business rules

the domain as it actually works at the dealership, written down so code can be checked against it. when something here is wrong, we fix this file first.

## 1. documents

| document | number series | when it is issued | editable after issue |
|---|---|---|---|
| quotation | `QTN` | price offered to a customer, valid 7 days | no (revise = new quote) |
| sales invoice (cash) | `INV` | customer pays in full | no |
| sales invoice (leasing) | `INV` | finance company pays the balance | no |
| proforma | `PRF` | sent to the leasing company so they release funds | no |
| advance receipt | `ADV` | customer pays a deposit to reserve a vehicle | no |
| payment receipt | `RCP` | any subsequent payment against an invoice | no (reverse only) |
| day end (z) | `Z` | cashier closes the day | no |

sales invoices and quotations are our own paper and get redesigned. the proforma is the leasing company's paper (see D10) and must look like `BIBILE 01.docx`.

## 2. numbering

- one series per `(tenant, document_type, year)`.
- 4 digit zero padded (`0001`), configurable prefix per type in settings.
- allocated inside the same transaction as the document, from a counter row locked `for update`. a rolled back invoice does not consume a number.
- gaps are allowed only by rollback, never by deletion. a daily job lists gaps and alerts the owner.
- numbers are printed on the document and stored in the pdf. never rewritten.

## 3. money

- stored and computed as integer cents.
- invoice: `price`, `down`, `balance = price - down`. cash invoices store `down = price`, `balance = 0`.
- leasing: `balance` is the amount the finance company settles.
- payments reduce the invoice balance. a payment cannot exceed the outstanding balance.
- gross margin (internal only) = `price - vehicle.cost`. never printed on customer documents.

## 4. statuses

**vehicle**: `AVAILABLE → RESERVED → SOLD → DELIVERED`, plus `RETURNED`. chassis number is unique per tenant. a vehicle in `SOLD` cannot be sold again (row lock at sale time).
**invoice**: `DRAFT → ISSUED → DELIVERED`, or `VOID` with reason. `ISSUED` is immutable except status transitions.
**quotation**: `OPEN → ACCEPTED → CONVERTED` or `EXPIRED`.
**payment**: `POSTED` or `REVERSED` (with reason, never deleted).

## 5. roles and permissions

tenant roles:
- **OWNER** everything, including settings, users, audit, tenant billing.
- **MANAGER** everything operational, can approve void/discount/backdate/stock adjustment, cannot change tenant billing or delete users.
- **SALES** create quotations, invoices, customers, receive stock; can see own documents and stock; cannot void, cannot change prices below floor, cannot see margin or other users' day-end.
- **ACCOUNTS** payments, receipts, receivables, day-end, reports; cannot create invoices or edit stock.
- **SERVICE** service jobs (v2), parts issue (v2); read-only customer/vehicle.
- **PLATFORM_ADMIN** outside tenancy, support access only, every action audited and visible to the tenant.

permissions are `resource.action` strings granted to roles, with per-user overrides. handlers enforce; templates only hide.

## 6. anti-cheat controls (v1 requirement)

| id | control |
|---|---|
| C1 | document numbers are allocated atomically and are not editable; no manual numbering |
| C2 | daily gap scan per series; any gap is alerted, not silently tolerated |
| C3 | append-only audit log: actor, ip, user agent, action, entity, before/after json, timestamp |
| C4 | no deletes anywhere; `VOID` requires reason + role, original document and pdf hash are kept |
| C5 | stock is an append-only ledger (`stock_movements`); on-hand is derived and reconciled; negative stock is impossible |
| C6 | every pdf generation and download is logged with sha256, actor and count |
| C7 | day-end close: cashier enters counted cash, system computes expected, variance is stored and alerted |
| C8 | price floor per model; discount above the approval threshold needs a manager pin, logged with both users |
| C9 | invoice dates default to today; backdating beyond 0 days needs manager approval and is logged |
| C10 | sessions are server-side, listed per user, revocable; logins, lockouts and device changes are logged |
| C11 | reconciliation view: sales total vs payments vs stock out vs cash banked, by day and user |
| C12 | anomaly alerts: void, override, variance, after-hours activity, missing number, negative stock attempt |
| C13 | issued pdfs are archived immutably (sha256 + r2 object) so any paper copy can be reproduced exactly |
| C14 | quote → invoice link is stored, so a changed quote is visible in the trail |

the point is not that fraud is impossible; it is that fraud leaves a record someone can find in ten seconds.

## 7. day end (z report)

expected cash = opening float + cash payments − cash refunds − cash payouts.
cashier enters counted cash; `variance = counted − expected`; variance beyond a configured threshold alerts the owner. the z report lists: invoices by type, payments by method, voids, discounts, stock movements, and the operator. it is immutable once closed.

## 8. reports

- **day end (z)** as above.
- **sales** per period: count, gross, down, balance, by type and by user.
- **stock**: vehicles by status, ageing days in yard, value at cost and at price.
- **receivables**: invoices with balance > 0, ageing buckets, finance company breakdown.
- **reconciliation**: per day and user, sales vs payments vs stock out.
- export csv for all of the above. pdf for day end, sales, stock, receivables.

## 9. settings (tenant editable)

company name, address, phones, email, vat number, letterhead image, logos, signatories, document prefixes and padding, terms text blocks, finance companies, vehicle makes/models/types/colours, price floors, approval thresholds, day-end variance threshold, users and roles, payment methods, tax display.

templates (html) are not editable. content is.

## 10. glossary

- **proforma** — quote-format document addressed to the leasing company, not a tax invoice.
- **advance** — deposit receipt that reserves a vehicle.
- **leasing** — customer finances through a company; the company pays the balance to the dealer.
- **yom** — year of manufacture.
- **yard** — physical vehicle stock.
- **z report** — end of day cash and sales summary.
