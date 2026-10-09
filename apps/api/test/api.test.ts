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

  test('customer search by q (name or nic)', async () => {
    const byName = await (await request('/api/customers?q=kasun')).json();
    expect(byName.customers).toHaveLength(1);
    expect(byName.customers[0].id).toBe(customerId);
    const byNic = await (await request('/api/customers?q=19901234')).json();
    expect(byNic.customers).toHaveLength(1);
    const none = await (await request('/api/customers?q=zzzznotfound')).json();
    expect(none.customers).toHaveLength(0);
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

  test('sales leasing invoice: vehicle mark + own counter + PDF', async () => {
    // stock unit to sell
    const vRes = await request('/api/vehicles', {
      method: 'POST',
      body: JSON.stringify({
        chassisNo: 'MBX1129CDTF771204', engineNo: 'A6F0187429',
        model: 'APE Classic DX', type: 'PICKUP', color: 'Star White',
        yom: 2026, price: '1795000', cost: '1500000',
      }),
    });
    expect(vRes.status).toBe(201);
    const vehicleId = (await vRes.json()).vehicle.id;

    const invRes = await request('/api/invoices', {
      method: 'POST',
      body: JSON.stringify({
        invoiceType: 'SALES_LEASING', customerId, vehicleId,
        price: '1795000.00', down: '295000.00',
        delivery: 'No. 14, Lake Road, Matara',
      }),
    });
    expect(invRes.status).toBe(201);
    const inv = (await invRes.json()).invoice;
    expect(inv.invoiceNo).toBe('0001'); // leasing has its own sequence
    expect(inv.balance).toBe('1500000.00');
    expect(inv.delivery).toBe('No. 14, Lake Road, Matara');
    // snapshot copied from the stock vehicle — documents are self-contained
    expect(inv.model).toBe('APE Classic DX');
    expect(inv.engineNo).toBe('A6F0187429');
    expect(inv.vehicleType).toBe('PICKUP');

    // stock is now SOLD and linked
    const v = await (await request(`/api/vehicles/${vehicleId}`)).json();
    expect(v.vehicle.status).toBe('SOLD');
    expect(v.vehicle.invoiceId).toBe(inv.id);

    if (!hasBrowser) return;
    const pdfRes = await request(`/api/invoices/${inv.id}/pdf`);
    expect(pdfRes.status).toBe(200);
    expect(pdfRes.headers.get('Content-Type')).toBe('application/pdf');
    const buf = new Uint8Array(await pdfRes.arrayBuffer());
    expect(buf.length).toBeGreaterThan(30_000);
    expect(String.fromCharCode(...buf.slice(0, 5))).toBe('%PDF-');
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
