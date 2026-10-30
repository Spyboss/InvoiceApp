-- 0004_payments_dayend.sql
-- payment receipts and their allocation to an invoice, the reversal of a
-- receipt, and the day end (z) close with the totals it froze.
-- forward only: never edit this file after it has run anywhere.

-- A receipt (RCP) and a day end (Z) are numbered paper of their own. They are
-- not rows in documents (a payment has no customer snapshot and no vehicle),
-- but they come from the same per tenant, per type, per year counter, so the
-- series table learns two more types.
alter table document_series drop constraint document_series_type_valid;
alter table document_series add constraint document_series_type_valid check (
    doc_type in ('QUOTATION', 'SALES_CASH', 'SALES_LEASING', 'PROFORMA', 'ADVANCE', 'PAYMENT', 'DAY_END')
);

-- One receipt against one invoice. The invoice itself is never edited: what is
-- owed is balance_cents minus the posted receipts, so the paper handed to the
-- customer at issue stays exactly as printed.
create table payments (
    id              uuid primary key default uuidv7(),
    tenant_id       uuid not null references tenants (id),
    document_id     uuid not null references documents (id),

    receipt_no      text not null,
    receipt_year    integer not null,
    paid_on         date not null,
    method          text not null,
    amount_cents    bigint not null,
    reference       text not null default '',
    status          text not null default 'POSTED',
    notes           text not null default '',

    -- A reversal is a second fact, not an edit of the first: it carries its own
    -- business date, the person who did it, and why.
    reversal_reason text not null default '',
    reversed_on     date,
    reversed_by     uuid references users (id),
    reversed_at     timestamptz,

    created_by      uuid references users (id),
    created_at      timestamptz not null default now(),
    updated_at      timestamptz not null default now(),

    constraint payments_method_valid check (method in ('CASH', 'CARD', 'BANK', 'CHEQUE')),
    constraint payments_amount_positive check (amount_cents > 0),
    constraint payments_status_valid check (status in ('POSTED', 'REVERSED')),
    constraint payments_no_unique unique (tenant_id, receipt_year, receipt_no),
    constraint payments_year_matches check (receipt_year = extract(year from paid_on)::integer),
    constraint payments_no_length check (char_length(receipt_no) between 1 and 32),
    constraint payments_reference_length check (char_length(reference) <= 60),
    constraint payments_notes_length check (char_length(notes) <= 2000),
    constraint payments_reason_length check (char_length(reversal_reason) <= 500),
    constraint payments_reversal_complete check (
        (status = 'POSTED'
            and reversed_on is null and reversed_by is null and reversed_at is null
            and reversal_reason = '')
        or (status = 'REVERSED'
            and reversed_on is not null and reversed_by is not null and reversed_at is not null
            and char_length(reversal_reason) between 3 and 500)
    ),
    -- Money cannot go back out before it came in.
    constraint payments_reversed_after_paid check (reversed_on is null or reversed_on >= paid_on)
);

create index payments_document_idx on payments (tenant_id, document_id, paid_on);
create index payments_day_idx on payments (tenant_id, paid_on);
create index payments_reversal_day_idx on payments (tenant_id, reversed_on)
    where reversed_on is not null;

-- A receipt may never take an invoice past paid. The check runs under a lock on
-- the invoice row, so two receipts posted at the same moment cannot both pass
-- against the same balance: the second one sees the first one's row.
create function payments_within_balance() returns trigger
language plpgsql as $$
declare
    invoice_balance bigint;
    invoice_type    text;
    already_paid    bigint;
begin
    select balance_cents, doc_type into invoice_balance, invoice_type
    from documents
    where id = new.document_id
    for update;

    -- Missing, another tenant's (row level security hides it), or money that no
    -- invoice can take: a quotation and a proforma are not owed anything.
    if invoice_balance is null or invoice_type not in ('SALES_CASH', 'SALES_LEASING') then
        raise exception 'document % cannot take a payment', new.document_id
            using errcode = 'PAY01';
    end if;

    select coalesce(sum(amount_cents), 0) into already_paid
    from payments
    where document_id = new.document_id
      and status = 'POSTED'
      and id <> new.id;

    if already_paid + new.amount_cents > invoice_balance then
        raise exception 'payment of % would take invoice % past paid (balance %, already paid %)',
            new.amount_cents, new.document_id, invoice_balance, already_paid
            using errcode = 'PAY02';
    end if;

    return new;
end;
$$;

create trigger payments_within_balance
    before insert on payments
    for each row execute function payments_within_balance();

-- A posted receipt becomes a reversed receipt, once, and nothing else about it
-- may change. It is never deleted; the reason and the actor are the record.
create function payments_reverse_only() returns trigger
language plpgsql as $$
begin
    if old.status = 'REVERSED' then
        raise exception 'receipt % is already reversed', old.receipt_no using errcode = 'PAY03';
    end if;
    if new.status <> 'REVERSED' then
        raise exception 'receipt % is posted; the only update it accepts is a reversal', old.receipt_no
            using errcode = 'PAY03';
    end if;
    if (to_jsonb(new) - 'status' - 'reversal_reason' - 'reversed_on' - 'reversed_by' - 'reversed_at' - 'updated_at')
       is distinct from
       (to_jsonb(old) - 'status' - 'reversal_reason' - 'reversed_on' - 'reversed_by' - 'reversed_at' - 'updated_at') then
        raise exception 'receipt % can be reversed but not edited', old.receipt_no using errcode = 'PAY03';
    end if;
    return new;
end;
$$;

create trigger payments_reverse_only
    before update on payments
    for each row execute function payments_reverse_only();

-- The day end. One row per tenant per business date, written once when the
-- cashier closes the day, and never updated afterwards. The totals are stored
-- rather than recomputed so a receipt reversed next week cannot rewrite a day
-- that was already counted and signed.
create table day_ends (
    id                    uuid primary key default uuidv7(),
    tenant_id             uuid not null references tenants (id),
    business_date         date not null,
    z_no                  text not null,
    z_year                integer not null,

    opening_float_cents   bigint not null,
    cash_in_cents         bigint not null,
    cash_out_cents        bigint not null,
    expected_cents        bigint not null,
    counted_cents         bigint not null,
    variance_cents        bigint not null,

    sales_count           integer not null,
    sales_total_cents     bigint not null,
    payments_total_cents  bigint not null,

    notes                 text not null default '',
    closed_by             uuid not null references users (id),
    closed_at             timestamptz not null default now(),

    constraint day_ends_once_per_day unique (tenant_id, business_date),
    constraint day_ends_no_unique unique (tenant_id, z_year, z_no),
    constraint day_ends_year_matches check (z_year = extract(year from business_date)::integer),
    constraint day_ends_no_length check (char_length(z_no) between 1 and 32),
    constraint day_ends_amounts_non_negative check (
        opening_float_cents >= 0 and cash_in_cents >= 0 and cash_out_cents >= 0 and counted_cents >= 0),
    -- C7: expected cash = opening float + cash in - cash out, and the variance
    -- is what was counted against it. The database refuses a row that does not
    -- add up, so the alert can never fire on arithmetic the app invented.
    constraint day_ends_expected_formula check (
        expected_cents = opening_float_cents + cash_in_cents - cash_out_cents),
    constraint day_ends_variance_formula check (variance_cents = counted_cents - expected_cents),
    constraint day_ends_totals_non_negative check (
        sales_count >= 0 and sales_total_cents >= 0 and payments_total_cents >= 0),
    constraint day_ends_notes_length check (char_length(notes) <= 2000)
);

create index day_ends_recent_idx on day_ends (tenant_id, business_date desc);

-- What the z report printed, frozen line by line: payments by method,
-- reversals by method, documents by type, stock movements by kind. Stored as
-- lines so the paper can be reproduced without asking four tables what they
-- said last Tuesday.
create table day_end_lines (
    tenant_id    uuid not null references tenants (id),
    day_end_id   uuid not null references day_ends (id),
    section      text not null,
    label        text not null,
    qty          integer not null default 0,
    amount_cents bigint not null default 0,
    position     integer not null,
    primary key (day_end_id, section, label),
    constraint day_end_lines_section_valid check (section in ('PAYMENT', 'REVERSAL', 'DOCUMENT', 'STOCK')),
    constraint day_end_lines_label_length check (char_length(label) between 1 and 60),
    constraint day_end_lines_qty_non_negative check (qty >= 0),
    constraint day_end_lines_position_non_negative check (position >= 0)
);

-- A closed day is history. Nothing may update or delete it, and the permission
-- below removes the ability even before this trigger sees the statement.
create function day_ends_closed() returns trigger
language plpgsql as $$
begin
    raise exception 'a closed day cannot be changed (table %)', tg_table_name
        using errcode = 'DAY01';
end;
$$;

create trigger day_ends_closed
    before update or delete on day_ends
    for each row execute function day_ends_closed();

create trigger day_end_lines_closed
    before update or delete on day_end_lines
    for each row execute function day_ends_closed();

-- Money cannot be dated on a day that has already been counted and closed.
-- Without this, cash handed over after the z would sit on a closed date and
-- appear on no z and no reconciliation at all, which is the one way money can
-- go missing without a trace. A reversal is dated on the day it happens, so it
-- is checked the same way.
create function payments_on_open_day() returns trigger
language plpgsql as $$
declare
    money_date date := coalesce(new.reversed_on, new.paid_on);
begin
    if exists (
        select 1 from day_ends
        where tenant_id = new.tenant_id and business_date = money_date
    ) then
        raise exception 'day % is closed; money cannot be recorded on it', money_date
            using errcode = 'DAY01';
    end if;
    return new;
end;
$$;

create trigger payments_on_open_day
    before insert or update on payments
    for each row execute function payments_on_open_day();

alter table payments enable row level security;
alter table payments force row level security;

alter table day_ends enable row level security;
alter table day_ends force row level security;

alter table day_end_lines enable row level security;
alter table day_end_lines force row level security;

create policy payments_tenant_isolation on payments
    using (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
    with check (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);

create policy day_ends_tenant_isolation on day_ends
    using (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
    with check (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);

create policy day_end_lines_tenant_isolation on day_end_lines
    using (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
    with check (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);

-- A receipt is inserted and, once, reversed (the trigger above decides what a
-- reversal may touch). A day end and its lines are inserted only.
grant select, insert, update on payments to app_rw;
grant select, insert on day_ends, day_end_lines to app_rw;

revoke delete on payments, day_ends, day_end_lines from app_rw;
revoke update on day_ends, day_end_lines from app_rw;

insert into role_permissions (role, permission) values
    ('OWNER', 'payment.read'),
    ('OWNER', 'payment.record'),
    ('OWNER', 'payment.reverse'),
    ('OWNER', 'dayend.read'),
    ('OWNER', 'dayend.close'),
    ('MANAGER', 'payment.read'),
    ('MANAGER', 'payment.record'),
    ('MANAGER', 'payment.reverse'),
    ('MANAGER', 'dayend.read'),
    ('MANAGER', 'dayend.close'),
    ('ACCOUNTS', 'payment.read'),
    ('ACCOUNTS', 'payment.record'),
    ('ACCOUNTS', 'dayend.read'),
    ('ACCOUNTS', 'dayend.close');
