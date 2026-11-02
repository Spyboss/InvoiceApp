# the two files this app reads

step 7 brings the paper book in from the streamlit app. two csv files carry it: `invoices.csv`, exported by the old app, and the stock sheet the dealership keeps by hand. this page is the contract for both — what a row means, what is refused, and what happens to a row that is uploaded twice.

the parsers live in `internal/legacy`; the meaning of a row and the refusals live in `internal/store/import.go`. this page describes them, it is not a second copy of them: when the two disagree, the code is right and this page is a bug.

## rules that hold for both files

- **utf-8, comma separated, one header row.** a leading byte order mark is tolerated, and so are `\n` and `\r\n`. header names are compared without case, spaces or underscores, so `invoice_type`, `Invoice Type` and `invoicetype` all name the same column; a column may sit in any order, and extra columns are ignored.
- **all or nothing.** one bad row refuses the whole file and nothing is written. every problem is reported, on the line it belongs to, so one correction pass fixes it.
- **a run is recorded.** each upload writes an `import_runs` row holding the file name, its sha256, the totals the file claimed and the totals the database held afterwards. the database refuses to store a run where those two disagree, so an import that did not balance cannot be reported as one.
- **uploading the same file again changes nothing.** rows the app already holds are matched by their own identity (`invoice_no` for a paper, `chassis_no` for a unit), counted as *already in the book*, and left untouched.
- **10 MB limit** per file.
- money is read as the old app wrote it: `1250000.0`, `1,250,000.00` and `Rs. 1250000` all read as twelve lakh fifty thousand rupees, and the noisy tail of a float (`900000.0499999999`) is recovered to the cent the paper said. the stock sheet is typed by a person, so it is read strictly: at most two decimal places, and three is a typo rather than a rounding. both are stored as whole cents from then on, and a negative amount is refused.
- a date is read as `2026-10-30`, `2026/10/30`, `30-10-2026` or `2-1-2026` (the separator and the zero padding may differ). a written out month, or a two-digit year, is refused rather than guessed, because guessing wrong moves a sale into another month.

## invoices.csv

the header must carry at least `invoice_type`, `invoice_no`, `date`, `customer`, `chassis`, `price`, `down`, `balance`. all eighteen columns the old app wrote are read:

| column | meaning | notes |
|---|---|---|
| `invoice_type` | `SALES`, `PROFORMA` or `ADVANCE` | anything else is refused |
| `invoice_no` | the number printed on the paper | kept exactly; never re-allocated |
| `date` | the day the paper was issued | decides the year of the number |
| `customer` | the name on the paper | required |
| `nic` | the customer's nic | used to match an existing customer |
| `cust_addr` | the customer's address | |
| `delivery` | the delivery address | |
| `model` | the model sold | required by the vehicle row |
| `engine` | engine number | |
| `chassis` | chassis number | required, at least 5 characters |
| `color` | colour | the old app's spelling of the column |
| `price` | the sale price | must equal `down + balance` |
| `down` | what the customer paid at issue | |
| `balance` | what is still owed | |
| `finance_company` | the leasing company | required on a leasing sale and a proforma |
| `finance_address` | the leasing company's address | |
| `dealer` | the dealer name | ignored: this app prints the tenant's own name |
| `payment_method` | `Cash`, `Leasing`, … | ignored: the split below decides the document type |

### how a `SALES` row is read

the old app numbered cash and leasing invoices in one series, so the kind alone does not say which one a row was. what is still owed does:

- `balance = 0` → a **cash invoice** (`SALES_CASH`). the sale is paid in full.
- `balance > 0` → a **leasing sale** (`SALES_LEASING`). it needs a finance company and something left for the finance company to pay.

`PROFORMA` and `ADVANCE` map straight across. an advance must record a deposit that is more than nothing and less than the whole price; anything else is a sale, not a deposit.

### what a row does to the yard

| kind | stock movements |
|---|---|
| cash or leasing sale | `RECEIVE` then `SELL` — the unit arrives and leaves with the paper |
| advance | `RECEIVE` then `RESERVED` — the unit is held for the customer |
| proforma | `RECEIVE` only — a proforma does not move a unit |

a unit that has already left the yard cannot leave twice, so re-sending a sold unit's paper fails rather than moving stock twice.

### what is refused

- a row whose `price` is zero, or where `down + balance` does not equal `price`
- a leasing sale or proforma with no finance company, or one that leaves nothing owing
- an advance with no deposit, or one for the whole price
- a blank type, number, customer, model or chassis
- two rows with the same type and number, or two rows selling the same chassis
- a chassis shorter than 5 characters
- a number over 32 characters, a customer name over 120, a nic over 20, an address over 500, a model over 80, an engine or chassis or colour over 40, a finance company over 120
- a date outside 2000–2100, or one this parser cannot read
- a number the app already holds under a different customer, chassis, date or money — the file disagrees with the book, and a person decides which is wrong

`default_make` is part of the form, not the file: the invoices file carries a model and no make, and a vehicle row needs one. a stock sheet that names a make on every row does not need it.

### numbers after the import

imported papers keep the number the old app gave them. afterwards the app's own counters are lifted above the highest imported number for that year, so the next paper continues the paper book instead of colliding with it. the two sales series split one legacy series, so they are lifted together to the higher of the pair.

## the stock sheet

`/import/template` hands out the header, and nothing else. every column but `chassis_no`, `model` and `price` may be blank:

```
stock_no,make,model,vehicle_type,variant,chassis_no,engine_no,colour,yom,cost,price,received_on,status,notes
```

| column | meaning | notes |
|---|---|---|
| `stock_no` | the yard's own number for the unit | free text |
| `make` | the manufacturer | blank falls back to the form's default make |
| `model` | the model | required |
| `vehicle_type` | `PETROL`, `ELECTRIC`, … | |
| `variant` | variant | |
| `chassis_no` | chassis number | required, identifies the unit |
| `engine_no` | engine number | |
| `colour` | colour | |
| `yom` | year of manufacture | 1990–2100 |
| `cost` | what the dealership paid | may be blank |
| `price` | the asking price | required |
| `received_on` | the day the unit arrived | blank takes the date on the form; it is what stock ageing counts from |
| `status` | `AVAILABLE` or `RESERVED` | blank means `AVAILABLE` |
| `notes` | free text | |

a unit that has left the yard is not on this sheet: it left with a sale, and the sale import is what puts it on the record. so `SOLD` and `RETURNED` are refused here.

## the parallel month

the import is half of step 7. the other half is proving the two apps agree, daily, for a month:

1. at the counter, keep issuing paper in the old app.
2. each evening, upload that day's `invoices.csv` to `/reconciliation` over the month to date.
3. the run compares the file with the papers this app imported, per day and per document type, keeping papers, price, down and balance apart.
4. a difference writes a `RECONCILIATION` alert in the same transaction and names the first day to open. an agreement writes nothing.

reconciliation only compares papers that came in from a file. a paper this app issued itself is not in that file, so it is not a difference — which is the point: this check asks whether the book that was imported is still the book the old app holds.

## the run, on the page

a good upload redirects to `/import?run=<id>` or `/reconciliation?run=<id>` and the page reads the run back from the database, so what is shown is what was stored. a refusal stays on the form with the typed values kept, because that is the file the person is about to fix.

who may do what: `import.run` is OWNER only. `reconcile.run` is OWNER, MANAGER and ACCOUNTS. the csv carries the whole book, so a salesperson sees neither page.
