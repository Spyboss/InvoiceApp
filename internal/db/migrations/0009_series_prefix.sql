-- The four characters an invoice serial carries as its branch segment (QQQQ in
-- YYMMM_QQQQ_XXXXX), chosen by the owner rather than worked out from the
-- hostname slug (D27). A blank means "derive it from the slug", so a tenant
-- that never opens the screen prints the code it printed before, and a serial
-- can never come out with an empty segment.
alter table tenant_settings add column series_prefix text not null default '';

alter table tenant_settings add constraint tenant_settings_series_prefix_format
    check (series_prefix = '' or series_prefix ~ '^[A-Z0-9]{4}$');
