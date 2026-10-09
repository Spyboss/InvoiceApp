/**
 * Preview generator: writes sample report PDFs (with enough rows to span
 * multiple pages) so pagination + repeating table headers can be checked.
 * Usage: bun run scripts/report_preview.ts [outdir]
 */
import path from 'node:path';
import fs from 'node:fs';
import {
  buildReceivablesReportHtml,
  buildSalesReportHtml,
  buildStockReportHtml,
  renderPdf,
} from '../src/index';

const outDir = process.argv[2] ?? 'C:\\Users\\Gunawardana Motors\\Downloads';
const rs = (n: number) => `Rs. ${n.toLocaleString('en-US', { minimumFractionDigits: 2 })}`;

const names = [
  'Mudiyanselage Kasun Perera', 'Wickramasinghe Bandara', 'Herath Mudiyanselage Sunil',
  'Jayasuriya Arachchi Chaminda', 'Dissanayake Gedara Nimal', 'Kumara Hamine Dilrukshi',
  'Perera Munidasa Thilanka', 'Ranasinghe Appuhamilage Ruwan',
];
const models = ['APE Classic DX', 'APE Xtra LDX', 'Piaggio Ape 50', 'TVS King', 'Bajaj RE'];
const num = (s: string) => Number(s.replace(/^Rs\.\s*/, '').replace(/,/g, ''));

// --- sales: 60 rows -> multiple A4 pages
const salesRows = Array.from({ length: 60 }, (_, i) => {
  const price = 950000 + ((i * 37000) % 900000);
  const down = Math.round(price * (0.1 + ((i % 5) * 0.05)));
  return {
    no: String(i + 1).padStart(4, '0'),
    date: `2026-${String(((i % 9) + 1)).padStart(2, '0')}-${String((i % 28) + 1).padStart(2, '0')}`,
    type: i % 7 === 0 ? 'Proforma' : i % 3 === 0 ? 'Sales · Leasing' : 'Sales · Cash',
    customer: names[i % names.length],
    vehicle: models[i % models.length],
    price: rs(price),
    down: rs(down),
    balance: rs(price - down),
    status: i % 4 === 0 ? 'PART' : 'OPEN',
  };
});
const sum = (pick: (r: (typeof salesRows)[number]) => string) =>
  rs(salesRows.reduce((a, r) => a + num(pick(r)), 0));

const salesHtml = buildSalesReportHtml({
  dealer: 'Gunawardhana Enterprises, Beliatta Road, Tangalle',
  from: '2026-01-01',
  to: '2026-12-31',
  generatedAt: new Date().toLocaleString('en-GB'),
  rows: salesRows,
  totals: { count: salesRows.length, price: sum((r) => r.price), down: sum((r) => r.down), balance: sum((r) => r.balance) },
});

// --- stock: vehicles + parts
const statuses = ['AVAILABLE', 'AVAILABLE', 'RESERVED', 'SOLD', 'DELIVERED'];
const stockHtml = buildStockReportHtml({
  dealer: 'Gunawardhana Enterprises, Beliatta Road, Tangalle',
  generatedAt: new Date().toLocaleString('en-GB'),
  vehicleRows: Array.from({ length: 24 }, (_, i) => ({
    no: `MBX1129CDTF77${String(1200 + i)}`,
    model: models[i % models.length],
    type: 'PICKUP',
    yom: '2026',
    status: statuses[i % statuses.length],
    cost: rs(1450000 + i * 8000),
    price: rs(1750000 + i * 9500),
  })),
  vehicleSummary: [
    { status: 'AVAILABLE', count: 9, cost: rs(13200000), price: rs(15900000) },
    { status: 'RESERVED', count: 3, cost: rs(4400000), price: rs(5300000) },
    { status: 'SOLD', count: 8, cost: rs(11800000), price: rs(14200000) },
    { status: 'DELIVERED', count: 4, cost: rs(5900000), price: rs(7100000) },
  ],
  partRows: Array.from({ length: 22 }, (_, i) => ({
    sku: `PLG-${String(200 + i)}`,
    name: ['Brake Lining Set', 'Clutch Plate', 'Air Filter', 'Oil Filter', 'Head Lamp 12V', 'Tail Light Assy'][i % 6],
    qty: String((i * 3) % 14),
    reorder: '5',
    value: rs(1200 * ((i * 3) % 14) + 400),
  })),
  partValue: rs(486500),
  lowStock: 7,
});

// --- receivables: 30 open invoices
const recvRows = Array.from({ length: 30 }, (_, i) => {
  const price = 1200000 + ((i * 51000) % 700000);
  const balance = Math.round(price * (0.2 + ((i % 6) * 0.1)));
  return {
    no: String(100 + i).padStart(4, '0'),
    date: `2026-0${(i % 9) + 1}-15`,
    customer: names[i % names.length],
    price: rs(price),
    paid: rs(price - balance),
    balance: rs(balance),
    status: i % 5 === 0 ? 'PART' : 'OPEN',
  };
});
const recvHtml = buildReceivablesReportHtml({
  dealer: 'Gunawardhana Enterprises, Beliatta Road, Tangalle',
  generatedAt: new Date().toLocaleString('en-GB'),
  rows: recvRows,
  totals: {
    count: recvRows.length,
    balance: rs(recvRows.reduce((a, r) => a + num(r.balance), 0)),
  },
});

const out = (name: string, html: string) => {
  const file = path.join(outDir, name);
  const pdf = renderPdf(html);
  fs.writeFileSync(file, pdf);
  console.log(`${name}: ${pdf.length} bytes`);
};

out('Preview_SalesReport.pdf', salesHtml);
out('Preview_StockReport.pdf', stockHtml);
out('Preview_ReceivablesReport.pdf', recvHtml);
