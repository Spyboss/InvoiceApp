/**
 * Sales invoice HTML (cash + leasing) — matched to the ReportLab Platypus
 * output of `generate_sales_pdf()` in web_app.py.
 *
 * Layout notes (measured from New_Sample_Sales_{Cash,Leasing}.pdf):
 *  - A4, frame margins 40/45/40/35; tables are CENTER hAlign in the frame
 *    (header 505pt, vehicle/payment 400pt, signature 480pt wide)
 *  - 18pt row pitch, text baseline = rowTop + 14
 *  - Helvetica == Arial metrics; baselines calibrated per element (ascent.json)
 */
import fs from 'node:fs';
import path from 'node:path';

export interface SalesData {
  invoiceNo: string;
  date: string;             // ISO, printed raw (parity with ReportLab)
  dealer: string;
  customer: string;
  custAddr: string;
  nic: string;
  model: string;
  engine: string;
  chassis: string;
  color: string;
  year: string;             // Manufactured Year (ReportLab uses current year)
  price: string;            // decimal string
  down: string;
  balance: string;          // decimal string
  isLeasing: boolean;
  delivery: string;
}

type FontCls = 'f-reg' | 'f-bold' | 'f-bi';
export interface Run {
  x: number;
  b: number;                // baseline (y from page top, as pdfjs measures)
  size: number;
  cls: FontCls;
  text: string;
  center?: number;          // center x instead of left x
  color?: 'gray' | 'lg';
}

const ASCENT = 0.905; // Arial hhea ascent — refined per element in ascent.json
const PAGE_W = 595.2756;
const CENTER = PAGE_W / 2;
const W = (s: string | number) =>
  'Rs. ' + Number(s).toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 });

const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
const assetDataUrl = (name: string) =>
  `data:image/png;base64,${fs.readFileSync(path.join(import.meta.dir, '..', 'assets', name)).toString('base64')}`;

/** Per-run baseline corrections keyed by `<x|c:center>:<text>` (variant-stable). */
function loadSalesAscent(): Record<string, number> {
  const p = path.join(import.meta.dir, '..', 'sales_ascent.json');
  if (!fs.existsSync(p)) return {};
  try { return JSON.parse(fs.readFileSync(p, 'utf8')); } catch { return {}; }
}

export function runKey(el: { x: number; center?: number; text: string }): string {
  return el.center !== undefined ? `c${el.center}:${el.text}` : `${el.x}:${el.text}`;
}

/** ReportLab/HTML wrap estimate for the delivery block (affects sign offset). */
function wrapLineCount(text: string, widthPt: number, size: number): number {
  const clean = text.replace(/\s+/g, ' ').trim();
  if (!clean) return 1;
  const maxChars = Math.max(1, Math.floor(widthPt / (size * 0.5))); // ~Helvetica avg glyph
  let lines = 1;
  let len = 0;
  for (const word of clean.split(' ')) {
    const w = word.length + (len > 0 ? 1 : 0);
    if (len + w > maxChars) { lines++; len = word.length; } else { len += w; }
  }
  return lines;
}

/** Baseline runs for one sales invoice — exported for the calibration harness. */
export function salesRuns(d: SalesData): Run[] {
  const runs: Run[] = [];
  const isLeasing = d.isLeasing;
  runs.push(
    { x: 49, b: 106, size: 9, cls: 'f-reg', text: 'Authorized Dealer', color: 'gray' },
    { x: CENTER, b: 131, size: 15, cls: 'f-bold', text: 'SALES INVOICE', center: CENTER },
    { x: CENTER, b: 160, size: 10, cls: 'f-reg', text: d.dealer, center: CENTER },
    // header table (x=45.14, cols 95/250/70/90, text = colX + 6 padding)
    { x: 396.14, b: 195, size: 10, cls: 'f-bold', text: 'Date:' },
    { x: 466.14, b: 195, size: 10, cls: 'f-bold', text: d.date },
    { x: 396.14, b: 213, size: 10, cls: 'f-bold', text: 'Invoice No:' },
    { x: 466.14, b: 213, size: 10, cls: 'f-bold', text: d.invoiceNo },
    { x: 51.14, b: 231, size: 10, cls: 'f-bold', text: 'Customer Name:' },
    { x: 146.14, b: 231, size: 10, cls: 'f-bold', text: d.customer },
    { x: 51.14, b: 249, size: 10, cls: 'f-bold', text: 'Address:' },
    { x: 146.14, b: 249, size: 10, cls: 'f-bold', text: d.custAddr },
    { x: 51.14, b: 267, size: 10, cls: 'f-bold', text: 'NIC:' },
    { x: 146.14, b: 267, size: 10, cls: 'f-bold', text: d.nic },
  );

  // ------------------------------------------------- vehicle details table
  runs.push({ x: 46, b: 312, size: 10, cls: 'f-bi', text: 'Vehicle Details' });
  const vRows: [string, string][] = [
    ['Model', d.model],
    ['Engine No', d.engine],
    ['Chassis No', d.chassis],
    ['Color', d.color],
    ['Engine Capacity', '435.6 cc'],
    ['Manufactured Year', d.year],
    ['Country of Origin', 'India'],
  ];
  vRows.forEach(([k, v], i) => {
    const b = 339 + i * 18;
    runs.push({ x: 103.64, b, size: 10, cls: 'f-reg', text: k });
    runs.push({ x: 253.64, b, size: 10, cls: 'f-reg', text: v });
  });

  // --------------------------------------------------- payment summary table
  runs.push({ x: 46, b: 490, size: 10, cls: 'f-bi', text: 'Payment Summary' });
  const rows: [string, string][] = isLeasing
    ? [['Total Price', W(d.price)], ['Down Payment', W(d.down)]]
    : [['Vehicle Price', W(d.price)], ['Total Payment', W(d.down)]];
  if (Number(d.balance) > 0) {
    rows.push([isLeasing ? 'Leasing Amount' : 'Balance', W(d.balance)]);
  }
  const payTop = 503;
  rows.forEach(([k, v], i) => {
    const b = 517 + i * 18;
    runs.push({ x: 103.64, b, size: 10, cls: 'f-bold', text: k });
    runs.push({ x: 303.64, b, size: 10, cls: 'f-bold', text: v });
  });
  const payBottom = payTop + rows.length * 18;

  // ------------------------------------------------------------- tail flow
  const delivery = d.delivery.replace(/\s+/g, ' ').trim();
  let tailTop = payBottom + 18; // Spacer(18) after the payment table
  if (delivery) {
    const lines = wrapLineCount(delivery, 515.28, 10);
    runs.push({ x: 46, b: tailTop + 11, size: 10, cls: 'f-bold', text: 'Delivery Address:' });
    runs.push({ x: 46, b: tailTop + 11 + 12, size: 10, cls: 'f-reg', text: delivery });
    tailTop += 12 * (1 + lines) + 20; // block + Spacer(20)
  }
  tailTop += 80; // Spacer(80)
  // signature table (480pt centered: col centers 177.64 / 417.64, row pitch 18)
  const dots = '.'.repeat(40);
  const signB = tailTop + 14;
  runs.push(
    { x: 177.64, b: signB, size: 10, cls: 'f-reg', text: dots, center: 177.64 },
    { x: 417.64, b: signB, size: 10, cls: 'f-reg', text: dots, center: 417.64 },
    { x: 177.64, b: signB + 18, size: 10, cls: 'f-bold', text: 'Customer Signature', center: 177.64 },
    { x: 417.64, b: signB + 18, size: 10, cls: 'f-bold', text: 'Authorized Signature & Stamp', center: 417.64 },
    { x: CENTER, b: signB + 63, size: 10, cls: 'f-bold', text: 'Thank you for your business! Come again!', center: CENTER },
  );

  // --------------------------------------------------------------- footers
  runs.push(
    { x: CENTER, b: 816.9, size: 9, cls: 'f-reg', text: 'Contact: 0778525428 / 0768525428 | Email: gunawardhanaenttangalle@gmail.com', center: CENTER, color: 'gray' },
    { x: CENTER, b: 826.9, size: 7, cls: 'f-reg', text: 'Software solution by UHADEV — uhadev.com', center: CENTER, color: 'lg' },
  );
  return runs;
}

export function buildSalesHtml(d: SalesData): string {
  const asc = loadSalesAscent();
  const runs = salesRuns(d);
  const parts: string[] = [];
  runs.forEach((el) => {
    const ascent = asc[runKey(el)] ?? ASCENT;
    const top = el.b - ascent * el.size;
    const pos = el.center !== undefined
      ? `left:${el.center}pt;transform:translateX(-50%)`
      : `left:${el.x}pt`;
    const color = el.color ? `;color:${el.color === 'gray' ? '#808080' : '#D3D3D3'}` : '';
    parts.push(
      `<div class="t ${el.cls}" style="${pos};top:${top.toFixed(4)}pt;font-size:${el.size}pt${color}">${esc(el.text)}</div>`,
    );
  });

  // ------------------------------------------------------------ table grids
  // vehicle table: x 97.64, w 400 (150/250), top 325, 7 rows
  parts.push(cellGrid(97.64, 325, [150, 250], 7, 'grid-g'));
  parts.push(cellBg(97.64, 325, 150, 7));
  // payment table: x 97.64, w 400 (200/200), top 503, one row per label run
  const payRows = runs.filter((r) => r.x === 103.64 && r.cls === 'f-bold').length;
  parts.push(cellGrid(97.64, 503, [200, 200], payRows, 'grid'));
  parts.push(cellBg(97.64, 503, 200, payRows));
  // logo (top margin 45, 90x40)
  parts.push(`<img class="img" src="${assetDataUrl('sales_logo.png')}" style="left:40pt;top:45pt;width:90pt;height:40pt">`);

  const tpl = fs.readFileSync(path.join(import.meta.dir, '..', 'sales.template.html'), 'utf8');
  return tpl.replace('{{BODY}}', parts.join('\n'));
}

/** Table grid: outer border + row lines + col lines (0.25pt). */
function cellGrid(x: number, y: number, cols: number[], rows: number, cls: string): string {
  const w = cols.reduce((a, b) => a + b, 0);
  const h = rows * 18;
  const out: string[] = [];
  out.push(`<div class="cell ${cls}" style="left:${x}pt;top:${y}pt;width:${w}pt;height:${h}pt"></div>`);
  for (let r = 1; r < rows; r++) {
    out.push(`<div class="cell" style="left:${x}pt;top:${y + r * 18}pt;width:${w}pt;height:0.25pt;background:#000"></div>`);
  }
  let cx = x;
  for (let c = 0; c < cols.length - 1; c++) {
    cx += cols[c]!;
    out.push(`<div class="cell" style="left:${cx}pt;top:${y}pt;width:0.25pt;height:${h}pt;background:${cls === 'grid-g' ? '#808080' : '#000'}"></div>`);
  }
  return out.join('\n');
}

/** Whitesmoke background for the label column. */
function cellBg(x: number, y: number, w: number, rows: number): string {
  return `<div class="cell bg" style="left:${x}pt;top:${y}pt;width:${w}pt;height:${rows * 18}pt"></div>`;
}
