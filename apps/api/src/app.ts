import fs from 'node:fs';
import path from 'node:path';
import { Hono } from 'hono';
import { cors } from 'hono/cors';
import type { Db } from '@erp/db';
import { authRoutes } from './routes/auth';
import { customerRoutes } from './routes/customers';
import { invoiceRoutes } from './routes/invoices';
import { paymentRoutes } from './routes/payments';
import { partRoutes } from './routes/parts';
import { reportRoutes } from './routes/reports';
import { vehicleRoutes } from './routes/vehicles';

// Built React client (local edition: one process serves API + UI).
const WEB_DIST = path.join(import.meta.dir, '..', '..', 'web', 'dist');

const MIME: Record<string, string> = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.ico': 'image/x-icon',
  '.json': 'application/json',
  '.map': 'application/json',
  '.woff': 'font/woff',
  '.woff2': 'font/woff2',
  '.txt': 'text/plain; charset=utf-8',
};

export function createApp(db: Db): Hono {
  const app = new Hono();

  app.use('*', cors()); // web dev server runs on a different origin
  app.get('/health', (c) => c.json({ ok: true, service: 'erp-api' }));

  app.route('/api/auth', authRoutes(db));
  app.route('/api/customers', customerRoutes(db));
  app.route('/api/vehicles', vehicleRoutes(db));
  app.route('/api/invoices', invoiceRoutes(db));
  app.route('/api/payments', paymentRoutes(db));
  app.route('/api/parts', partRoutes(db));
  app.route('/api/reports', reportRoutes(db));

  app.notFound((c) => {
    const reqPath = new URL(c.req.url).pathname;
    if (reqPath.startsWith('/api/') || reqPath === '/health') {
      return c.json({ error: 'not found' }, 404);
    }
    // Local edition: serve the built client; unknown paths fall back to the
    // SPA shell (client-side state routing — no server routes to match).
    const indexFile = path.join(WEB_DIST, 'index.html');
    if (!fs.existsSync(indexFile)) return c.json({ error: 'not found' }, 404);
    if (reqPath !== '/') {
      const target = path.resolve(path.join(WEB_DIST, `.${reqPath}`));
      if (target.startsWith(WEB_DIST + path.sep) && fs.existsSync(target) && fs.statSync(target).isFile()) {
        return c.newResponse(Bun.file(target), 200, { 'Content-Type': MIME[path.extname(target).toLowerCase()] ?? 'application/octet-stream' });
      }
    }
    return c.newResponse(Bun.file(indexFile), 200, { 'Content-Type': MIME['.html'] ?? 'text/html; charset=utf-8' });
  });
  app.onError((err, c) => {
    console.error('[api]', err);
    const status = (err as { status?: number }).status ?? 500;
    return c.json({ error: err.message || 'internal error' }, status as 500);
  });

  return app;
}
