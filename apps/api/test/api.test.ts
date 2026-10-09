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

  test('double-sell guard: one vehicle cannot back two invoices', async () => {
    const vRes = await request('/api/vehicles', {
      method: 'POST',
      body: JSON.stringify({
        chassisNo: 'MBX1129CDTF779901', engineNo: 'A6F0187499',
        model: 'APE Classic DX', type: 'PICKUP', color: 'Star White',
        yom: 2026, price: '1795000', cost: '1500000',
      }),
    });
    expect(vRes.status).toBe(201);
    const vid = (await vRes.json()).vehicle.id;

    const payload = JSON.stringify({
      invoiceType: 'SALES_CASH', customerId, vehicleId: vid,
      price: '1795000.00', down: '295000.00',
    });
    const first = await request('/api/invoices', { method: 'POST', body: payload });
    expect(first.status).toBe(201);
    const firstId = (await first.json()).invoice.id;

    const second = await request('/api/invoices', { method: 'POST', body: payload });
    expect(second.status).toBe(400);
    expect((await second.json()).error).toMatch(/vehicle not available/);

    // rolled back cleanly: still SOLD, linked to the FIRST invoice
    const v = await (await request(`/api/vehicles/${vid}`)).json();
    expect(v.vehicle.status).toBe('SOLD');
    expect(v.vehicle.invoiceId).toBe(firstId);
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

describe('stock: parts movements', () => {
  let partId = '';

  test('create part', async () => {
    const res = await request('/api/parts', {
      method: 'POST',
      body: JSON.stringify({
        sku: 'PLG-100', name: 'Brake Lining Set', category: 'BRAKES',
        cost: '1200.00', price: '1800.00', qtyOnHand: '0', reorderLevel: '5',
      }),
    });
    expect(res.status).toBe(201);
    partId = (await res.json()).part.id;
  });

  test('duplicate sku rejected', async () => {
    const res = await request('/api/parts', {
      method: 'POST',
      body: JSON.stringify({ sku: 'PLG-100', name: 'Duplicate' }),
    });
    expect(res.status).toBe(409);
  });

  test('receive stock: PURCHASE must be positive', async () => {
    const ok = await request(`/api/parts/${partId}/movements`, {
      method: 'POST',
      body: JSON.stringify({ qty: 10, reason: 'PURCHASE' }),
    });
    expect(ok.status).toBe(201);
    expect((await ok.json()).qtyOnHand).toBe('10.00');

    const bad = await request(`/api/parts/${partId}/movements`, {
      method: 'POST',
      body: JSON.stringify({ qty: -3, reason: 'PURCHASE' }),
    });
    expect(bad.status).toBe(400);
    expect((await bad.json()).error).toMatch(/must be positive/);
  });

  test('issue stock: SERVICE_JOB must be negative', async () => {
    const ok = await request(`/api/parts/${partId}/movements`, {
      method: 'POST',
      body: JSON.stringify({ qty: -4, reason: 'SERVICE_JOB' }),
    });
    expect(ok.status).toBe(201);
    expect((await ok.json()).qtyOnHand).toBe('6.00');

    const bad = await request(`/api/parts/${partId}/movements`, {
      method: 'POST',
      body: JSON.stringify({ qty: 4, reason: 'SERVICE_JOB' }),
    });
    expect(bad.status).toBe(400);
    expect((await bad.json()).error).toMatch(/must be negative/);
  });

  test('cannot issue more than on hand', async () => {
    const res = await request(`/api/parts/${partId}/movements`, {
      method: 'POST',
      body: JSON.stringify({ qty: -7, reason: 'SERVICE_JOB' }),
    });
    expect(res.status).toBe(400);
    expect((await res.json()).error).toMatch(/insufficient stock/);

    const list = await (await request('/api/parts?q=lining')).json();
    expect(list.parts[0].qtyOnHand).toBe('6.00'); // unchanged after rollback
  });

  test('movement history newest first', async () => {
    const res = await (await request(`/api/parts/${partId}/movements`)).json();
    expect(res.movements).toHaveLength(2);
    expect(res.movements[0].reason).toBe('SERVICE_JOB');
    expect(res.movements[1].reason).toBe('PURCHASE');
  });
});

describe('payments history', () => {
  test('record payments then list newest first', async () => {
    const invRes = await request('/api/invoices', {
      method: 'POST',
      body: JSON.stringify({ invoiceType: 'PROFORMA', customerId, price: '100000.00', down: '0.00' }),
    });
    expect(invRes.status).toBe(201);
    const id = (await invRes.json()).invoice.id;

    const p1 = await request('/api/payments', {
      method: 'POST',
      body: JSON.stringify({ invoiceId: id, amount: '40000.00', method: 'CASH' }),
    });
    expect(p1.status).toBe(201);
    const p2 = await request('/api/payments', {
      method: 'POST',
      body: JSON.stringify({ invoiceId: id, amount: '10000.00', method: 'CHEQUE' }),
    });
    expect(p2.status).toBe(201);

    const hist = await (await request(`/api/payments?invoiceId=${id}`)).json();
    expect(hist.payments).toHaveLength(2);
    expect(hist.payments[0].method).toBe('CHEQUE');
    expect(hist.payments[1].method).toBe('CASH');

    const inv = await (await request(`/api/invoices/${id}`)).json();
    expect(inv.invoice.balance).toBe('50000.00');
  });

  test('invoiceId query param required', async () => {
    expect((await request('/api/payments')).status).toBe(400);
  });
});

describe('reports', () => {
  const checkPdf = async (url: string) => {
    const res = await request(url);
    expect(res.status).toBe(200);
    expect(res.headers.get('Content-Type')).toBe('application/pdf');
    const buf = new Uint8Array(await res.arrayBuffer());
    expect(String.fromCharCode(...buf.slice(0, 5))).toBe('%PDF-');
    expect(buf.length).toBeGreaterThan(20_000);
  };

  test('sales report renders a PDF', async () => {
    if (!hasBrowser) return;
    await checkPdf('/api/reports/sales/pdf?from=2026-01-01&to=2026-12-31');
  });

  test('stock report renders a PDF', async () => {
    if (!hasBrowser) return;
    await checkPdf('/api/reports/stock/pdf');
  });

  test('receivables report renders a PDF', async () => {
    if (!hasBrowser) return;
    await checkPdf('/api/reports/receivables/pdf');
  });
});
