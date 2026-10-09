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

  // Record a payment and draw down the invoice balance.
  app.post('/', async (c) => {
    const parsed = paymentSchema.safeParse(await c.req.json());
    if (!parsed.success) return c.json({ error: parsed.error.flatten() }, 400);
    const input = parsed.data;

    const [inv] = await db.select().from(invoices).where(eq(invoices.id, input.invoiceId)).limit(1);
    if (!inv) return c.json({ error: 'unknown invoice' }, 400);

    const newBalance = fromCents(toCents(inv.balance) - toCents(input.amount));
    if (toCents(newBalance) < 0) return c.json({ error: 'payment exceeds outstanding balance' }, 400);

    const row = {
      id: randomUUID(),
      invoiceId: input.invoiceId,
      amount: input.amount,
      method: input.method,
      notes: input.notes ?? null,
      receivedBy: c.get('user').sub,
    };
    await db.insert(payments).values(row);
    await db.update(invoices).set({ balance: newBalance }).where(eq(invoices.id, input.invoiceId));
    await audit(db, c.get('user'), 'CREATE', 'payment', row.id, null, row as unknown as Record<string, unknown>);
    return c.json({ payment: row, invoiceBalance: newBalance }, 201);
  });

  return app;
}
