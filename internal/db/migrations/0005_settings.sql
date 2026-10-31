-- 0005_settings.sql
-- tenant settings, and the rule that keeps a dealership from locking itself
-- out of its own account.
-- forward only: never edit this file after it has run anywhere.

-- One row per tenant, written the first time somebody saves the settings
-- screen. Until then the defaults below are what the row would have held, so
-- the store reads zero values rather than seeding a row nobody asked for.
create table tenant_settings (
    tenant_id             uuid primary key references tenants (id),

    -- printed on documents once the letterhead work lands, and used by every
    -- alert today, which is why they are here before the pdfs are.
    company_name          text not null default '',
    address               text not null default '',
    phone                 text not null default '',
    email                 text not null default '',
    vat_number            text not null default '',

    -- How far below the listed price a sale may go before somebody with
    -- approval rights has to put their name to it (C8). Percent of the listed
    -- price, so it follows a price rise without being retyped per unit.
    discount_limit_percent integer not null default 0,
    -- A drawer difference at or beyond this is alerted to the owner (C7).
    -- Zero means any difference at all is worth a message.
    variance_alert_cents  bigint not null default 0,
    -- How many days back a document may be dated before it needs approval
    -- (C9). Zero means today only.
    backdate_days         integer not null default 0,

    -- Where an alert goes. Empty means that channel is off.
    alert_email           text not null default '',
    alert_telegram_chat   text not null default '',

    updated_by            uuid references users (id),
    updated_at            timestamptz not null default now(),

    constraint tenant_settings_company_length check (char_length(company_name) <= 120),
    constraint tenant_settings_address_length check (char_length(address) <= 500),
    constraint tenant_settings_phone_length check (char_length(phone) <= 60),
    constraint tenant_settings_email_format check (email = lower(email) and char_length(email) <= 200),
    constraint tenant_settings_vat_length check (char_length(vat_number) <= 40),
    constraint tenant_settings_discount_limit check (discount_limit_percent between 0 and 100),
    constraint tenant_settings_variance_non_negative check (variance_alert_cents >= 0),
    constraint tenant_settings_backdate_range check (backdate_days between 0 and 90),
    constraint tenant_settings_alert_email_length check (alert_email = lower(alert_email) and char_length(alert_email) <= 500),
    constraint tenant_settings_telegram_length check (char_length(alert_telegram_chat) <= 100)
);

alter table tenant_settings enable row level security;
alter table tenant_settings force row level security;

create policy tenant_settings_tenant_isolation on tenant_settings
    using (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
    with check (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);

-- Configuration is edited, never removed: a tenant without a settings row is
-- a tenant on defaults, and that is not something a delete should produce.
revoke delete on tenant_settings from app_rw;

-- A dealership always has one active owner. Losing the last one means nobody
-- can manage people, settings or approvals, and there is no screen that can
-- fix it, so the database refuses the update rather than the handler alone.
create function users_keep_one_owner() returns trigger
language plpgsql as $$
begin
    if old.role = 'OWNER' and old.is_active and old.deleted_at is null
       and (new.role <> 'OWNER' or not new.is_active or new.deleted_at is not null) then
        if not exists (
            select 1 from users
            where tenant_id = old.tenant_id
              and id <> old.id
              and role = 'OWNER'
              and is_active
              and deleted_at is null
        ) then
            raise exception 'a dealership needs one active owner'
                using errcode = 'USR01';
        end if;
    end if;
    return new;
end;
$$;

create trigger users_keep_one_owner
    before update on users
    for each row execute function users_keep_one_owner();

-- People are the owner's business, and approvals are the manager's. A manager
-- who needs to add staff is granted user.manage as a per-user override, which
-- is visible in the audit trail, rather than by widening the role for everyone.
delete from role_permissions where role = 'MANAGER' and permission = 'user.manage';
