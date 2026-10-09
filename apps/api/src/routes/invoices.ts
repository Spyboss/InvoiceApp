import { randomUUID } from 'node:crypto';
import { desc, eq } from 'drizzle-orm';
import { Hono } from 'hono';
import { customers, invoices, vehicles, type Db, type Invoice } from '@erp/db';
import type { Customer, Vehicle } from '@erp/db';
import { DEALER, DEFAULT_SIGNATORY, invoiceCreateSchema } from '@erp/shared';
import { buildProformaHtml, buildSalesHtml, renderPdf, type ProformaData, type SalesData } from '@erp/pdf';
import { audit } from '../audit';
import { requireAuth } from '../auth';
import { fromCents, toCents } from '../money';
import { nextInvoiceNumber } from '../numbering';

type InvoiceWithRefs = Invoice & { customer: Customer; vehicle: Vehicle | null };

const iso = (d: Date) => d.toISOString().slice(0, 10);

/** Proforma document data — mirrors generate_proforma_pdf() in web_app.py. */
function proformaData(inv: InvoiceWithRefs): ProformaData {
  const v = inv.vehicle;
  const spaced = (s: string) => s.split('').join(' ');
  const d = new Date(inv.date);
  return {
    invoiceNo: spaced(inv.invoiceNo),
    date: `${String(d.getDate()).padStart(2, '0')} /${String(d.getMonth() + 1).padStart(2, '0')}/${d.getFullYear()}`,
    customer: inv.customer.name.toUpperCase(),
    custAddr: inv.customer.address ?? '',
    nic: inv.customer.nic ?? '',
    financeCompany: inv.financeCompany ?? '',
    financeAddress: inv.financeAddress ?? '',
    // snapshot first, stock vehicle fallback (pre-snapshot rows)
    model: inv.model ?? v?.model ?? '',
    type: inv.vehicleType ?? v?.type ?? '',
    color: inv.color ?? v?.color ?? '',
    yom: inv.yom != null ? String(inv.yom) : v ? String(v.yom) : '',
    engine: inv.engineNo ?? v?.engineNo ?? '',
    chassis: inv.chassisNo ?? v?.chassisNo ?? '',
    price: `RS.${Number(inv.price).toLocaleString('en-US', { minimumFractionDigits: 2 })}`,
    down: `RS.${Number(inv.down).toLocaleString('en-US', { minimumFractionDigits: 2 })}`,
    balance: `RS .${Number(inv.balance).toLocaleString('en-US', { minimumFractionDigits: 2 })}`,
    signatory: inv.signatory ?? DEFAULT_SIGNATORY,
  };
}

/** Sales document data — mirrors generate_sales_pdf() in web_app.py. */
function salesData(inv: InvoiceWithRefs): SalesData {
  const v = inv.vehicle;
  return {
    invoiceNo: inv.invoiceNo,                       // printed raw (unlike proforma's spaced style)
    date: iso(new Date(inv.date)),
    dealer: inv.dealer ?? DEALER,
    customer: inv.customer.name,
    custAddr: inv.customer.address ?? '',
    nic: inv.customer.nic ?? '',
    model: inv.model ?? v?.model ?? '',
    engine: inv.engineNo ?? v?.engineNo ?? '',
    chassis: inv.chassisNo ?? v?.chassisNo ?? '',
    color: inv.color ?? v?.color ?? '',
    year: inv.yom != null ? String(inv.yom) : v ? String(v.yom) : String(new Date().getFullYear()), // explicit yom, stock yom, else current year
    price: inv.price,
    down: inv.down,
    balance: inv.balance,
    isLeasing: inv.invoiceType === 'SALES_LEASING',
    delivery: inv.delivery ?? '',
  };
}

export function invoiceRoutes(db: Db): Hono {
  const app = new Hono();
  app.use('*', requireAuth);

  app.get('/', async (c) => {
    const type = c.req.query('type');
    const rows = await db.query.invoices.findMany({
      where: type ? (t, { eq }) => eq(t.invoiceType, type as Invoice['invoiceType']) : undefined,
      with: { customer: true },
      limit: 100,
      orderBy: [desc(invoices.createdAt)],
    });
    return c.json({ invoices: rows });
  });

  app.get('/:id', async (c) => {
    const [row] = await db.query.invoices.findMany({
      where: (t, { eq }) => eq(t.id, c.req.param('id')),
      with: { customer: true, payments: true, vehicle: true },
      limit: 1,
    });
    if (!row) return c.json({ error: 'not found' }, 404);
    return c.json({ invoice: row });
  });

  app.post('/', async (c) => {
    const parsed = invoiceCreateSchema.safeParse(await c.req.json());
    if (!parsed.success) return c.json({ error: parsed.error.flatten() }, 400);
    const input = parsed.data;

    const [cust] = await db.select().from(customers).where(eq(customers.id, input.customerId)).limit(1);
    if (!cust) return c.json({ error: 'unknown customer' }, 400);

    const date = new Date(input.date);
    const year = date.getFullYear();
    const balance = fromCents(toCents(input.price) - toCents(input.down));

    const id = randomUUID();
    const invoiceNo = await nextInvoiceNumber(db, input.invoiceType, year);

    // snapshot: authoritative values come from the stock vehicle when attached
    let snapshot = {
      model: input.model ?? null,
      engineNo: input.engineNo ?? null,
      chassisNo: input.chassisNo ?? null,
      color: input.color ?? null,
      vehicleType: input.vehicleType ?? null,
      yom: input.yom ?? null,
    };
    if (input.vehicleId) {
      const [v] = await db.select().from(vehicles).where(eq(vehicles.id, input.vehicleId)).limit(1);
      if (!v) return c.json({ error: 'unknown vehicle' }, 400);
      snapshot = {
        model: v.model, engineNo: v.engineNo, chassisNo: v.chassisNo,
        color: v.color, vehicleType: v.type, yom: v.yom,
      };
    }

    const row = {
      id,
      invoiceType: input.invoiceType,
      invoiceNo,
      year,
      date,
      customerId: input.customerId,
      financeCompany: input.financeCompany ?? null,
      financeAddress: input.financeAddress ?? null,
      delivery: input.delivery ?? null,
      dealer: input.dealer ?? null,
      vehicleId: input.vehicleId ?? null,
      ...snapshot,
      price: input.price,
      down: input.down,
      balance,
      isLeasing: input.invoiceType === 'SALES_LEASING',
      signatory: input.signatory ?? null,
      status: input.status,
      createdBy: c.get('user').sub,
    };
    await db.insert(invoices).values(row);

    if (input.vehicleId) {
      await db.update(vehicles).set({ status: 'SOLD', invoiceId: id }).where(eq(vehicles.id, input.vehicleId));
    }
    await audit(db, c.get('user'), 'CREATE', 'invoice', id, null, row as unknown as Record<string, unknown>);
    return c.json({ invoice: row }, 201);
  });

  app.get('/:id/pdf', async (c) => {
    const inv = (await db.query.invoices.findFirst({
      where: (t, { eq }) => eq(t.id, c.req.param('id')),
      with: { customer: true, vehicle: true },
    })) as InvoiceWithRefs | undefined;
    if (!inv) return c.json({ error: 'not found' }, 404);

    const html = inv.invoiceType === 'PROFORMA'
      ? buildProformaHtml(proformaData(inv))
      : buildSalesHtml(salesData(inv));
    const pdf = renderPdf(html);
    const name = `${inv.invoiceType === 'PROFORMA' ? 'Proforma' : 'Sales'}_${inv.invoiceNo}_${inv.customer.name.replace(/[^\w]+/g, '')}.pdf`;
    return c.body(pdf, 200, {
      'Content-Type': 'application/pdf',
      'Content-Disposition': `attachment; filename="${name}"`,
    });
  });

  return app;
}
