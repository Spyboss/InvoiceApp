import { randomUUID } from 'node:crypto';
import { desc, eq } from 'drizzle-orm';
import { Hono } from 'hono';
import { customers, invoices, vehicles, type Db, type Invoice } from '@erp/db';
import { invoiceCreateSchema } from '@erp/shared';
import { buildProformaHtml, renderPdf } from '@erp/pdf';
import { audit } from '../audit';
import { requireAuth } from '../auth';
import { fromCents, toCents } from '../money';
import { nextInvoiceNumber } from '../numbering';

/** Map a stored invoice row onto the proforma template data. */
function proformaData(inv: Invoice, cust: { name: string; nic: string | null; address: string | null }) {
  const spaced = (s: string) => s.split('').join(' ');
  return {
    invoiceNo: spaced(inv.invoiceNo),
    date: `${String(new Date(inv.date).getDate()).padStart(2, '0')} /${String(new Date(inv.date).getMonth() + 1).padStart(2, '0')}/${new Date(inv.date).getFullYear()}`,
    customer: cust.name.toUpperCase(),
    custAddr: cust.address ?? '',
    nic: cust.nic ?? '',
    financeCompany: inv.financeCompany ?? '',
    financeAddress: inv.financeAddress ?? '',
    model: '', type: '', color: '', yom: '', engine: '', chassis: '', // vehicle detail joins land in M2
    price: `RS.${Number(inv.price).toLocaleString('en-US', { minimumFractionDigits: 2 })}`,
    down: `RS.${Number(inv.down).toLocaleString('en-US', { minimumFractionDigits: 2 })}`,
    balance: `RS .${Number(inv.balance).toLocaleString('en-US', { minimumFractionDigits: 2 })}`,
    signatory: inv.signatory ?? 'Yashodha Rashmi',
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
      with: { customer: true, payments: true },
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
    const row = {
      id,
      invoiceType: input.invoiceType,
      invoiceNo,
      year,
      date,
      customerId: input.customerId,
      financeCompany: input.financeCompany ?? null,
      financeAddress: input.financeAddress ?? null,
      dealer: input.dealer ?? null,
      vehicleId: input.vehicleId ?? null,
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
    const [inv] = await db.select().from(invoices).where(eq(invoices.id, c.req.param('id'))).limit(1);
    if (!inv) return c.json({ error: 'not found' }, 404);
    const [cust] = await db.select().from(customers).where(eq(customers.id, inv.customerId)).limit(1);
    if (!cust) return c.json({ error: 'invoice has no customer' }, 500);

    if (inv.invoiceType !== 'PROFORMA') {
      return c.json({ error: 'sales invoice template lands in M2 — use type=PROFORMA for now' }, 501);
    }
    const pdf = renderPdf(buildProformaHtml(proformaData(inv, cust)));
    return c.body(pdf, 200, {
      'Content-Type': 'application/pdf',
      'Content-Disposition': `attachment; filename="Proforma_${inv.invoiceNo.replace(/\\s/g, '')}.pdf"`,
    });
  });

  return app;
}
