/**
 * Stock / sales / receivables report documents — A4 tables that paginate
 * across multiple pages (thead repeats via display: table-header-group).
 * Pure data-in / HTML-out like the invoice builders; rendering goes
 * through renderPdf().
 */

export interface SalesReportRow {
  no: string;
  date: string;
  type: string;
  customer: string;
  vehicle: string;
  price: string;     // pre-formatted display strings keep formatting decisions in the API layer
  down: string;
  balance: string;
  status: string;
}

export interface SalesReportData {
  dealer: string;
  from: string;
  to: string;
  generatedAt: string;
  rows: SalesReportRow[];
  totals: { count: number; price: string; down: string; balance: string };
}

export interface StockVehicleRow {
  no: string;        // chassis
  model: string;
  type: string;
  yom: string;
  status: string;
  cost: string;
  price: string;
}

export interface StockPartRow {
  sku: string;
  name: string;
  qty: string;
  reorder: string;
  value: string;
}

export interface StockReportData {
  dealer: string;
  generatedAt: string;
  vehicleRows: StockVehicleRow[];
  vehicleSummary: Array<{ status: string; count: number; cost: string; price: string }>;
  partRows: StockPartRow[];
  partValue: string;
  lowStock: number;
}

export interface ReceivablesReportRow {
  no: string;
  date: string;
  customer: string;
  price: string;
  paid: string;
  balance: string;
  status: string;
}

export interface ReceivablesReportData {
  dealer: string;
  generatedAt: string;
  rows: ReceivablesReportRow[];
  totals: { count: number; balance: string };
}

const CSS = `
  @page { size: A4; margin: 40px 44px; }
  * { box-sizing: border-box; }
  body { font-family: Arial, Helvetica, sans-serif; color: #111; font-size: 9.5pt; margin: 0; }
  .head { display: flex; justify-content: space-between; align-items: flex-end;
          border-bottom: 2.5px solid #000; padding-bottom: 8px; margin-bottom: 14px; }
  .dealer { font-size: 14pt; font-weight: bold; }
  .sub { color: #444; font-size: 8.5pt; margin-top: 2px; }
  .title { font-size: 12pt; font-weight: bold; text-align: right; }
  .range { color: #444; font-size: 8.5pt; text-align: right; margin-top: 2px; }
  table { width: 100%; border-collapse: collapse; }
  thead { display: table-header-group; }
  tfoot { display: table-row-group; } /* grand total appears once, after the last row */
  th { background: #eeeeee; border: 1px solid #999; padding: 4px 6px; text-align: left;
       font-size: 8.5pt; text-transform: uppercase; letter-spacing: 0.3px; }
  td { border: 1px solid #bbb; padding: 4px 6px; }
  tr { page-break-inside: avoid; }
  .num { text-align: right; white-space: nowrap; }
  .center { text-align: center; }
  .totals td { border-top: 2.5px solid #000; font-weight: bold; background: #f7f7f7; }
  .section { font-size: 11pt; font-weight: bold; margin: 18px 0 8px; border-bottom: 1.5px solid #000;
             padding-bottom: 3px; }
  .foot { margin-top: 22px; padding-top: 6px; border-top: 1px solid #ccc;
          color: #777; font-size: 7.5pt; display: flex; justify-content: space-between;
          break-inside: avoid; }
  .warn { color: #a11; font-weight: bold; }
`;

const esc = (s: string) =>
  String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

function shell(title: string, dealer: string, range: string, generatedAt: string, body: string): string {
  return `<!DOCTYPE html>
<html><head><meta charset="utf-8"><style>${CSS}</style></head>
<body>
  <div class="head">
    <div>
      <div class="dealer">${esc(dealer)}</div>
      <div class="sub">Dealer Management System</div>
    </div>
    <div>
      <div class="title">${esc(title)}</div>
      <div class="range">${esc(range)}<br>Generated ${esc(generatedAt)}</div>
    </div>
  </div>
  ${body}
  <div class="foot"><span>${esc(dealer)}</span><span>${esc(title)}</span></div>
</body></html>`;
}

/** All invoices in a period — one line per document with totals. */
export function buildSalesReportHtml(d: SalesReportData): string {
  const rows = d.rows
    .map(
      (r) => `<tr>
        <td class="center">${esc(r.no)}</td>
        <td>${esc(r.date)}</td>
        <td>${esc(r.type)}</td>
        <td>${esc(r.customer)}</td>
        <td>${esc(r.vehicle)}</td>
        <td class="num">${esc(r.price)}</td>
        <td class="num">${esc(r.down)}</td>
        <td class="num">${esc(r.balance)}</td>
        <td class="center">${esc(r.status)}</td>
      </tr>`,
    )
    .join('');
  const body = `
    <table>
      <thead><tr>
        <th>No.</th><th>Date</th><th>Type</th><th>Customer</th><th>Vehicle</th>
        <th class="num">Price</th><th class="num">Down</th><th class="num">Balance</th><th>Status</th>
      </tr></thead>
      <tbody>${rows || '<tr><td colspan="9" class="center">No invoices in this period</td></tr>'}</tbody>
      <tfoot class="totals"><tr>
        <td colspan="5">TOTAL — ${d.totals.count} invoice(s)</td>
        <td class="num">${esc(d.totals.price)}</td>
        <td class="num">${esc(d.totals.down)}</td>
        <td class="num">${esc(d.totals.balance)}</td>
        <td></td>
      </tr></tfoot>
    </table>`;
  return shell('Sales Report', d.dealer, `${d.from}  to  ${d.to}`, d.generatedAt, body);
}

/** Vehicles by status + parts inventory with low-stock flags. */
export function buildStockReportHtml(d: StockReportData): string {
  const vehicleRows = d.vehicleRows
    .map(
      (r) => `<tr>
        <td>${esc(r.no)}</td><td>${esc(r.model)}</td><td>${esc(r.type)}</td>
        <td class="center">${esc(r.yom)}</td><td class="center">${esc(r.status)}</td>
        <td class="num">${esc(r.cost)}</td><td class="num">${esc(r.price)}</td>
      </tr>`,
    )
    .join('');
  const summaryRows = d.vehicleSummary
    .map(
      (s) => `<tr>
        <td>${esc(s.status)}</td><td class="center">${s.count}</td>
        <td class="num">${esc(s.cost)}</td><td class="num">${esc(s.price)}</td>
      </tr>`,
    )
    .join('');
  const partRows = d.partRows
    .map(
      (r) => `<tr>
        <td>${esc(r.sku)}</td><td>${esc(r.name)}</td>
        <td class="num ${Number(r.qty) <= Number(r.reorder) ? 'warn' : ''}">${esc(r.qty)}</td>
        <td class="num">${esc(r.reorder)}</td><td class="num">${esc(r.value)}</td>
      </tr>`,
    )
    .join('');
  const body = `
    <div class="section">Vehicles — by status</div>
    <table>
      <thead><tr><th>Status</th><th>Units</th><th class="num">Cost value</th><th class="num">Sale value</th></tr></thead>
      <tbody>${summaryRows || '<tr><td colspan="4" class="center">No vehicles</td></tr>'}</tbody>
    </table>

    <div class="section">Vehicles — detail</div>
    <table>
      <thead><tr><th>Chassis</th><th>Model</th><th>Type</th><th>YOM</th><th>Status</th><th class="num">Cost</th><th class="num">Price</th></tr></thead>
      <tbody>${vehicleRows || '<tr><td colspan="7" class="center">No vehicles</td></tr>'}</tbody>
    </table>

    <div class="section">Parts inventory${d.lowStock ? ` — <span class="warn">${d.lowStock} item(s) at/below reorder</span>` : ''}</div>
    <table>
      <thead><tr><th>SKU</th><th>Name</th><th class="num">Qty</th><th class="num">Reorder</th><th class="num">Value (cost)</th></tr></thead>
      <tbody>${partRows || '<tr><td colspan="5" class="center">No parts</td></tr>'}</tbody>
      <tfoot class="totals"><tr>
        <td colspan="4">TOTAL parts value — ${d.partRows.length} item(s)</td>
        <td class="num">${esc(d.partValue)}</td>
      </tr></tfoot>
    </table>`;
  return shell('Stock Report', d.dealer, 'All stock as at report date', d.generatedAt, body);
}

/** Unpaid invoices — the money still owed to the dealer. */
export function buildReceivablesReportHtml(d: ReceivablesReportData): string {
  const rows = d.rows
    .map(
      (r) => `<tr>
        <td class="center">${esc(r.no)}</td><td>${esc(r.date)}</td><td>${esc(r.customer)}</td>
        <td class="num">${esc(r.price)}</td><td class="num">${esc(r.paid)}</td>
        <td class="num">${esc(r.balance)}</td><td class="center">${esc(r.status)}</td>
      </tr>`,
    )
    .join('');
  const body = `
    <table>
      <thead><tr>
        <th>No.</th><th>Date</th><th>Customer</th>
        <th class="num">Price</th><th class="num">Paid</th><th class="num">Balance</th><th>Status</th>
      </tr></thead>
      <tbody>${rows || '<tr><td colspan="7" class="center">Nothing outstanding — all settled</td></tr>'}</tbody>
      <tfoot class="totals"><tr>
        <td colspan="3">TOTAL — ${d.totals.count} invoice(s) outstanding</td>
        <td></td><td></td>
        <td class="num">${esc(d.totals.balance)}</td><td></td>
      </tr></tfoot>
    </table>`;
  return shell('Receivables Report', d.dealer, 'Invoices with an outstanding balance', d.generatedAt, body);
}
