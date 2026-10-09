import { randomUUID } from 'node:crypto';
import { eq } from 'drizzle-orm';
import { Hono } from 'hono';
import { vehicles, type Db } from '@erp/db';
import { vehicleSchema, type VehicleStatus } from '@erp/shared';
import { audit } from '../audit';
import { requireAuth } from '../auth';

export function vehicleRoutes(db: Db): Hono {
  const app = new Hono();
  app.use('*', requireAuth);

  app.get('/', async (c) => {
    const status = c.req.query('status') as VehicleStatus | undefined;
    const rows = await db.query.vehicles.findMany({
      where: status ? (t, { eq }) => eq(t.status, status) : undefined,
      limit: 200,
      orderBy: (t, { desc }) => [desc(t.receivedAt)],
    });
    return c.json({ vehicles: rows });
  });

  app.get('/:id', async (c) => {
    const [row] = await db.select().from(vehicles).where(eq(vehicles.id, c.req.param('id'))).limit(1);
    if (!row) return c.json({ error: 'not found' }, 404);
    return c.json({ vehicle: row });
  });

  app.post('/', async (c) => {
    const parsed = vehicleSchema.safeParse(await c.req.json());
    if (!parsed.success) return c.json({ error: parsed.error.flatten() }, 400);
    const row = {
      id: randomUUID(),
      ...parsed.data,
      supplierId: parsed.data.supplierId ?? null,
      notes: parsed.data.notes ?? null,
    };
    try {
      await db.insert(vehicles).values(row);
    } catch (e) {
      if (String(e).includes('vehicles_chassis_idx')) return c.json({ error: 'chassis number already exists' }, 409);
      throw e;
    }
    await audit(db, c.get('user'), 'CREATE', 'vehicle', row.id, null, row);
    return c.json({ vehicle: row }, 201);
  });

  app.patch('/:id', async (c) => {
    const id = c.req.param('id');
    const [before] = await db.select().from(vehicles).where(eq(vehicles.id, id)).limit(1);
    if (!before) return c.json({ error: 'not found' }, 404);
    const parsed = vehicleSchema.partial().safeParse(await c.req.json());
    if (!parsed.success) return c.json({ error: parsed.error.flatten() }, 400);
    await db.update(vehicles).set(parsed.data).where(eq(vehicles.id, id));
    const [after] = await db.select().from(vehicles).where(eq(vehicles.id, id)).limit(1);
    await audit(db, c.get('user'), 'UPDATE', 'vehicle', id, before, after);
    return c.json({ vehicle: after });
  });

  return app;
}
