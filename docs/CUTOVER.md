# cutover

D28 dropped the parallel month: this goes live on the real business and the first month is the test. this is the order to do it in, what to check at each step, and the two things that cannot be undone.

## 1. what carries over

the streamlit app has no database. it keeps `invoices.csv` and `invoice_log.csv` beside itself, and neither file exists on this machine, so there is no dump to restore. a dump of a database that does not exist is not worth taking.

what the app did leave is its printed output. twelve piaggio pdfs were mined into `docs/invoices.csv` (D32) and go in through `/import` while the book is still empty. they have to go in before the first live paper rather than after, because the counter starts at 0001 and the old book already holds a 0001.

## 2. a new database, empty

create a **new neon project** in the same region as the app (singapore), then use its owner connection string.

not a branch of the staging project: a branch is a copy, and it would carry staging's tenants, test customers and test papers into the book the business bills on. production starts with nothing in it, and the migrations build the schema.

the owner role is the right string for two jobs and the app is not one of them:

- **migrations and backups** run as the owner. on neon the owner role bypasses forced row level security, which is what lets a dump hold every tenant's rows.
- **the app** takes the same string but drops to `app_rw` on every connection (`internal/store/db.go`), a role that owns nothing, is not a superuser, and cannot see past a tenant's policy.

## 3. the app

create a railway environment `production` and duplicate the `app` and `gotenberg` services into it, so they keep the private network name `gotenberg` the app already uses.

variables on `app`:

| variable | why |
|---|---|
| `APP_ENV` | `production`. it refuses `TENANT_DEV`, so localhost can never resolve to a tenant on this service |
| `DATABASE_URL` | the new project's owner string, from step 2 |
| `GOTENBERG_URL` | `http://gotenberg:3000`, and the same basic auth pair the staging service uses |

printing is the only thing that needs gotenberg: with it unset the app still runs, and only the print button refuses. alerts are optional the same way (`ALERT_SMTP_*`, `ALERT_TELEGRAM_TOKEN`); with neither set, alerts still queue and still show on `/alerts`, they just leave by no route.

deploy. migrations run at boot and the log line says how many it applied. `GET /healthz` answers `ok` with a version and a commit, which is the first thing to check when a deploy looks wrong.

## 4. the first account, before the address is public

`/setup` creates the dealership and its owner in one step, and **the first person to submit it becomes the owner**. that is fine on day one and is a race on day two.

so do it in this order: open the service's railway-generated domain, finish `/setup` there, and only then attach the custom domain. by the time the address answers to the public, there is already an owner and `/setup` has nothing left to do.

## 5. the settings that are printed on paper

`/settings`, as the owner:

- company details: these four lines are the letterhead printed on every dealer document, so what is typed here is what a customer reads. the name falls back to the tenant's own name; the address and phones fall back to what the shop used before the screen existed; **the VAT line appears only when a number is set**, so a business that is not registered prints none (D30).
- **invoice prefix `GM01`** (D27). four characters, upper case letters and digits. blank means "derive four characters from the slug again", so a mistake is undone by clearing the box. set it before the first paper, not after: changing it mid-year splits the year's serials across two codes.
- discount approval percent, variance threshold, backdate limit. these are the anti-cheat controls, and changing them now writes an `settings.update` row with who did it and both values (found missing, fixed).
- alert email and telegram chat, if alerts are to leave the building.

## 6. people

`/users`, one account per person, real names, and the role that matches what they do. a password is handed over in person and changed on the first sign in. the last active owner cannot be deactivated, by trigger.

## 7. the nightly dump

the database keeps **six hours** of history on its current plan. that covers a row deleted by mistake ten minutes ago and nothing older, which is not enough for a book that bills a business.

create a service `backup` in the same environment, from this repository, with `RAILWAY_DOCKERFILE_PATH=backup/Dockerfile`, then:

- a volume mounted at `/backups`
- `DATABASE_URL` set to the **same owner string as the app** (see the top of this file: any other role dumps a file that is not the book)
- `KEEP_DAYS=14`
- a cron schedule of `45 18 * * *`. that is 00:15 in colombo, so the dump holds the day that just closed.

each run writes `book-<utc>.dump`, proves `pg_restore` can read it, and deletes the ones older than `KEEP_DAYS`. the newest dump is exempt from pruning by name, so a clock that jumps cannot delete it.

the dumps are on railway, not on neon. that is the point: whatever takes one provider down leaves the other holding a copy.

## 8. the drill, before anything is signed off

a dump nobody has restored is a hope.

```
scripts/restore-drill.sh SOURCE_DSN TARGET_DSN
```

the target must be an empty database. the source must be reachable by a role that bypasses row level security, because a role that does not either stops inside `pg_dump` or compares an empty source with an empty target. on neon that is the owner string; on a local cluster it is `postgres`, whose password is not in this repository.

it compares row counts, the money in documents, receipts and vehicles, and the guards (forced rls, triggers, `app_rw` cannot delete an append-only row and is neither superuser nor `bypassrls`). it exits non-zero on the first difference and names it.

record the date and the outcome in `docs/RESTORE-DRILL.md`. it cannot prove two things, and says so itself: that the app binary can issue and print an invoice from the restored copy, and that a dump exists tomorrow.

## 9. the imported book

`docs/invoices.csv` carries the three papers mined from the printed pdfs (D32). `/import` takes it with `default_make` set to PIAGGIO, the papers keep the number the old app printed, and the counters lift above every imported number, cash and leasing sharing one series (D26). after it the first live paper is 0006.

if a number ever looks wrong, `/reconciliation` compares a fresh export from the old app against the papers that came in from a file, per day and per type. it is a tool for that day, not a routine (D28).

## sign-off

step 8 is done when all of these are true:

- [ ] `/healthz` answers on the production domain, with the commit that is deployed
- [ ] the owner account exists and nobody else created it
- [ ] the invoice prefix reads `GM01` on the settings screen
- [ ] the old app's dump is off the machine and opens with `pg_restore --list`, or there was no old book (step 1)
- [ ] the backup service has written one dump, and `pg_restore --list` reads it
- [ ] the drill ran against production and matched
- [ ] the book starts empty, or the csvs imported and the run's totals equal the file's
- [ ] one staff member issued a real invoice unaided, printed it, and it carries the right serial
