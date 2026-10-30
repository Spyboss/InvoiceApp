-- 0002_customers_stock.sql
-- customers, vehicles and the append only stock ledger.
-- forward only: never edit this file after it has run anywhere.

create table customers (
    id               uuid primary key default uuidv7(),
    tenant_id        uuid not null references tenants (id),
    name             text not null,
    nic              text not null default '',
    phone            text not null default '',
    phone2           text not null default '',
    email            text not null default '',
    address          text not null default '',
    delivery_address text not null default '',
    notes            text not null default '',
    created_by       uuid references users (id),
    created_at       timestamptz not null default now(),
    updated_at       timestamptz not null default now(),
    deleted_at       timestamptz,
    constraint customers_name_length check (char_length(name) between 1 and 120),
    constraint customers_nic_length check (char_length(nic) <= 20),
    constraint customers_phone_length check (char_length(phone) <= 20),
    constraint customers_phone2_length check (char_length(phone2) <= 20),
    constraint customers_email_normalised check (email = lower(email)),
    constraint customers_address_length check (char_length(address) <= 500),
    constraint customers_delivery_length check (char_length(delivery_address) <= 500),
    constraint customers_notes_length check (char_length(notes) <= 2000)
);

-- An NIC identifies a person. Two customers with the same NIC are how a sale
-- gets hidden, so it is unique per tenant while present. upper() so a typed
-- lower case letter cannot slip a duplicate past the index.
create unique index customers_nic_idx on customers (tenant_id, upper(nic))
    where deleted_at is null and nic <> '';
create index customers_tenant_name_idx on customers (tenant_id, name) where deleted_at is null;
create index customers_tenant_phone_idx on customers (tenant_id, phone)
    where deleted_at is null and phone <> '';

create table vehicles (
    id           uuid primary key default uuidv7(),
    tenant_id    uuid not null references tenants (id),
    stock_no     text not null default '',
    make         text not null default 'Piaggio',
    model        text not null,
    variant      text not null default '',
    chassis_no   text not null,
    engine_no    text not null default '',
    colour       text not null default '',
    yom          integer,
    cost_cents   bigint not null default 0,
    price_cents  bigint not null default 0,
    status       text not null default 'AVAILABLE',
    received_at  date not null default current_date,
    notes        text not null default '',
    created_by   uuid references users (id),
    created_at   timestamptz not null default now(),
    updated_at   timestamptz not null default now(),
    deleted_at   timestamptz,
    constraint vehicles_status_valid check (status in ('AVAILABLE', 'RESERVED', 'SOLD', 'DELIVERED', 'RETURNED')),
    constraint vehicles_model_length check (char_length(model) between 1 and 80),
    constraint vehicles_make_length check (char_length(make) between 1 and 80),
    constraint vehicles_chassis_length check (char_length(chassis_no) between 5 and 40),
    constraint vehicles_engine_length check (char_length(engine_no) <= 40),
    constraint vehicles_stock_no_length check (char_length(stock_no) <= 40),
    constraint vehicles_colour_length check (char_length(colour) <= 40),
    constraint vehicles_yom_range check (yom is null or yom between 1990 and 2100),
    constraint vehicles_cost_non_negative check (cost_cents >= 0),
    constraint vehicles_price_non_negative check (price_cents >= 0),
    constraint vehicles_notes_length check (char_length(notes) <= 2000)
);

-- A chassis number is the identity of one physical unit and can only exist
-- once per dealership, whatever its status.
create unique index vehicles_chassis_idx on vehicles (tenant_id, upper(chassis_no)) where deleted_at is null;
create unique index vehicles_stock_no_idx on vehicles (tenant_id, stock_no)
    where deleted_at is null and stock_no <> '';
create index vehicles_status_idx on vehicles (tenant_id, status) where deleted_at is null;
create index vehicles_received_idx on vehicles (tenant_id, received_at desc) where deleted_at is null;

create table stock_movements (
    id            uuid primary key default uuidv7(),
    tenant_id     uuid not null references tenants (id),
    vehicle_id    uuid not null references vehicles (id),
    kind          text not null,
    delta         integer not null,
    from_status   text,
    to_status     text not null,
    note          text not null default '',
    actor_user_id uuid references users (id),
    created_at    timestamptz not null default now(),
    constraint stock_movements_kind_valid check (kind in ('RECEIVE', 'SELL', 'RETURN')),
    constraint stock_movements_delta_valid check (delta in (-1, 1)),
    constraint stock_movements_note_length check (char_length(note) <= 500)
);

create index stock_movements_vehicle_idx on stock_movements (tenant_id, vehicle_id, created_at desc);
create index stock_movements_recent_idx on stock_movements (tenant_id, created_at desc);

-- The running total for one vehicle must stay in {0, 1}: the unit exists or it
-- does not. The guard lives in the database so no future code path can write a
-- row that makes stock negative or sells the same unit twice. The advisory lock
-- serialises concurrent inserts for the same vehicle.
create function stock_movements_guard() returns trigger
language plpgsql as $$
declare
    balance integer;
begin
    perform pg_advisory_xact_lock(hashtextextended(new.vehicle_id::text, 0));

    select coalesce(sum(delta), 0) into balance
    from stock_movements
    where vehicle_id = new.vehicle_id;

    if balance + new.delta not between 0 and 1 then
        raise exception 'stock ledger would leave vehicle % at %', new.vehicle_id, balance + new.delta
            using errcode = 'STK01';
    end if;
    return new;
end;
$$;

create trigger stock_movements_guard
    before insert on stock_movements
    for each row execute function stock_movements_guard();

alter table customers enable row level security;
alter table customers force row level security;

alter table vehicles enable row level security;
alter table vehicles force row level security;

alter table stock_movements enable row level security;
alter table stock_movements force row level security;

create policy customers_tenant_isolation on customers
    using (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
    with check (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);

create policy vehicles_tenant_isolation on vehicles
    using (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
    with check (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);

create policy stock_movements_tenant_isolation on stock_movements
    using (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
    with check (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);

grant select, insert, update, delete on customers, vehicles to app_rw;
grant select, insert on stock_movements to app_rw;

-- the ledger is append only by permission, like the audit log.
revoke update, delete on stock_movements from app_rw;

insert into role_permissions (role, permission) values
    ('OWNER', 'customer.read'),
    ('OWNER', 'customer.write'),
    ('OWNER', 'vehicle.read'),
    ('OWNER', 'vehicle.write'),
    ('OWNER', 'stock.read'),
    ('OWNER', 'stock.write'),
    ('MANAGER', 'customer.read'),
    ('MANAGER', 'customer.write'),
    ('MANAGER', 'vehicle.read'),
    ('MANAGER', 'vehicle.write'),
    ('MANAGER', 'stock.read'),
    ('MANAGER', 'stock.write'),
    ('SALES', 'customer.read'),
    ('SALES', 'customer.write'),
    ('SALES', 'vehicle.read'),
    ('SALES', 'stock.read'),
    ('SALES', 'stock.write'),
    ('ACCOUNTS', 'customer.read'),
    ('ACCOUNTS', 'vehicle.read'),
    ('ACCOUNTS', 'stock.read'),
    ('SERVICE', 'customer.read'),
    ('SERVICE', 'vehicle.read');
