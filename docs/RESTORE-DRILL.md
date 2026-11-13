# the restore drill

step 8 is signed off on one thing above all: a backup that has been read back. this page is what the drill does, how to run it, and what the rehearsal on 2026-11-03 answered.

the script is `scripts/restore-drill.sh`. it is deliberately small and has no dependencies beyond `pg_dump`, `pg_restore` and `psql`.

the connection to SOURCE has to be able to bypass row level security. the tables that hold the book force it, so `pg_dump` either reads every tenant's rows or fails trying, and a role that cannot get past a policy would otherwise be compared with an empty target and pass. on neon the owner role bypasses; on a local cluster it is `postgres`, whose password is not in this repository. the script asks this before it dumps and names the role it is running as.

## what it does

```
scripts/restore-drill.sh SOURCE_DSN TARGET_DSN
```

1. **refuses to start unless the target is empty.** a drill that restores over the live book is not a drill.
2. **refuses a source with nothing in it.** a drill against an empty book passes for the wrong reason: nothing in, nothing out. the source must have a tenant, and rows in `users`, `documents` and `vehicles`.
3. **dumps the source** in custom format, `--no-owner`.
4. **creates `app_rw`** in the target cluster when it is missing. the role is cluster wide and the migration runner creates it on a fresh install; a cluster that has never run this app has no such role, and every grant in the dump that mentions it would be skipped.
5. **restores** with `--exit-on-error`.
6. **asks both databases the same three questions** and diffs the answers:
   - **row counts**, of every table in `public`. a table that did not come back cannot hide behind the ones that did.
   - **money**: documents (papers, price, down, balance), receipts (count, total), vehicles (count, cost, price). this is what a restore that "looks fine" gets wrong: the rows come back, the amounts do not.
   - **guards**: how many tables have RLS forced, how many triggers exist, whether `app_rw` can delete from any append-only table, and whether `app_rw` is superuser or `bypassrls`. a restore that brings the rows back without the guards hands back a database where money can be edited in place.
7. exits non-zero on the first difference.

## the rehearsal, 2026-11-03

source `invoiceapp_ui` (the local book holding a real import run and three legacy papers), target a fresh `invoiceapp_drill` on the same cluster:

```
dumping the source
restoring into the target
comparing the source with the restored copy
  ok    row counts
  ok    money
  ok    guards
the restored copy matches the source
```

then the app itself was booted against the restored copy (`DATABASE_URL=…/invoiceapp_drill`, port 8100) rather than only interrogated with `psql`:

- migrations applied: **0**. the dump carried the schema and every migration row, so the runner had nothing to do. that is the expected answer and the one to check on a real restore: a migration the dump does not know about means the backup predates it.
- `/documents` listed the three imported papers under the numbers the old app printed (1001 cash, 1002 leasing, 1003 advance), with the customer, the unit and the amount intact.
- `/stock` read 4 units on hand, 3 available, 1 reserved, and a ledger in which every `RECEIVE` and `SELL` came back with the papers.
- the owner account signed in with its own password, so `users`, the password hash and `sessions` all survived.

## what it cannot prove, and what to do by hand

- **that the app can issue and print a paper.** boot against the target, issue one invoice, print it, void it. printing needs the gotenberg service; run this part on staging, not on a laptop.
- **that the file still exists tomorrow.** the dump is deleted with its temporary directory when the drill ends, and retention is the platform's job. the database keeps **six hours** of point-in-time restore on its current plan, which covers a mistake found the same day and nothing older; that is why the nightly dump in `backup/` exists and goes to a different provider.
- **that the schedule ran.** the drill proves a dump restores; it does not prove one was taken tonight. `docs/CUTOVER.md` step 7 is where the daily dump is wired up, and the sign-off checklist is where somebody looks at one.

## before cutover

1. run the drill against the production database as source, into a fresh database, as a role that bypasses rls.
2. boot the current release against that fresh database and issue, print and void one invoice.
3. confirm the dump the backup service wrote restores too, and that the six-hour window is understood for what it does not cover.
4. keep the output of the run with the sign-off.
