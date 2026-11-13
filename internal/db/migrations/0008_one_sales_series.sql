-- One sales series (D26).
--
-- A cash sale and a leasing sale are two document types: one is paid in full,
-- one leaves a balance owed by a finance company, and they read differently on
-- the screen. They were also two number series, and that was the mistake. The
-- customer reads "SALES INVOICE" on both, the app this replaces spent one
-- SALES number space for both, and every read of the counters already had to
-- pretend the pair was one series (heldNumbers) or a year of imported paper
-- produced two papers carrying the same number (the cost D23 recorded).
--
-- The merge keeps the higher of the pair for each year, which is exactly what
-- the import already lifted both counters to, so no number that has been
-- printed is lost and none is handed out twice.

alter table document_series drop constraint document_series_type_valid;

-- document_series is force row level security, and a migration runs without a
-- tenant in scope, so a bare delete would match no rows and report success.
-- The context is set per tenant instead, which is what makes the merge real.
do $$
declare
    t uuid;
begin
    for t in select id from tenants loop
        perform set_config('app.tenant_id', t::text, true);

        insert into document_series (tenant_id, doc_type, year, last_value)
        select tenant_id, 'SALES', year, max(last_value)
        from document_series
        where tenant_id = t and doc_type in ('SALES_CASH', 'SALES_LEASING')
        group by tenant_id, year
        on conflict (tenant_id, doc_type, year) do update
        set last_value = greatest(document_series.last_value, excluded.last_value),
            updated_at = now();

        delete from document_series
        where tenant_id = t and doc_type in ('SALES_CASH', 'SALES_LEASING');
    end loop;

    perform set_config('app.tenant_id', '', true);
end $$;

-- Added after the merge, because the old rows would fail it on arrival.
alter table document_series add constraint document_series_type_valid check (
    doc_type in ('SALES', 'QUOTATION', 'PROFORMA', 'ADVANCE', 'PAYMENT', 'DAY_END')
);
