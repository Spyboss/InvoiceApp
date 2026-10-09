import { randomUUID } from 'node:crypto';
import { count, eq } from 'drizzle-orm';
import { Hono } from 'hono';
import { customers, users, type Db } from '@erp/db';
import { loginSchema, registerSchema } from '@erp/shared';
import { audit } from '../audit';
import { hashPassword, requireAuth, signToken, verifyPassword, verifyToken, type JwtUser } from '../auth';

export function authRoutes(db: Db): Hono {
  const app = new Hono();

  // First user always becomes ADMIN (bootstrap); afterwards admin-only.
  app.post('/register', async (c) => {
    const parsed = registerSchema.safeParse(await c.req.json());
    if (!parsed.success) return c.json({ error: parsed.error.flatten() }, 400);

    const countRows = await db.select({ value: count() }).from(users);
    const userCount = countRows[0]?.value ?? 0;
    let actor: JwtUser | undefined;
    if ((userCount ?? 0) > 0) {
      const header = c.req.header('Authorization') ?? '';
      const token = header.startsWith('Bearer ') ? header.slice(7) : '';
      if (!token) return c.json({ error: 'unauthorized' }, 401);
      try {
        actor = await verifyToken(token);
      } catch {
        return c.json({ error: 'invalid token' }, 401);
      }
      if (actor.role !== 'ADMIN') return c.json({ error: 'forbidden' }, 403);
    }

    const input = parsed.data;
    const email = input.email.toLowerCase();
    const exists = await db.select().from(users).where(eq(users.email, email)).limit(1);
    if (exists.length) return c.json({ error: 'email already registered' }, 409);

    const user = {
      id: randomUUID(),
      name: input.name,
      email,
      passwordHash: await hashPassword(input.password),
      role: userCount === 0 ? ('ADMIN' as const) : input.role,
    };
    await db.insert(users).values(user);
    await audit(db, actor, 'CREATE', 'user', user.id, null, { email, role: user.role });

    const token = await signToken({ sub: user.id, name: user.name, role: user.role });
    return c.json({ token, user: { id: user.id, name: user.name, email, role: user.role } }, 201);
  });

  app.post('/login', async (c) => {
    const parsed = loginSchema.safeParse(await c.req.json());
    if (!parsed.success) return c.json({ error: parsed.error.flatten() }, 400);

    const email = parsed.data.email.toLowerCase();
    const [user] = await db.select().from(users).where(eq(users.email, email)).limit(1);
    if (!user || !user.active || !(await verifyPassword(user.passwordHash, parsed.data.password))) {
      return c.json({ error: 'invalid credentials' }, 401);
    }
    const token = await signToken({ sub: user.id, name: user.name, role: user.role });
    return c.json({ token, user: { id: user.id, name: user.name, email: user.email, role: user.role } });
  });

  app.get('/me', requireAuth, async (c) => {
    const jwtUser = c.get('user');
    const [user] = await db
      .select({ id: users.id, name: users.name, email: users.email, role: users.role, active: users.active })
      .from(users)
      .where(eq(users.id, jwtUser.sub))
      .limit(1);
    if (!user) return c.json({ error: 'not found' }, 404);
    return c.json({ user });
  });

  return app;
}
