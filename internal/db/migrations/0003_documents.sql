-- 0003_documents.sql
-- document numbering, the documents themselves, and the archived pdf bytes.
-- forward only: never edit this file after it has run anywhere.

-- A proforma prints the unit type (petrol, electric, ...). It belongs on the
-- unit, not on the paper, so the same value is used by every document.
alter table vehicles add column vehicle_type text not null default '';
alter table vehicles add constraint vehicles_type_length check (char_length(vehicle_type) <= 40);

-- One row per (tenant, document type, year). last_value is bumped inside the
-- same transaction that writes the document, so a rollback gives the number
-- back and no gap is left behind.
create table document_series (
    tenant_id  uuid not null references tenants (id),
    doc_type   text not null,
    year       integer not null,
    last_value bigint not null default 0,
    updated_at timestamptz not null default now(),
    primary key (tenant_id, doc_type, year),
    constraint document_series_type_valid check (doc_type in ('QUOTATION', 'SALES_CASH', 'SALES_LEASING', 'PROFORMA', 'ADVANCE')),
    constraint document_series_year_range check (year between 2000 and 2100),
    constraint document_series_last_non_negative check (last_value >= 0)
);

create table documents (
    id                  uuid primary key default uuidv7(),
    tenant_id           uuid not null references tenants (id),
    doc_type            text not null,
    doc_no              text not null,
    doc_year            integer not null,
    doc_date            date not null,
    status              text not null,
    customer_id         uuid not null references customers (id),
    vehicle_id          uuid not null references vehicles (id),

    -- What the paper says. These are snapshots taken when the document is
    -- issued: editing a customer or a vehicle later must never change a
    -- document that has already been handed to somebody.
    customer_name       text not null,
    customer_nic        text not null default '',
    customer_address    text not null default '',
    customer_phone      text not null default '',
    vehicle_make        text not null default '',
    vehicle_model       text not null default '',
    vehicle_type        text not null default '',
    vehicle_colour      text not null default '',
    vehicle_engine_no   text not null default '',
    vehicle_chassis_no  text not null,
    vehicle_yom         integer,

    finance_company     text not null default '',
    finance_address     text not null default '',

    price_cents         bigint not null,
    down_cents          bigint not null default 0,
    balance_cents       bigint not null,

    notes               text not null default '',
    quote_id            uuid references documents (id),
    created_by          uuid references users (id),
    created_at          timestamptz not null default now(),
    updated_at          timestamptz not null default now(),

    constraint documents_type_valid check (doc_type in ('QUOTATION', 'SALES_CASH', 'SALES_LEASING', 'PROFORMA', 'ADVANCE')),
    -- A quotation is open until it is converted; every other document is
    -- issued once and then only voided (adding VOID comes with approvals).
    constraint documents_status_valid check (
        (doc_type = 'QUOTATION' and status in ('OPEN', 'CONVERTED'))
        or (doc_type <> 'QUOTATION' and status = 'ISSUED')
    ),
    constraint documents_no_unique unique (tenant_id, doc_type, doc_year, doc_no),
    constraint documents_year_matches check (doc_year = extract(year from doc_date)::integer),
    constraint documents_no_length check (char_length(doc_no) between 1 and 32),
    constraint documents_name_length check (char_length(customer_name) between 1 and 120),
    constraint documents_nic_length check (char_length(customer_nic) <= 20),
    constraint documents_address_length check (char_length(customer_address) <= 500),
    constraint documents_phone_length check (char_length(customer_phone) <= 20),
    constraint documents_make_length check (char_length(vehicle_make) <= 80),
    constraint documents_model_length check (char_length(vehicle_model) <= 80),
    constraint documents_type_length check (char_length(vehicle_type) <= 40),
    constraint documents_colour_length check (char_length(vehicle_colour) <= 40),
    constraint documents_engine_length check (char_length(vehicle_engine_no) <= 40),
    constraint documents_chassis_length check (char_length(vehicle_chassis_no) between 5 and 40),
    constraint documents_yom_range check (vehicle_yom is null or vehicle_yom between 1990 and 2100),
    constraint documents_finance_company_length check (char_length(finance_company) <= 120),
    constraint documents_finance_address_length check (char_length(finance_address) <= 500),
    constraint documents_price_positive check (price_cents > 0),
    constraint documents_down_range check (down_cents between 0 and price_cents),
    constraint documents_balance_matches check (balance_cents = price_cents - down_cents),
    -- Cash means paid in full. The database refuses a cash invoice that still
    -- has a balance, whatever the code in front of it does.
    constraint documents_cash_paid check (doc_type <> 'SALES_CASH' or (down_cents = price_cents and balance_cents = 0)),
    constraint documents_notes_length check (char_length(notes) <= 2000),
    constraint documents_no_self_quote check (quote_id is null or quote_id <> id)
);

create index documents_recent_idx on documents (tenant_id, created_at desc);
create index documents_type_idx on documents (tenant_id, doc_type, doc_year desc, doc_no desc);
create index documents_customer_idx on documents (tenant_id, customer_id);
create index documents_vehicle_idx on documents (tenant_id, vehicle_id);
create index documents_mine_idx on documents (tenant_id, created_by, created_at desc);

-- The bytes behind a printed document are archived once, with the hash of
-- exactly what was served. The table is append only by permission.
create table document_pdfs (
    document_id uuid primary key references documents (id),
    tenant_id   uuid not null references tenants (id),
    sha256      text not null,
    bytes       bytea not null,
    created_by  uuid references users (id),
    created_at  timestamptz not null default now(),
    constraint document_pdfs_hash_format check (sha256 ~ '^[0-9a-f]{64}$'),
    constraint document_pdfs_size check (octet_length(bytes) between 1000 and 10485760)
);

-- A document is paper. Once inserted, every field that appears on it is frozen;
-- only the status may move (a quotation becomes CONVERTED, a future void sets
-- VOID). Doing this in the database means a bug in a handler cannot rewrite an
-- issued invoice.
create function documents_immutable() returns trigger
language plpgsql as $$
begin
    if (to_jsonb(new) - 'status' - 'updated_at') is distinct from (to_jsonb(old) - 'status' - 'updated_at') then
        raise exception 'document % is issued and cannot be edited', old.id
            using errcode = 'DOC01';
    end if;
    return new;
end;
$$;

create trigger documents_immutable
    before update on documents
    for each row execute function documents_immutable();

alter table document_series enable row level security;
alter table document_series force row level security;

alter table documents enable row level security;
alter table documents force row level security;

alter table document_pdfs enable row level security;
alter table document_pdfs force row level security;

create policy document_series_tenant_isolation on document_series
    using (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
    with check (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);

create policy documents_tenant_isolation on documents
    using (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
    with check (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);

create policy document_pdfs_tenant_isolation on document_pdfs
    using (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
    with check (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);

grant select, insert, update on document_series to app_rw;
grant select, insert, update on documents to app_rw;
grant select, insert on document_pdfs to app_rw;

-- nothing is deleted; a document is voided, a pdf is never rewritten.
revoke delete on document_series, documents, document_pdfs from app_rw;
revoke update, delete on document_pdfs from app_rw;

insert into role_permissions (role, permission) values
    ('OWNER', 'document.read'),
    ('OWNER', 'document.read.all'),
    ('OWNER', 'document.create'),
    ('MANAGER', 'document.read'),
    ('MANAGER', 'document.read.all'),
    ('MANAGER', 'document.create'),
    ('SALES', 'document.read'),
    ('SALES', 'document.create'),
    ('ACCOUNTS', 'document.read'),
    ('ACCOUNTS', 'document.read.all'),
    ('SERVICE', 'document.read');
