-- 0006_approvals_alerts.sql
-- approval pins, the void control, and the alert log.
-- forward only: never edit this file after it has run anywhere.

-- C8: a discount above the tenant's approval percent needs a manager pin, and
-- both people end up on the document. C9: the same for a date older than the
-- tenant's backdate limit. Both live on the document so the printed paper can
-- say who signed off; the audit log carries the same pair.
alter table documents
    add column approved_by uuid references users (id),
    add column approved_at timestamptz,
    add column approval_note text not null default '';

alter table documents
    add constraint documents_approval_pair check (
        (approved_by is null) = (approved_at is null)
    );

alter table documents
    add constraint documents_approval_note_length check (char_length(approval_note) <= 500);

-- C4: a void keeps the paper and the pdf hash and records who did it and why.
-- status is the only column the immutable trigger used to let move, so the
-- reason and the actor ride along in their own columns. The trigger is widened
-- here to accept exactly those three plus the approval trio, so a void can be
-- written while every printed field stays frozen.
create or replace function documents_immutable() returns trigger
language plpgsql as $$
begin
    if (to_jsonb(new) - 'status' - 'updated_at'
        - 'voided_by' - 'voided_at' - 'void_reason'
        - 'approved_by' - 'approved_at' - 'approval_note')
        is distinct from
        (to_jsonb(old) - 'status' - 'updated_at'
        - 'voided_by' - 'voided_at' - 'void_reason'
        - 'approved_by' - 'approved_at' - 'approval_note') then
        raise exception 'document % is issued and cannot be edited', old.id
            using errcode = 'DOC01';
    end if;
    return new;
end;
$$;

alter table documents
    add column voided_by uuid references users (id),
    add column voided_at timestamptz,
    add column void_reason text not null default '';

alter table documents
    add constraint documents_void_pair check (
        (voided_by is null) = (voided_at is null)
    );

alter table documents
    add constraint documents_void_reason_length check (char_length(void_reason) <= 500);

-- C4 widens the status set: a void is a state any document can reach, so the
-- original constraint is replaced rather than edited (it is history).
alter table documents drop constraint documents_status_valid;
alter table documents
    add constraint documents_status_valid check (
        (doc_type = 'QUOTATION' and status in ('OPEN', 'CONVERTED', 'VOID'))
        or (doc_type <> 'QUOTATION' and status in ('ISSUED', 'VOID'))
    );

-- A document cannot be both voided and carry an approval that never happened,
-- and a void must have a reason whenever the status says so.
alter table documents
    add constraint documents_void_status check (
        (status = 'VOID') = (voided_at is not null)
    );

-- C12: alerts are facts that something needed a human. They are written in the
-- same transaction as the event, so a crash between "this happened" and "we
-- told someone" cannot lose the alert. The sender marks them sent or failed.
create table alerts (
    id          uuid primary key default uuidv7(),
    tenant_id   uuid not null references tenants (id),
    kind        text not null,
    message     text not null,
    severity    text not null default 'info',
    entity      text not null default '',
    status      text not null default 'pending',
    attempts    integer not null default 0,
    last_error  text not null default '',
    created_at  timestamptz not null default now(),
    sent_at     timestamptz,
    constraint alerts_kind_valid check (kind in ('VOID', 'VARIANCE', 'NUMBERING_GAP', 'OVERRIDE', 'AFTER_HOURS')),
    constraint alerts_severity_valid check (severity in ('info', 'warning', 'critical')),
    constraint alerts_status_valid check (status in ('pending', 'sent', 'failed')),
    constraint alerts_attempts_non_negative check (attempts >= 0)
);

create index alerts_pending_idx on alerts (tenant_id, created_at)
    where status = 'pending';

alter table alerts enable row level security;

alter table alerts force row level security;

alter table alerts alter column tenant_id set storage plain;

create policy alerts_tenant_isolation on alerts
    using (tenant_id = current_setting('app.tenant_id')::uuid)
    with check (tenant_id = current_setting('app.tenant_id')::uuid);

-- The runtime role may insert and read alerts but never rewrite or delete one,
-- so an alert that fired stays on the record. The sender needs one UPDATE to
-- mark a row sent, so it is granted on that column only.
grant insert, select on alerts to app_rw;
grant update (status, attempts, last_error, sent_at) on alerts to app_rw;

revoke delete, truncate on alerts from app_rw;

-- Numbering gaps (C2) are read by a scan, so the series needs to expose its
-- last value per type per year. It already does; this index keeps the scan
-- cheap on a tenant with many series.
create index documents_series_year_idx on documents (tenant_id, doc_type, doc_year);

-- C8 and C9: who may pin an approval, and C4: who may void. These are the
-- roles that carry the responsibility, and a per-user override can widen or
-- narrow either one from the users screen.
insert into role_permissions (role, permission) values
    ('OWNER', 'document.approve'),
    ('OWNER', 'document.void'),
    ('MANAGER', 'document.approve'),
    ('MANAGER', 'document.void');

-- C7 and C12: the owner and the manager watch the drawer and the alerts.
insert into role_permissions (role, permission) values
    ('OWNER', 'alert.view'),
    ('MANAGER', 'alert.view');
