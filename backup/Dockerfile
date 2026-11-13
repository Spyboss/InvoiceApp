# the nightly dump

# A railway cron service with a volume mounted at /backups, and this file as
# its dockerfile (RAILWAY_DOCKERFILE_PATH=backup/Dockerfile). It writes one dump
# of the whole book, proves pg_restore can read it, and deletes the ones past
# KEEP_DAYS. scripts/backup.sh does the work; scripts/restore-drill.sh proves a
# dump can be put back.
FROM postgres:18-alpine

RUN apk add --no-cache bash

COPY scripts/backup.sh /usr/local/bin/backup.sh
RUN chmod 0755 /usr/local/bin/backup.sh

# Everything the script needs comes from the service's own variables, so the
# image holds no settings. DATABASE_URL must be the database owner: row level
# security is forced on the tables that hold the book, and a role that cannot
# bypass it dumps a file that is not the book. See docs/CUTOVER.md.
ENTRYPOINT ["sh", "-c", "exec /usr/local/bin/backup.sh \"$DATABASE_URL\" \"${BACKUP_DIR:-/backups}\" \"${KEEP_DAYS:-14}\""]
