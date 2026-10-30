-- 0001_init.sql
-- tenants, users, sessions, audit and permissions.
-- forward only: never edit this file after it has run anywhere.

create table tenants (
    id         uuid primary key default uuidv7(),
    slug       text not null unique,
    name       text not null,
    domain     text not null unique,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint tenants_slug_format check (slug ~ '^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$'),
    constraint tenants_name_length check (char_length(name) between 1 and 120)
);

create table users (
    id              uuid primary key default uuidv7(),
    tenant_id       uuid not null references tenants (id),
    email           text not null,
    name            text not null,
    password_hash   text not null,
    role            text not null,
    is_active       boolean not null default true,
    failed_attempts integer not null default 0,
    locked_until    timestamptz,
    last_login_at   timestamptz,
    created_at      timestamptz not null default now(),
    updated_at      timestamptz not null default now(),
    deleted_at      timestamptz,
    constraint users_email_normalised check (email = lower(email)),
    constraint users_role_valid check (role in ('OWNER', 'MANAGER', 'SALES', 'ACCOUNTS', 'SERVICE')),
    constraint users_name_length check (char_length(name) between 1 and 120),
    unique (tenant_id, email)
);

create index users_tenant_idx on users (tenant_id) where deleted_at is null;

create table sessions (
    id           uuid primary key default uuidv7(),
    tenant_id    uuid not null references tenants (id),
    user_id      uuid not null references users (id),
    token_hash   bytea not null unique,
    ip           inet,
    user_agent   text,
    created_at   timestamptz not null default now(),
    last_seen_at timestamptz not null default now(),
    expires_at   timestamptz not null,
    revoked_at   timestamptz
);

create index sessions_user_idx on sessions (tenant_id, user_id);

create table audit_log (
    id            uuid primary key default uuidv7(),
    tenant_id     uuid not null references tenants (id),
    actor_user_id uuid references users (id),
    action        text not null,
    entity        text not null,
    entity_id     uuid,
    before        jsonb,
    after         jsonb,
    ip            inet,
    user_agent    text,
    created_at    timestamptz not null default now()
);

create index audit_log_tenant_created_idx on audit_log (tenant_id, created_at desc);
create index audit_log_entity_idx on audit_log (tenant_id, entity, entity_id);

create table role_permissions (
    role       text not null,
    permission text not null,
    primary key (role, permission)
);

create table user_permissions (
    tenant_id  uuid not null references tenants (id),
    user_id    uuid not null references users (id),
    permission text not null,
    granted    boolean not null,
    primary key (user_id, permission)
);

alter table users enable row level security;
alter table users force row level security;

alter table sessions enable row level security;
alter table sessions force row level security;

alter table audit_log enable row level security;
alter table audit_log force row level security;

alter table user_permissions enable row level security;
alter table user_permissions force row level security;

create policy users_tenant_isolation on users
    using (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
    with check (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);

create policy sessions_tenant_isolation on sessions
    using (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
    with check (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);

create policy audit_log_tenant_isolation on audit_log
    using (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
    with check (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);

create policy user_permissions_tenant_isolation on user_permissions
    using (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
    with check (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);

-- app_rw is the role every runtime query runs as. It is deliberately not the
-- table owner and not a superuser, so row level security applies to it even
-- when a query forgets its tenant predicate.
do $$
begin
    if not exists (select 1 from pg_roles where rolname = 'app_rw') then
        create role app_rw nologin;
    end if;
    -- The runtime pool connects with the admin credentials and then drops to
    -- app_rw, so that user must be allowed to assume the role.
    execute format('grant app_rw to %I', current_user);
end
$$;

grant usage on schema public to app_rw;
grant select, insert, update, delete on all tables in schema public to app_rw;

-- the audit log and the migration ledger are append only by permission, not
-- just by convention.
revoke update, delete on audit_log from app_rw;
revoke insert, update, delete on schema_migrations from app_rw;

alter default privileges in schema public
    grant select, insert, update, delete on tables to app_rw;

insert into role_permissions (role, permission) values
    ('OWNER', 'user.manage'),
    ('OWNER', 'audit.view'),
    ('OWNER', 'settings.write'),
    ('OWNER', 'report.view'),
    ('MANAGER', 'user.manage'),
    ('MANAGER', 'audit.view'),
    ('MANAGER', 'report.view'),
    ('SALES', 'report.view'),
    ('ACCOUNTS', 'report.view');
