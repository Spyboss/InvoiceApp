import { Hono } from 'hono';
import { type Db } from '@erp/db';
import { DEALER, type InvoiceType } from '@erp/shared';
import {
  buildReceivablesReportHtml,
  buildSalesReportHtml,
  buildStockReportHtml,
  renderPdf,
} from '@erp/pdf';
import { requireAuth } from '../auth';
import { fromCents, toCents } from '../money';

const iso = (d: Date) => d.toISOString().slice(0, 10);
const rs = (v: string | number) =>
  `Rs. ${Number(v).toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;

const TYPE_LABEL: Record<InvoiceType, string> = {
  PROFORMA: 'Proforma',
  SALES_CASH: 'Sales · Cash',
  SALES_LEASING: 'Sales · Leasing',
};

const sumCents = (vals: string[]) => vals.reduce((a, v) => a + toCents(v), 0);
const qtyValueCents = (qty: string, cost: string) => Math.round((toCents(qty) * toCents(cost)) / 100);

function sendPdf(c: any, html: string, filename: string) {
  const pdf = renderPdf(html);
  return c.body(pdf, 200, {
    'Content-Type': 'application/pdf',
    'Content-Disposition': `attachment; filename="${filename}"`,
  });
}

export function reportRoutes(db: Db): Hono {
  const app = new Hono();
  app.use('*', requireAuth);

  // Invoices issued in a period (default: current month) with totals.
  app.get('/sales/pdf', async (c) => {
    const now = new Date();
    const from = c.req.query('from') ?? `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-01`;
    const to = c.req.query('to') ?? iso(now);
    const start = new Date(`${from}T00:00:00`);
    const end = new Date(`${to}T23:59:59.999`);

    const rows = await db.query.invoices.findMany({
      where: (t, { and, gte, lte }) => and(gte(t.date, start), lte(t.date, end)),
      with: { customer: true, vehicle: true },
      orderBy: (t, { asc }) => [asc(t.date)],
      limit: 1000,
    });

    const html = buildSalesReportHtml({
      dealer: DEALER,
      from,
      to,
      generatedAt: new Date().toLocaleString('en-GB'),
      rows: rows.map((inv) => ({
        no: inv.invoiceNo,
        date: iso(new Date(inv.date)),
        type: TYPE_LABEL[inv.invoiceType] ?? inv.invoiceType,
        customer: inv.customer.name,
        vehicle: inv.model ?? inv.vehicle?.model ?? '',
        price: rs(inv.price),
        down: rs(inv.down),
        balance: rs(inv.balance),
        status: inv.status,
      })),
      totals: {
        count: rows.length,
        price: rs(fromCents(sumCents(rows.map((r) => r.price)))),
        down: rs(fromCents(sumCents(rows.map((r) => r.down)))),
        balance: rs(fromCents(sumCents(rows.map((r) => r.balance)))),
      },
    });
    return sendPdf(c, html, `SalesReport_${from}_${to}.pdf`);
  });

  // Whole yard: vehicles by status + parts inventory.
  app.get('/stock/pdf', async (c) => {
    const vehs = await db.query.vehicles.findMany({
      limit: 1000,
      orderBy: (t, { asc }) => [asc(t.receivedAt)],
    });
    const ps = await db.query.parts.findMany({
      limit: 1000,
      orderBy: (t, { asc }) => [asc(t.name)],
    });

    const statusOrder = ['AVAILABLE', 'RESERVED', 'SOLD', 'DELIVERED'];
    const byStatus = new Map<string, typeof vehs>();
    for (const v of vehs) {
      const list = byStatus.get(v.status) ?? [];
      list.push(v);
      byStatus.set(v.status, list);
    }

    const html = buildStockReportHtml({
      dealer: DEALER,
      generatedAt: new Date().toLocaleString('en-GB'),
      vehicleRows: vehs.map((v) => ({
        no: v.chassisNo,
        model: v.model,
        type: v.type,
        yom: String(v.yom),
        status: v.status,
        cost: rs(v.cost),
        price: rs(v.price),
      })),
      vehicleSummary: statusOrder
        .filter((s) => byStatus.has(s))
        .map((s) => {
          const list = byStatus.get(s)!;
          return {
            status: s,
            count: list.length,
            cost: rs(fromCents(sumCents(list.map((v) => v.cost)))),
            price: rs(fromCents(sumCents(list.map((v) => v.price)))),
          };
        }),
      partRows: ps.map((p) => ({
        sku: p.sku,
        name: p.name,
        qty: String(Number(p.qtyOnHand)),
        reorder: String(Number(p.reorderLevel)),
        value: rs(fromCents(qtyValueCents(p.qtyOnHand, p.cost))),
      })),
      partValue: rs(fromCents(ps.reduce((a, p) => a + qtyValueCents(p.qtyOnHand, p.cost), 0))),
      lowStock: ps.filter((p) => Number(p.qtyOnHand) <= Number(p.reorderLevel)).length,
    });
    return sendPdf(c, html, `StockReport_${iso(new Date())}.pdf`);
  });

  // Invoices that still carry a balance.
  app.get('/receivables/pdf', async (c) => {
    const all = await db.query.invoices.findMany({
      with: { customer: true },
      orderBy: (t, { asc }) => [asc(t.date)],
      limit: 1000,
    });
    const open = all.filter((i) => toCents(i.balance) > 0);

    const html = buildReceivablesReportHtml({
      dealer: DEALER,
      generatedAt: new Date().toLocaleString('en-GB'),
      rows: open.map((inv) => ({
        no: inv.invoiceNo,
        date: iso(new Date(inv.date)),
        customer: inv.customer.name,
        price: rs(inv.price),
        paid: rs(fromCents(toCents(inv.price) - toCents(inv.balance))),
        balance: rs(inv.balance),
        status: inv.status,
      })),
      totals: {
        count: open.length,
        balance: rs(fromCents(open.reduce((a, i) => a + toCents(i.balance), 0))),
      },
    });
    return sendPdf(c, html, `ReceivablesReport_${iso(new Date())}.pdf`);
  });

  return app;
}
