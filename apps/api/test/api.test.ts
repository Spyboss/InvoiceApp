import path from 'node:path';
import { beforeAll, describe, expect, test } from 'bun:test';
import { createDb, migrate } from '@erp/db';
import { findBrowser } from '@erp/pdf';
import { createApp } from '../src/app';

const db = createDb(); // no DATABASE_URL -> embedded PGlite (real Postgres, in-process)
const app = createApp(db);
const migrations = path.join(import.meta.dir, '..', '..', '..', 'packages', 'db', 'drizzle');

const request = (url: string, init: RequestInit = {}) =>
  app.request(url, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...init.headers,
    },
  });

let token = '';
let customerId = '';

const hasBrowser = (() => {
  try {
    findBrowser();
    return true;
  } catch {
    return false;
  }
})();

beforeAll(async () => {
  await migrate(db, migrations);
});

describe('auth', () => {
  test('first user bootstraps as ADMIN', async () => {
    const res = await request('/api/auth/register', {
      method: 'POST',
      body: JSON.stringify({ name: 'Boss', email: 'boss@gm.lk', password: 'supersecret1' }),
    });
    expect(res.status).toBe(201);
    const body = await res.json();
    expect(body.user.role).toBe('ADMIN');
    token = body.token;
  });

  test('login + me round-trip', async () => {
    const res = await request('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ email: 'boss@gm.lk', password: 'supersecret1' }),
    });
    expect(res.status).toBe(200);
    token = (await res.json()).token;
    const me = await (await request('/api/auth/me')).json();
    expect(me.user.email).toBe('boss@gm.lk');
    expect(me.user.role).toBe('ADMIN');
  });

  test('bad password is rejected', async () => {
    const res = await request('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ email: 'boss@gm.lk', password: 'wrong' }),
    });
    expect(res.status).toBe(401);
  });

  test('protected route without token -> 401', async () => {
    const res = await app.request('/api/customers');
    expect(res.status).toBe(401);
  });
});

describe('customers + invoices', () => {
  test('create customer', async () => {
    const res = await request('/api/customers', {
      method: 'POST',
      body: JSON.stringify({ name: 'Mudiyanselage Kasun Perera', nic: '199012345678', phone: '0711234567', address: 'Wewurukannala, Dikwella' }),
    });
    expect(res.status).toBe(201);
    customerId = (await res.json()).customer.id;
  });

  test('proforma invoice gets number 0001 and computes balance', async () => {
    const res = await request('/api/invoices', {
      method: 'POST',
      body: JSON.stringify({
        invoiceType: 'PROFORMA',
        customerId,
        price: '1795000.00',
        down: '295000.00',
        financeCompany: "People's Leasing PLC, Matara",
        financeAddress: 'No. 40, Sir de Mel Road, Matara',
      }),
    });
    expect(res.status).toBe(201);
    const inv = (await res.json()).invoice;
    expect(inv.invoiceNo).toBe('0001');
    expect(inv.balance).toBe('1500000.00');
    expect(inv.year).toBe(new Date().getFullYear());
  });

  test('second invoice in same year continues sequence', async () => {
    const res = await request('/api/invoices', {
      method: 'POST',
      body: JSON.stringify({ invoiceType: 'PROFORMA', customerId, price: '100000', down: '0' }),
    });
    expect((await res.json()).invoice.invoiceNo).toBe('0002');
  });

  test('payment draws down balance and cannot overpay', async () => {
    const list = await (await request('/api/invoices?type=PROFORMA')).json();
    const inv = list.invoices.find((i: { invoiceNo: string }) => i.invoiceNo === '0001');

    const ok = await request('/api/payments', {
      method: 'POST',
      body: JSON.stringify({ invoiceId: inv.id, amount: '500000', method: 'CASH' }),
    });
    expect(ok.status).toBe(201);
    expect((await ok.json()).invoiceBalance).toBe('1000000.00');

    const tooMuch = await request('/api/payments', {
      method: 'POST',
      body: JSON.stringify({ invoiceId: inv.id, amount: '999999999', method: 'CASH' }),
    });
    expect(tooMuch.status).toBe(400);
  });

  test('invoice listing includes customer join', async () => {
    const body = await (await request('/api/invoices')).json();
    expect(body.invoices[0].customer.name).toBe('Mudiyanselage Kasun Perera');
  });

  test('proforma renders to a real PDF', async () => {
    if (!hasBrowser) return; // CI without a browser — parity harness covers this elsewhere
    const list = await (await request('/api/invoices?type=PROFORMA')).json();
    const inv = list.invoices.find((i: { invoiceNo: string }) => i.invoiceNo === '0001');
    const res = await request(`/api/invoices/${inv.id}/pdf`);
    expect(res.status).toBe(200);
    expect(res.headers.get('Content-Type')).toBe('application/pdf');
    const buf = new Uint8Array(await res.arrayBuffer());
    expect(buf.length).toBeGreaterThan(30_000);
    expect(String.fromCharCode(...buf.slice(0, 5))).toBe('%PDF-');
  });
});
