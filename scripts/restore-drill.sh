#!/usr/bin/env bash
# restore-drill.sh — prove a backup of the book can be read back.
#
#   scripts/restore-drill.sh SOURCE_DSN TARGET_DSN
#
# SOURCE is the database being backed up: production, or a copy of it. TARGET
# must be an empty database, because a drill that restores over a live book is
# not a drill. The script dumps the source, restores it into the target, and
# then makes the two answer the same questions: every table's row count, the
# money the business is owed, and the guards that make the book hard to lie in.
#
# It exits non-zero on the first difference. That is the point: a backup nobody
# has restored is a hope, and the cutover is signed off on this script's output.
#
# Two things it cannot prove, and says so at the end: that the dump is
# readable by the app binary (boot it against TARGET and issue an invoice), and
# that the file the dump went to still exists tomorrow (that is the retention
# question, and Neon's own point-in-time restore answers it).
set -euo pipefail

if [ "$#" -ne 2 ]; then
	echo "usage: $0 SOURCE_DSN TARGET_DSN" >&2
	exit 2
fi

source_dsn="$1"
target_dsn="$2"

if [ "$source_dsn" = "$target_dsn" ]; then
	echo "refusing to restore a database into itself" >&2
	exit 2
fi

workdir="$(mktemp -d)"
dump="$workdir/book.dump"
trap 'rm -rf "$workdir"' EXIT

psql_at() { psql -X -q -A -t -v ON_ERROR_STOP=1 "$@"; }

# 1. The target has to be empty. An empty database still has the public schema,
#    and nothing else.
tables="$(psql_at -d "$target_dsn" -c "select count(*) from pg_tables where schemaname = 'public'")"
if [ "$tables" != "0" ]; then
	echo "the target already holds $tables tables; a restore drill needs an empty database" >&2
	exit 2
fi

# 2. The dump is the backup. Custom format, so pg_restore can be selective with
#    it and so it is compressed without being a shell pipeline.
echo "dumping the source"
pg_dump --format=custom --no-owner --file="$dump" "$source_dsn"

# 3. app_rw is the role every runtime query drops to, and the dump carries the
#    grants that make it powerless in the ways that matter. The role itself is
#    cluster wide, so it is created here when the target cluster has never run
#    this app; the migration runner does the same thing on a fresh install.
psql_at -d "$target_dsn" <<'SQL'
do $$
begin
    if not exists (select 1 from pg_roles where rolname = 'app_rw') then
        create role app_rw nologin;
    end if;
end
$$;
SQL

echo "restoring into the target"
pg_restore --no-owner --exit-on-error -d "$target_dsn" "$dump"

# 4. The same questions on both sides. Row counts are asked of every table, so
#    a table that did not come back cannot hide behind the ones that did.
row_counts() {
	psql_at -d "$1" <<'SQL'
select tablename || ' ' ||
       (xpath('/row/c/text()',
              query_to_xml(format('select count(*) as c from public.%I', tablename),
                           false, true, '')))[1]::text
from pg_tables
where schemaname = 'public'
order by tablename;
SQL
}

# A drill against an empty book passes for the wrong reason: nothing in,
# nothing out. The source has to hold what every real one holds.
if [ "$(psql_at -d "$source_dsn" -c "select count(*) from tenants")" = "0" ]; then
	echo "the source has no tenants, so there is nothing to restore; point this at a real book" >&2
	exit 2
fi
for table in users documents vehicles; do
	if [ "$(psql_at -d "$source_dsn" -c "select count(*) from $table")" = "0" ]; then
		echo "the source has no rows in $table, so this drill would pass on nothing" >&2
		exit 2
	fi
done

# The money the business is owed and the money it paid for the yard, which is
# what a restore that "looks fine" gets wrong: the rows come back, the amounts
# do not.
money() {
	psql_at -d "$1" <<'SQL'
select 'documents ' || count(*) || ' ' || coalesce(sum(price_cents), 0) || ' ' ||
       coalesce(sum(down_cents), 0) || ' ' || coalesce(sum(balance_cents), 0)
from documents
union all
select 'payments ' || count(*) || ' ' || coalesce(sum(amount_cents), 0) || ' 0 0'
from payments
union all
select 'vehicles ' || count(*) || ' ' || coalesce(sum(cost_cents), 0) || ' ' ||
       coalesce(sum(price_cents), 0) || ' 0'
from vehicles
order by 1;
SQL
}

# The guards are part of the book. A restore that brings the rows back without
# them hands back a database where money can be edited in place.
guards() {
	psql_at -d "$1" <<'SQL'
select 'rls_forced ' || count(*)
from pg_class c
join pg_namespace n on n.oid = c.relnamespace
where n.nspname = 'public' and c.relkind = 'r' and c.relrowsecurity and c.relforcerowsecurity
union all
select 'triggers ' || count(*) from pg_trigger where not tgisinternal
union all
select 'app_rw_can_delete_append_only ' || count(*)
from information_schema.role_table_grants
where grantee = 'app_rw' and privilege_type = 'DELETE'
  and table_name in ('documents', 'payments', 'alerts', 'audit_log', 'day_ends',
                     'day_end_lines', 'import_runs', 'reconciliation_runs',
                     'reconciliation_lines', 'document_pdfs', 'document_series',
                     'stock_movements')
union all
select 'app_rw_is_powerful ' || count(*)
from pg_roles where rolname = 'app_rw' and (rolsuper or rolbypassrls)
order by 1;
SQL
}

failed=0
compare() {
	what="$1"
	before="$2"
	after="$3"
	if [ "$before" = "$after" ]; then
		echo "  ok    $what"
		return
	fi
	failed=1
	echo "  WRONG $what" >&2
	diff <(echo "$before") <(echo "$after") >&2 || true
}

echo "comparing the source with the restored copy"
compare "row counts" "$(row_counts "$source_dsn")" "$(row_counts "$target_dsn")"
compare "money" "$(money "$source_dsn")" "$(money "$target_dsn")"
compare "guards" "$(guards "$source_dsn")" "$(guards "$target_dsn")"

if [ "$failed" -ne 0 ]; then
	echo "the restore does not match the source; this backup is not a backup yet" >&2
	exit 1
fi

echo "the restored copy matches the source"
echo
echo "not proved here, and to be done by hand before a cutover is signed off:"
echo "  - boot the app against TARGET_DSN and issue one invoice, print it, then void it"
echo "  - confirm the dump file or the point-in-time window still exists tomorrow"
