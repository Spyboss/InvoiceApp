/**
 * UI end-to-end: register → invoice form → PDF download → invoice list.
 * Uses system Edge via playwright-core (no browser download).
 * Requires: API on :8787 (fresh DB_DATA_DIR) + `vite dev` on :5173.
 */
import { chromium } from 'playwright-core';

const BASE = process.env.E2E_BASE ?? 'http://localhost:5173';
const EMAIL = `e2e_${Date.now()}@gm.lk`;

const browser = await chromium.launch({ channel: 'msedge', headless: true });
const context = await browser.newContext({ acceptDownloads: true });
const page = await context.newPage();
const pageErrors: string[] = [];
page.on('pageerror', (e) => pageErrors.push(String(e)));

try {
  await page.goto(BASE, { waitUntil: 'networkidle' });

  // 1. register first user on the fresh DB (becomes ADMIN)
  await page.getByText('No account yet? Register the first user').click();
  await page.fill('#name', 'E2E Admin');
  await page.fill('#email', EMAIL);
  await page.fill('#password', 'e2epassword1');
  await page.getByRole('button', { name: 'Register', exact: true }).click();
  await page.waitForSelector('text=Invoice Generator', { timeout: 15000 });
  await page.screenshot({ path: 'e2e-1-form.png' });

  // 2. fill an invoice manually (no stock vehicle)
  await page.fill('input[name="customer_name"]', 'E2E Customer');
  await page.fill('input[name="nic"]', '199912345678');
  await page.fill('input[name="model"]', 'APE Classic DX');
  await page.fill('input[name="price"]', '1795000');
  await page.fill('input[name="down"]', '295000');

  const [download] = await Promise.all([
    page.waitForEvent('download', { timeout: 45000 }),
    page.click('button:has-text("Create Invoice & Download PDF")'),
  ]);
  const fname = download.suggestedFilename();
  await download.saveAs('e2e-invoice.pdf');
  console.log('DOWNLOAD OK:', fname);
  await page.waitForSelector('text=PDF downloaded', { timeout: 15000 });

  // 3. invoice list shows the record
  await page.click('button:has-text("Invoices")');
  await page.waitForSelector('text=E2E Customer', { timeout: 15000 });
  const rows = await page.locator('tbody tr').count();
  console.log('LIST ROWS:', rows);
  await page.screenshot({ path: 'e2e-2-list.png' });

  if (pageErrors.length) throw new Error(`page errors: ${pageErrors.join('; ')}`);
  console.log('E2E PASS');
} catch (e) {
  await page.screenshot({ path: 'e2e-fail.png' }).catch(() => {});
  console.error('E2E FAIL:', e);
  process.exitCode = 1;
} finally {
  await browser.close();
}
