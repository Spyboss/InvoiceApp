# the nightly dump

A railway cron service with a volume at `/backups`. Each run writes one dump of
the whole book, proves `pg_restore` can read it, and deletes the ones past
`KEEP_DAYS`. The script is `scripts/backup.sh`; the drill that restores one is
`scripts/restore-drill.sh`.

The image is the same major version as the database, because `pg_dump` refuses
a server newer than itself. `bash` is installed because the script is written
for it, not for busybox ash.

`docs/CUTOVER.md` is where this is wired up, and why `DATABASE_URL` here has to
be the database owner rather than the role the app runs as.
