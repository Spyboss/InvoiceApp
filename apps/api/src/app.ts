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

  app.notFound((c) => c.json({ error: 'not found' }, 404));
  app.onError((err, c) => {
    console.error('[api]', err);
    const status = (err as { status?: number }).status ?? 500;
    return c.json({ error: err.message || 'internal error' }, status as 500);
  });

  return app;
}
