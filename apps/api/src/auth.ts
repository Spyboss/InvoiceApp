import { SignJWT, jwtVerify } from 'jose';
import type { Context, Next } from 'hono';
import type { Role } from '@erp/shared';
import { env } from './env';

const secret = new TextEncoder().encode(env.JWT_SECRET);

export interface JwtUser {
  sub: string;      // user id
  name: string;
  role: Role;
}

export async function hashPassword(plain: string): Promise<string> {
  return Bun.password.hash(plain);            // scrypt by default in Bun
}

export async function verifyPassword(hash: string, plain: string): Promise<boolean> {
  return Bun.password.verify(plain, hash);
}

export async function signToken(user: JwtUser): Promise<string> {
  return new SignJWT({ name: user.name, role: user.role })
    .setProtectedHeader({ alg: 'HS256' })
    .setSubject(user.sub)
    .setIssuedAt()
    .setExpirationTime('12h')
    .sign(secret);
}

export async function verifyToken(token: string): Promise<JwtUser> {
  const { payload } = await jwtVerify(token, secret, { algorithms: ['HS256'] });
  if (!payload.sub || !payload.role) throw new Error('invalid token');
  return { sub: payload.sub, name: String(payload.name ?? ''), role: payload.role as Role };
}

/** Requires a valid bearer token; sets c.var.user. */
export async function requireAuth(c: Context, next: Next) {
  const header = c.req.header('Authorization') ?? '';
  const token = header.startsWith('Bearer ') ? header.slice(7) : '';
  if (!token) return c.json({ error: 'unauthorized' }, 401);
  try {
    c.set('user', await verifyToken(token));
  } catch {
    return c.json({ error: 'invalid token' }, 401);
  }
  await next();
}

/** Role gate — use after requireAuth. */
export function requireRole(...roles: Role[]) {
  return async (c: Context, next: Next) => {
    const user = c.get('user') as JwtUser | undefined;
    if (!user) return c.json({ error: 'unauthorized' }, 401);
    if (user.role === 'ADMIN' || roles.includes(user.role)) return next();
    return c.json({ error: 'forbidden' }, 403);
  };
}

declare module 'hono' {
  interface ContextVariableMap {
    user: JwtUser;
  }
}
