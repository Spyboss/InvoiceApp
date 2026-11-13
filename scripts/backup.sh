#!/usr/bin/env bash
# backup.sh — write one dump of the book, and keep the last few.
#
#   scripts/backup.sh DSN DIRECTORY [KEEP_DAYS]
#
# A dump is only a backup if it is still there tomorrow, and only a backup if
# pg_restore can read it. The database keeps six hours of history on its
# current plan, so a mistake found the next morning has nothing behind it; this
# writes a dump somewhere that is not the database, proves the archive reads
# back, and removes the ones past KEEP_DAYS so the volume holds the recent past
# instead of filling up.
#
# It refuses an empty book. A dump of an empty database restores cleanly and
# passes every check a drill makes, which is how a backup job runs for a year
# and holds nothing.
set -euo pipefail

if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
	echo "usage: $0 DSN DIRECTORY [KEEP_DAYS]" >&2
	exit 2
fi

dsn="$1"
dir="$2"
keep="${3:-14}"

case "$keep" in
'' | *[!0-9]*)
	echo "KEEP_DAYS must be a whole number of days, not '$keep'" >&2
	exit 2
	;;
esac
if [ "$keep" -lt 1 ]; then
	echo "KEEP_DAYS must keep at least one day, or this deletes what it just wrote" >&2
	exit 2
fi

psql_at() { psql -X -q -A -t -v ON_ERROR_STOP=1 "$@"; }

# Row level security is forced on the tables that hold the book, so a role that
# cannot bypass it reads nothing past its policy, and pg_dump either fails or
# writes a file describing a business that is not there. This is asked first:
# a blocked role still counts the tenants table, which has no policy, so the
# next check would report an empty book and send somebody looking in the wrong
# place. Neon's owner role bypasses; a locally created owner does not.
role_state="$(psql_at -d "$dsn" -c "select rolsuper or rolbypassrls from pg_roles where rolname = current_user")"
if [ "$role_state" != "t" ]; then
	echo "the connection role cannot bypass row level security, so a dump would not hold the book" >&2
	echo "run this as the database owner: neon's owner role bypasses rls; a local cluster needs postgres" >&2
	exit 2
fi

# The book has to hold something. A file describing an empty business is well
# formed and worthless, and the drill would happily restore it.
tenants="$(psql_at -d "$dsn" -c "select count(*) from tenants")"
if [ "$tenants" = "0" ]; then
	echo "the database holds no tenants, so there is no book to back up" >&2
	exit 2
fi

mkdir -p "$dir"
if [ ! -w "$dir" ]; then
	echo "$dir is not writable" >&2
	exit 2
fi

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
final="$dir/book-$stamp.dump"
partial="$final.partial"

# Written under a name nothing reads, then renamed. A job that dies mid dump
# leaves a partial file, and a drill that picks the newest must never find it.
pg_dump --format=custom --no-owner --file="$partial" "$dsn"
chmod 600 "$partial"

# The archive has to list its contents. That is the cheapest proof the file was
# finished, and that whatever restores it later is reading something whole.
if ! pg_restore --list "$partial" >/dev/null; then
	rm -f "$partial"
	echo "pg_restore cannot read the dump back, so it is not a backup" >&2
	exit 1
fi

mv "$partial" "$final"

# Retention, counted from the file's own age. The dump just written is exempt
# by name, so a machine whose clock jumped cannot delete the only copy it has.
find "$dir" -maxdepth 1 -type f -name 'book-*.dump' -mtime +"$keep" \
	! -name "$(basename "$final")" -print -delete

echo "wrote $final ($(wc -c <"$final" | tr -d ' ') bytes)"
echo "kept:"
find "$dir" -maxdepth 1 -type f -name 'book-*.dump' | sort | while read -r file; do
	echo "  $(basename "$file") $(wc -c <"$file" | tr -d ' ') bytes"
done
