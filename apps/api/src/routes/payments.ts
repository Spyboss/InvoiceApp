import { randomUUID } from 'node:crypto';
import { eq } from 'drizzle-orm';
import { Hono } from 'hono';
import { invoices, payments, type Db } from '@erp/db';
import { paymentSchema } from '@erp/shared';
import { audit } from '../audit';
import { requireAuth } from '../auth';
import { fromCents, toCents } from '../money';

export function paymentRoutes(db: Db): Hono {
  const app = new Hono();
  app.use('*', requireAuth);

  // Payment history for one invoice.
  app.get('/', async (c) => {
    const invoiceId = c.req.query('invoiceId');
    if (!invoiceId) return c.json({ error: 'invoiceId query param required' }, 400);
    const rows = await db.query.payments.findMany({
      where: (t, { eq }) => eq(t.invoiceId, invoiceId),
      orderBy: (t, { desc }) => [desc(t.paidAt)],
      limit: 200,
    });
    return c.json({ payments: rows });
  });

  // Record a payment and draw down the invoice balance — one transaction with
  // a FOR UPDATE lock on the invoice, so concurrent payments can't overdraw.
  app.post('/', async (c) => {
    const parsed = paymentSchema.safeParse(await c.req.json());
    if (!parsed.success) return c.json({ error: parsed.error.flatten() }, 400);
    const input = parsed.data;

    try {
      const result = await (db as any).transaction(async (tx: any) => {
        const [inv] = await tx
          .select().from(invoices)
          .where(eq(invoices.id, input.invoiceId))
          .for('update')
          .limit(1);
        if (!inv) throw new Error('unknown invoice');

        const newBalance = fromCents(toCents(inv.balance) - toCents(input.amount));
        if (toCents(newBalance) < 0) throw new Error('payment exceeds outstanding balance');

        const row = {
          id: randomUUID(),
          invoiceId: input.invoiceId,
          amount: input.amount,
          method: input.method,
          notes: input.notes ?? null,
          receivedBy: c.get('user').sub,
        };
        await tx.insert(payments).values(row);
        await tx.update(invoices).set({ balance: newBalance }).where(eq(invoices.id, input.invoiceId));
        await audit(tx, c.get('user'), 'CREATE', 'payment', row.id, null, row as unknown as Record<string, unknown>);
        return { payment: row, invoiceBalance: newBalance };
      });
      return c.json(result, 201);
    } catch (e) {
      const msg = e instanceof Error ? e.message : '';
      if (msg === 'unknown invoice' || msg === 'payment exceeds outstanding balance') {
        return c.json({ error: msg }, 400);
      }
      throw e;
    }
  });

  return app;
}
