# at the counter

what each screen is for, in the order a day uses them. one page, so it can be
printed and left by the till.

## before a paper can be written

a document picks a customer and a unit that already exist, so those two come
first:

- `/customers` — name, nic, address, phone. one row per person, and the nic is
  unique, so an existing customer is found rather than typed twice.
- `/vehicles` — the yard. make, model, chassis, engine, year, colour, cost,
  asking price. the chassis is unique and the unit starts `AVAILABLE`.

## a sale

1. `/documents` → new document → **sales (cash)** or **sales (leasing)**.
2. choose the customer and the unit, then the price and what was paid. a
   leasing sale keeps the balance the finance company owes.
3. a discount above the owner's percent stops and asks for an approval at the
   counter: the approver types **their own** password, so the name on the
   approval is theirs.
4. save. the number is allocated in the same transaction as the paper, so two
   clerks never share one, and a rollback leaves no gap. a serial looks like
   `26NOV_GM01_00001`: year, month, the four characters from `/settings`, and
   the count for that month.
5. print. the first pdf is kept; a reprint is the same bytes as the first.
6. the unit is `SOLD` and off the yard from the same transaction, so it cannot
   be sold twice.

other documents on the same screen: **quotation** (converts to a sale once),
**advance receipt** (money held against a unit, which is `RESERVED`), and
**proforma** (singer paper only, because the leasing company's form is theirs).

## money in

`/payments` — a receipt pays one named invoice. it can never be more than that
invoice still owes. money handed back is a **reversal dated the day it
happens**; a receipt is never edited, because the customer is holding a copy.

## the end of the day

`/day-end` — count the drawer, type the counted total, close. the server
computes what should be there, stores the difference, and compares it with the
variance threshold. past it, an alert is raised on its own.

a close is insert only. once the day is closed, a `z` cannot be changed by
anyone, and no money can be dated on it.

## when something is wrong

a paper issued in error is **voided with a reason**, never deleted. it keeps its
number, the unit goes back to the yard, and the void appears on `/alerts`.

## what is refused, and where it says so

| the attempt | what stops it |
|---|---|
| a discount above the limit | an approval by someone holding `document.approve` |
| a receipt above the balance | the balance |
| money dated on a closed day | the close |
| a document dated before the backdate limit | the limit on `/settings` |
| editing a printed paper | the fields are frozen by a trigger |
| a second unit with the same chassis | the chassis is unique |

## who sees what

a clerk sees their own documents and their own payments. anyone holding
`document.read.all` sees the whole shop. `/reports`, `/import`, `/reconciliation`,
`/settings` and `/users` are not on a clerk's screen at all, because the header
only offers a page that will open.
