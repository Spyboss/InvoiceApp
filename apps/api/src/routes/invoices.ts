import { randomUUID } from 'node:crypto';
import { desc, eq } from 'drizzle-orm';
import { Hono } from 'hono';
import { customers, invoices, vehicles, type Db, type Invoice } from '@erp/db';
import type { Customer, Vehicle } from '@erp/db';
import { DEALER, invoiceCreateSchema } from '@erp/shared';
import {
  buildProformaHtml, buildSalesHtml, renderPdf,
  PROFORMA_REMARKS, PROFORMA_VAT_NO,
  type ProformaData, type SalesData,
} from '@erp/pdf';
import { audit } from '../audit';
import { requireAuth } from '../auth';
import { fromCents, toCents } from '../money';
import { allocateInvoiceNumber } from '../numbering';

type InvoiceWithRefs = Invoice & { customer: Customer; vehicle: Vehicle | null };

const iso = (d: Date) => d.toISOString().slice(0, 10);

/** Proforma document data — Singer (SRI LANKA) PLC. format, see @erp/pdf. */
function proformaData(inv: InvoiceWithRefs): ProformaData {
  const v = inv.vehicle;
  const d = new Date(inv.date);
  const day = (n: number) => String(n).padStart(2, '0');
  // the format prints prices ungrouped, e.g. "LKR 980000/-" — no separators
  const money = (x: string) => String(Number(x));
  return {
    invoiceNo: inv.invoiceNo,
    date: `${day(d.getDate())}/${day(d.getMonth() + 1)}/${d.getFullYear()}`,
    customer: inv.customer.name.toUpperCase(),
    nic: inv.customer.nic ?? '',
    contact: inv.customer.phone ?? '',
    financeCompany: inv.financeCompany ?? '',
    financeAddress: inv.financeAddress ?? '',
    // snapshot first, stock vehicle fallback (pre-snapshot rows)
    make: inv.make ?? v?.make ?? '',
    model: inv.model ?? v?.model ?? '',
    type: inv.vehicleType ?? v?.type ?? '',
    color: inv.color ?? v?.color ?? '',
    yom: inv.yom != null ? String(inv.yom) : v ? String(v.yom) : '',
    engine: inv.engineNo ?? v?.engineNo ?? '',
    chassis: inv.chassisNo ?? v?.chassisNo ?? '',
    price: money(inv.price),
    down: money(inv.down),
    balance: money(inv.balance),
    vatNo: PROFORMA_VAT_NO,
    remarks: PROFORMA_REMARKS,
  };
}

/** Sales document data — mirrors generate_sales_pdf() in web_app.py. */
function salesData(inv: InvoiceWithRefs): SalesData {
  const v = inv.vehicle;
  return {
    invoiceNo: inv.invoiceNo,
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

    // One transaction: vehicle lock + number allocation + invoice row + stock
    // flip + audit commit or roll back together — a failure can never leave a
    // sold vehicle AVAILABLE (double-sell) or an invoice missing its number.
    let row!: typeof invoices.$inferInsert;
    try {
      row = await (db as any).transaction(async (tx: any) => {
        // snapshot: authoritative values come from the stock vehicle when attached
        let snapshot = {
          make: input.make ?? null,
          model: input.model ?? null,
          engineNo: input.engineNo ?? null,
          chassisNo: input.chassisNo ?? null,
          color: input.color ?? null,
          vehicleType: input.vehicleType ?? null,
          yom: input.yom ?? null,
        };
        if (input.vehicleId) {
          // FOR UPDATE: a concurrent create for the same vehicle blocks here
          const [v] = await tx
            .select().from(vehicles)
            .where(eq(vehicles.id, input.vehicleId))
            .for('update')
            .limit(1);
          if (!v) throw new Error('unknown vehicle');
          if (v.status !== 'AVAILABLE') throw new Error(`vehicle not available (status ${v.status})`);
          snapshot = {
            make: v.make,
            model: v.model, engineNo: v.engineNo, chassisNo: v.chassisNo,
            color: v.color, vehicleType: v.type, yom: v.yom,
          };
        }

        const invoiceNo = await allocateInvoiceNumber(tx, input.invoiceType, year);
        const row: typeof invoices.$inferInsert = {
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
        await tx.insert(invoices).values(row);

        if (input.vehicleId) {
          await tx.update(vehicles).set({ status: 'SOLD', invoiceId: id }).where(eq(vehicles.id, input.vehicleId));
        }
        await audit(tx, c.get('user'), 'CREATE', 'invoice', id, null, row as unknown as Record<string, unknown>);
        return row;
      });
    } catch (e) {
      const msg = e instanceof Error ? e.message : '';
      if (msg === 'unknown vehicle' || msg.startsWith('vehicle not available')) {
        return c.json({ error: msg }, 400);
      }
      throw e;
    }
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
