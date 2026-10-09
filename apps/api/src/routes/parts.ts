import { randomUUID } from 'node:crypto';
import { eq } from 'drizzle-orm';
import { Hono } from 'hono';
import { partMovements, parts, type Db } from '@erp/db';
import { partMovementPostSchema, partSchema } from '@erp/shared';
import { audit } from '../audit';
import { requireAuth } from '../auth';
import { fromCents, toCents } from '../money';

// Reasons that add stock must carry a positive qty; reasons that consume
// stock must be negative. ADJUSTMENT may go either way (stock take).
const IN_REASONS = new Set(['PURCHASE', 'RETURN']);
const OUT_REASONS = new Set(['SALE', 'SERVICE_JOB']);

export function partRoutes(db: Db): Hono {
  const app = new Hono();
  app.use('*', requireAuth);

  app.get('/', async (c) => {
    const q = c.req.query('q');
    const rows = await db.query.parts.findMany({
      where: q
        ? (t, { ilike, or }) => or(ilike(t.name, `%${q}%`), ilike(t.sku, `%${q}%`))
        : undefined,
      limit: 200,
      orderBy: (t, { asc }) => [asc(t.name)],
    });
    return c.json({ parts: rows });
  });

  app.post('/', async (c) => {
    const parsed = partSchema.safeParse(await c.req.json());
    if (!parsed.success) return c.json({ error: parsed.error.flatten() }, 400);
    const row = { id: randomUUID(), ...parsed.data };
    try {
      await db.insert(parts).values(row);
    } catch (e) {
      if (String(e).includes('parts_sku_idx')) return c.json({ error: 'sku already exists' }, 409);
      throw e;
    }
    await audit(db, c.get('user'), 'CREATE', 'part', row.id, null, row);
    return c.json({ part: row }, 201);
  });

  // Stock movement: lock the part, guard negative stock, update qty, append
  // the movement — all in one transaction.
  app.post('/:id/movements', async (c) => {
    const parsed = partMovementPostSchema.safeParse(await c.req.json());
    if (!parsed.success) return c.json({ error: parsed.error.flatten() }, 400);
    const input = parsed.data;

    if (IN_REASONS.has(input.reason) && input.qty <= 0) {
      return c.json({ error: `${input.reason} movements must be positive` }, 400);
    }
    if (OUT_REASONS.has(input.reason) && input.qty >= 0) {
      return c.json({ error: `${input.reason} movements must be negative` }, 400);
    }

    try {
      const result = await (db as any).transaction(async (tx: any) => {
        const [part] = await tx.select().from(parts).where(eq(parts.id, c.req.param('id'))).for('update').limit(1);
        if (!part) throw new Error('unknown part');

        const newQty = fromCents(toCents(part.qtyOnHand) + toCents(String(input.qty)));
        if (toCents(newQty) < 0) throw new Error(`insufficient stock (on hand: ${part.qtyOnHand})`);

        const movement = {
          id: randomUUID(),
          partId: part.id,
          qty: String(input.qty),
          reason: input.reason,
          refType: input.refType ?? null,
          refId: input.refId ?? null,
          createdBy: c.get('user').sub,
        };
        await tx.insert(partMovements).values(movement);
        await tx.update(parts).set({ qtyOnHand: newQty }).where(eq(parts.id, part.id));
        await audit(tx, c.get('user'), 'CREATE', 'part_movement', movement.id, null, movement);
        return { movement, qtyOnHand: newQty };
      });
      return c.json(result, 201);
    } catch (e) {
      const msg = e instanceof Error ? e.message : '';
      if (msg === 'unknown part' || msg.startsWith('insufficient stock')) {
        return c.json({ error: msg }, 400);
      }
      throw e;
    }
  });

  app.get('/:id/movements', async (c) => {
    const rows = await db.query.partMovements.findMany({
      where: (t, { eq }) => eq(t.partId, c.req.param('id')),
      orderBy: (t, { desc }) => [desc(t.createdAt)],
      limit: 200,
    });
    return c.json({ movements: rows });
  });

  return app;
}
