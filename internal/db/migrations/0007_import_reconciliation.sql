-- 0007_import_reconciliation.sql
-- step 7: importing the old app's files, and proving what was imported.
-- forward only: never edit this file after it has run anywhere.

-- The old app kept the whole book in two csv files: invoices.csv and a stock
-- sheet kept by hand. A run is one file, and it records both what the file said
-- and what the database holds now. The check constraint is the acceptance test
-- of this step: a run that does not balance cannot be written down, so no
-- screen can report an import that did not happen.
create table import_runs (
    id            uuid primary key default uuidv7(),
    tenant_id     uuid not null references tenants (id),
    kind          text not null,
    filename      text not null,
    -- The bytes of the file, so two runs over the same file are recognisable
    -- even when somebody renamed it.
    sha256        text not null,
    source_totals jsonb not null,
    stored_totals jsonb not null,
    rows_imported integer not null default 0,
    rows_skipped  integer not null default 0,
    created_by    uuid references users (id),
    created_at    timestamptz not null default now(),
    constraint import_runs_kind_valid check (kind in ('INVOICES', 'STOCK')),
    constraint import_runs_hash_format check (sha256 ~ '^[0-9a-f]{64}$'),
    constraint import_runs_filename_length check (char_length(filename) between 1 and 200),
    constraint import_runs_verified check (source_totals = stored_totals),
    constraint import_runs_counts_non_negative check (rows_imported >= 0 and rows_skipped >= 0)
);

create index import_runs_recent_idx on import_runs (tenant_id, created_at desc);

alter table import_runs enable row level security;

alter table import_runs force row level security;

create policy import_runs_tenant_isolation on import_runs
    using (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
    with check (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);

-- A run is a record of what happened, so it may be written once and read. It is
-- never corrected: if the import was wrong, the file is imported again and a
-- second run says so.
grant select, insert on import_runs to app_rw;
revoke update, delete, truncate on import_runs from app_rw;

-- An imported document is one that already existed on paper: it keeps the
-- number the old app gave it, and it says which run brought it in. The pair is
-- what lets the reconciliation prove the file and the book still agree.
alter table documents
    add column imported_at timestamptz,
    add column import_run_id uuid;

alter table documents
    add constraint documents_import_pair check (
        (imported_at is null) = (import_run_id is null)
    );

-- The run row is written last, because its totals are counted from the rows it
-- just inserted, so the reference is deferred to the end of the transaction.
alter table documents
    add constraint documents_import_run_fk foreign key (import_run_id)
        references import_runs (id) deferrable initially deferred;

create index documents_import_idx on documents (tenant_id, import_run_id)
    where import_run_id is not null;

-- Reconciliation compares a file with the book over a range of days. A run
-- keeps the summary; a line keeps one day of one document type, so a difference
-- points at a day somebody can open rather than at "the import".
create table reconciliation_runs (
    id                    uuid primary key default uuidv7(),
    tenant_id             uuid not null references tenants (id),
    -- The file that was checked, so a row in three weeks says which file the
    -- book was compared with and not only which days.
    filename              text not null,
    sha256                text not null,
    date_from             date not null,
    date_to               date not null,
    difference_papers     integer not null,
    difference_gross      bigint not null,
    difference_down       bigint not null,
    difference_balance    bigint not null,
    -- matched is not something a handler may set: it is true exactly when the
    -- four counts above are zero, and the database decides.
    matched               boolean generated always as (
                              difference_papers = 0
                              and difference_gross = 0
                              and difference_down = 0
                              and difference_balance = 0
                          ) stored,
    created_by            uuid references users (id),
    created_at            timestamptz not null default now(),
    constraint reconciliation_runs_range check (date_from <= date_to),
    constraint reconciliation_runs_hash_format check (sha256 ~ '^[0-9a-f]{64}$'),
    constraint reconciliation_runs_filename_length check (char_length(filename) between 1 and 200)
);

create index reconciliation_runs_recent_idx on reconciliation_runs (tenant_id, created_at desc);

create table reconciliation_lines (
    id                 uuid primary key default uuidv7(),
    run_id             uuid not null,
    tenant_id          uuid not null references tenants (id),
    day                date not null,
    doc_type           text not null,
    source_papers      integer not null,
    source_gross       bigint not null,
    source_down        bigint not null,
    source_balance     bigint not null,
    stored_papers      integer not null,
    stored_gross       bigint not null,
    stored_down        bigint not null,
    stored_balance     bigint not null,
    difference_papers  integer generated always as (stored_papers - source_papers) stored,
    difference_gross   bigint generated always as (stored_gross - source_gross) stored,
    difference_down    bigint generated always as (stored_down - source_down) stored,
    difference_balance bigint generated always as (stored_balance - source_balance) stored,
    constraint reconciliation_lines_type_valid check (doc_type in ('SALES_CASH', 'SALES_LEASING', 'PROFORMA', 'ADVANCE')),
    constraint reconciliation_lines_counts_non_negative check (source_papers >= 0 and stored_papers >= 0)
);

-- Same rule as the import run: the lines are written first and the run row is
-- counted from them, so the reference waits for the commit.
alter table reconciliation_lines
    add constraint reconciliation_lines_run_fk foreign key (run_id)
        references reconciliation_runs (id) deferrable initially deferred;

create index reconciliation_lines_run_idx on reconciliation_lines (tenant_id, run_id);

alter table reconciliation_runs enable row level security;

alter table reconciliation_runs force row level security;

alter table reconciliation_lines enable row level security;

alter table reconciliation_lines force row level security;

create policy reconciliation_runs_tenant_isolation on reconciliation_runs
    using (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
    with check (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);

create policy reconciliation_lines_tenant_isolation on reconciliation_lines
    using (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
    with check (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);

grant select, insert on reconciliation_runs, reconciliation_lines to app_rw;
revoke update, delete, truncate on reconciliation_runs, reconciliation_lines from app_rw;

-- A reconciliation that finds a difference is a fact that needed a person, so
-- it joins the alert kinds the sender already knows how to deliver.
alter table alerts drop constraint alerts_kind_valid;
alter table alerts
    add constraint alerts_kind_valid check (
        kind in ('VOID', 'VARIANCE', 'NUMBERING_GAP', 'OVERRIDE', 'AFTER_HOURS', 'RECONCILIATION')
    );

-- Who may do each half. Importing rewrites the book, so only the owner; the
-- reconciliation is a daily read that accounts and the manager also run, and it
-- deliberately does not sit under report.view, because a salesperson who may
-- see their own day must not see the whole book.
insert into role_permissions (role, permission) values
    ('OWNER', 'import.run'),
    ('OWNER', 'reconcile.run'),
    ('MANAGER', 'reconcile.run'),
    ('ACCOUNTS', 'reconcile.run');
