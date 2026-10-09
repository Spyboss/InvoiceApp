import { randomUUID } from 'node:crypto';
import { eq } from 'drizzle-orm';
import { Hono } from 'hono';
import { customers, type Db } from '@erp/db';
import { customerSchema } from '@erp/shared';
import { audit } from '../audit';
import { requireAuth } from '../auth';

export function customerRoutes(db: Db): Hono {
  const app = new Hono();
  app.use('*', requireAuth);

  app.get('/', async (c) => {
    const q = c.req.query('q');
    const rows = await db.query.customers.findMany({
      where: q
        ? (t, { ilike, or }) => or(ilike(t.name, `%${q}%`), ilike(t.nic, `%${q}%`))
        : undefined,
      limit: 100,
      orderBy: (t, { desc }) => [desc(t.createdAt)],
    });
    return c.json({ customers: rows });
  });

  app.get('/:id', async (c) => {
    const [row] = await db.select().from(customers).where(eq(customers.id, c.req.param('id'))).limit(1);
    if (!row) return c.json({ error: 'not found' }, 404);
    return c.json({ customer: row });
  });

  app.post('/', async (c) => {
    const parsed = customerSchema.safeParse(await c.req.json());
    if (!parsed.success) return c.json({ error: parsed.error.flatten() }, 400);
    const row = { id: randomUUID(), ...parsed.data, nic: parsed.data.nic || null, phone: parsed.data.phone || null, address: parsed.data.address || null };
    await db.insert(customers).values(row);
    await audit(db, c.get('user'), 'CREATE', 'customer', row.id, null, row);
    return c.json({ customer: row }, 201);
  });

  app.patch('/:id', async (c) => {
    const id = c.req.param('id');
    const [before] = await db.select().from(customers).where(eq(customers.id, id)).limit(1);
    if (!before) return c.json({ error: 'not found' }, 404);
    const parsed = customerSchema.partial().safeParse(await c.req.json());
    if (!parsed.success) return c.json({ error: parsed.error.flatten() }, 400);
    await db.update(customers).set(parsed.data).where(eq(customers.id, id));
    const [after] = await db.select().from(customers).where(eq(customers.id, id)).limit(1);
    await audit(db, c.get('user'), 'UPDATE', 'customer', id, before, after);
    return c.json({ customer: after });
  });

  return app;
}
