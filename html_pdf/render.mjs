// HTML -> PDF renderer for the exact-match proforma invoice.
// Usage:  bun render.mjs
//   1. builds proforma.html from proforma.template.html + invoice data
//   2. prints it to PDF with headless Edge (same engine as Chrome)
//   3. calibrates every text element's CSS top against its real PDF baseline
//      (Blink rounds text origins, so a per-element table beats a per-font factor)
//   4. diffs text positions against the original sample PDF and renders a PNG
import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath, pathToFileURL } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));          // InvoiceApp/html_pdf
const APP = path.dirname(HERE);                                      // InvoiceApp
const OUT = path.join(HERE, 'out');
const PDFWORK = path.join(process.env.LOCALAPPDATA, 'Temp', 'opencode', 'pdfwork');
const EDGE = 'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe';
const SAMPLE = 'C:\\Users\\Gunawardana Motors\\Downloads\\Proforma Invoice CHANDRASENA.pdf';

// ---------------------------------------------------------------- invoice data
const D = {
  inv: 'S P G O 0 0 0 1 8 6',
  date: '06 /10/2026',
  customer: 'MUDIYANSELAGE KASUN PERERA',
  cust_addr: 'WEWURUKANNALA, DIKWELLA',
  nic: '199804702315',
  finance_company: "PEOPLE'S LEASING COMPANY PLC",
  finance_address: 'MATARA BRANCH, NO. 42, OLD GALL ROAD, MATARA',
  model: 'APE CLASSIC DX',
  type: 'PICKUP',
  color: 'STAR WHITE',
  yom: '2026',
  engine: 'A6F0187429',
  chassis: 'MBX1129CDTF771204',
  price: 'RS.1,795,000.00',
  down: 'RS.295,000.00',
  balance: 'RS .1,500,000.00',
  signatory: 'Yashodha Rashmi',
};

// per-element ascent factors: css_top = baseline_from_page_top - A * font_size
const ASC_PATH = path.join(HERE, 'ascent.json');
let ASC_FONT = { cal: 0.95, calb: 0.95, camb: 0.95, tah: 0.98, times: 0.9 };
let ASC_EL = fs.existsSync(ASC_PATH) ? JSON.parse(fs.readFileSync(ASC_PATH, 'utf8')) : [];

const CLASS = { cal: 'f-cal', calb: 'f-calb', camb: 'f-camb', tah: 'f-tah', times: 'f-times' };

// [x, baseline_from_top, size, font, text, center?]
const T = [
  // ---- header
  [448.1, 59, 28, 'tah', 'SINGER'],
  [227.4, 82.9, 14.05, 'cal', 'PROFORMA INVOICE'],
  // ---- row 1
  [37.9, 101.6, 12, 'cal', `Proforma Invoice. No.  ${D.inv}`],
  [295.3, 101.6, 12, 'cal', `DATE: ${D.date}`],
  // ---- row 2 left
  [37.9, 118, 11.05, 'cal', 'MANUFACTURER: INDIA'],
  [37.9, 133, 11.05, 'cal', 'PIAGGIO VEHICLES PVT LTD'],
  [37.9, 155.9, 11.05, 'cal', 'VIMAN NAGAR'],
  [37.9, 172, 11.05, 'cal', 'PUNE 411032,'],
  [37.9, 186.9, 11.05, 'cal', 'MAHARASHTRA'],
  [37.9, 206.1, 11.05, 'cal', 'INDIA'],
  // ---- row 2 right
  [295.4, 117.8, 12, 'cal', 'TO : THE MANAGER'],
  [322.6, 131, 12, 'cal', D.finance_company],
  [322.6, 144, 12, 'cal', D.finance_address],
  [292.6, 170.5, 11.05, 'cal', `O/A - ${D.customer}`],
  [301.1, 188.2, 11.05, 'cal', D.cust_addr],
  [306, 206.5, 11.05, 'cal', `ID -${D.nic}`],
  // ---- row 3 left (description)
  [40.7, 228.8, 12, 'cal', 'DESCRIPTION'],
  [37.9, 254.1, 11.05, 'cal', 'MAKE'], [109, 254.1, 11.05, 'cal', 'PIAGGIO'],
  [37.9, 267.4, 11.05, 'cal', 'MODEL'], [109, 268.5, 12, 'cal', D.model],
  [37.9, 289.3, 11.05, 'cal', 'TYPE'], [109, 291.3, 11.05, 'cal', D.type],
  [37.9, 302.2, 11.05, 'cal', 'COLOUR'], [109, 306.2, 11.05, 'cal', D.color],
  [37.9, 319.7, 11.05, 'cal', 'YOM'], [109, 321.2, 11.05, 'cal', D.yom],
  [37.9, 337.1, 11.05, 'cal', 'ENGINE NO'], [111.2, 337.7, 12, 'camb', D.engine],
  [37.9, 354.8, 11.05, 'cal', 'CHASSIS NO'], [113.5, 357.1, 12, 'camb', D.chassis],
  // ---- row 3 right (prices)
  [295.3, 254.1, 11.05, 'cal', 'SELLING PRICE'],
  [459.5, 254.1, 11.05, 'cal', D.price],
  [295.3, 268.9, 11.05, 'cal', 'ADVANCE PAYMENT'],
  [459.5, 268.9, 11.05, 'cal', D.down],
  [295.3, 302.2, 11.05, 'cal', 'BALANCE AMOUNT'],
  [459.1, 302.2, 11.05, 'calb', D.balance],
  [461.7, 328.1, 11.05, 'calb', 'RMV CHARGES FREE', true],
  // ---- row 4 left
  [37.9, 376.5, 12, 'cal', 'BRAND NEW\u2019 DIESEL THREE WHEELER'],
  [40.4, 392, 11.05, 'cal', '12V Self-start, four stroke, Air cooled Diesel engine.'],
  [37.9, 405.5, 11.05, 'cal', '435CC 8h.p.'],
  [37.9, 421.4, 12, 'cal', 'Warranty :'],
  [106.4, 421.4, 12, 'cal', '18 months or 25,000kms which ever'],
  [37.9, 436.5, 12, 'cal', 'come first on Engine & Gear Box'],
  [37.9, 453.4, 12, 'cal', 'Services'],
  [88.4, 453.4, 12, 'cal', ':'],
  [102.4, 453.4, 12, 'cal', '1 full free services will be provided'],
  // ---- row 4 right (remarks)
  [290, 376.3, 12, 'cal', 'REMARKS:'],
  [301.3, 392.6, 11.05, 'cal', 'Please note that the above price offered is on prevailing'],
  [295.3, 409.5, 11.05, 'cal', 'rates of exchange, import duties other Government levies'],
  [295.3, 426.3, 11.05, 'cal', '& any variations to the above will be adjusted in the final'],
  [295.3, 443.5, 11.05, 'cal', 'invoice.'],
  // ---- row 5 (VAT / dealer)
  [161.1, 478.5, 12, 'cal', 'VAT REG. NO. 124008026-7000', true],
  [290, 477.1, 11.05, 'times', 'DEALER - GUNAWARDHANA MOTORS'],
  // ---- terms block
  [30.4, 494.6, 11.05, 'calb', 'VALIDITY \u2013 14 DAYS'],
  [32, 517, 11.05, 'cal', 'PAYMENT TERMS:  All payments should be made in favor of \u2018SINGER (SRI'],
  [32, 531.5, 11.05, 'cal', 'LANKA) PLC, 112, Havelock Rd, Colombo 05, Sri Lanka.'],
  [31.6, 554.1, 11.05, 'cal', 'DELIVERY \u2013 Within 14 30 DAYS'],
  [48.8, 568.9, 9.95, 'cal', '1.'],
  [58.4, 569, 11.05, 'cal', 'Prices & Specifications subject to change without prior notice.'],
  [48.8, 584.2, 11.05, 'cal', '2.Goods being quoted are subject to prior sales and will only be issued if available at time of receiving your'],
  [48.8, 598, 11.05, 'cal', 'confirmed order.'],
  [48.8, 613.3, 9.95, 'cal', '2.'],
  [58.4, 613.3, 11.05, 'cal', 'Model of the vehicle has to be mentioned clearly on your purchase order.'],
  [48.8, 628.5, 9.95, 'cal', '3.'],
  [58.4, 628.5, 11.05, 'cal', 'Seller shall not be responsible for non-fulfillment or delayed performance of all or part of the order due,'],
  [58.6, 642.2, 11.05, 'cal', 'directly or indirectly to Act of good, Government order or restriction, war, threat of war, warlike conditions'],
  [58.6, 655.9, 11.05, 'cal', 'and hostilities'],
  // ---- signature block
  [48.8, 733.4, 9, 'calb', 'For ,'],
  [75.5, 733.4, 12, 'calb', D.signatory],
  [75, 749.1, 11.05, 'calb', 'Admin Executive'],
];

// table borders (reportlab drew 0.8pt strokes centred on the coordinates)
const HLINES = [89.45, 107.04, 216.89, 364.49, 466.49, 483.84];
const VLINES = [32.3, 289.85, 585.45];
const BOX_TOP = 89.45, BOX_BOT = 483.84;

const esc = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

function buildBody() {
  const parts = [];
  const tops = [];
  T.forEach(([x, b, size, font, text, center], i) => {
    const A = ASC_EL[i] ?? ASC_FONT[font];
    const top = b - A * size;
    tops.push(top);
    const centerStyle = center ? ';transform:translateX(-50%)' : '';
    const extra = font === 'tah' && text === 'SINGER' ? ';letter-spacing:4.25pt;color:#FF0000' : '';
    parts.push(
      `<div class="t ${CLASS[font]}" style="left:${x}pt;top:${top.toFixed(4)}pt;font-size:${size}pt${centerStyle}${extra}">${esc(text)}</div>`
    );
  });
  for (const y of HLINES) parts.push(`<div class="b" style="left:32.3pt;top:${(y - 0.4).toFixed(3)}pt;width:553.15pt;height:0.8pt"></div>`);
  const vh = BOX_BOT - BOX_TOP;
  for (const x of VLINES) parts.push(`<div class="b" style="left:${(x - 0.4).toFixed(3)}pt;top:${BOX_TOP}pt;width:0.8pt;height:${vh}pt"></div>`);
  parts.push(`<img class="img" src="../proforma_signature.png" style="left:51.72pt;top:668.16pt;width:76.08pt;height:51.96pt">`);
  parts.push(`<img class="img" src="../proforma_footer.png" style="left:151.32pt;top:754.2pt;width:253.8pt;height:24.72pt">`);
  return { html: parts.join('\n'), tops };
}

function writeHtml() {
  const { html, tops } = buildBody();
  const tpl = fs.readFileSync(path.join(HERE, 'proforma.template.html'), 'utf8');
  fs.writeFileSync(path.join(HERE, 'proforma.html'), tpl.replace('{{BODY}}', html));
  return tops;
}

function printPdf() {
  fs.mkdirSync(OUT, { recursive: true });
  const pdf = path.join(OUT, 'proforma_html.pdf');
  const profile = path.join(OUT, 'edge-profile');
  const r = spawnSync(EDGE, [
    '--headless', '--disable-gpu', '--no-first-run',
    `--user-data-dir=${profile}`,
    '--no-pdf-header-footer',
    `--print-to-pdf=${pdf}`,
    `file:///${path.join(HERE, 'proforma.html').replace(/\\/g, '/')}`,
  ], { encoding: 'utf8', timeout: 60000 });
  if (!fs.existsSync(pdf)) {
    console.error('Edge failed:', r.stderr || r.error);
    process.exit(1);
  }
  return pdf;
}

const loadPdfjs = () => import(pathToFileURL(path.join(PDFWORK, 'node_modules', 'pdfjs-dist', 'legacy', 'build', 'pdf.mjs')).href);

async function extract(pdfPath) {
  const { getDocument } = await loadPdfjs();
  const doc = await getDocument({ data: new Uint8Array(fs.readFileSync(pdfPath)) }).promise;
  const page = await doc.getPage(1);
  const tc = await page.getTextContent();
  return tc.items.filter(i => i.str.trim().length).map(i => ({
    str: i.str, x: Math.round(i.transform[4] * 100) / 100, y: Math.round(i.transform[5] * 100) / 100,
  }));
}

// find the PDF text piece belonging to manifest element i.
// Chrome splits long runs, so match on position (x for left-aligned, y always).
function findPiece(items, i) {
  const [x, b, size, font, text, center] = T[i];
  const fromTop = (it) => 792 - it.y;
  let cands = items.filter(it => Math.abs(fromTop(it) - b) <= 1.2);
  if (!center) cands = cands.filter(it => Math.abs(it.x - x) <= 1.2);
  if (!cands.length) return null;
  if (center && cands.length > 1) {            // prefer a piece that contains our text
    const t = text.slice(0, 12);
    const hit = cands.find(it => it.str.includes(t));
    if (hit) return hit;
  }
  // among remaining, prefer the piece whose text starts like ours (first piece)
  const norm = (s) => s.replace(/\s+/g, '');
  const hit = cands.find(it => norm(text).startsWith(norm(it.str)) || norm(it.str).startsWith(norm(text.slice(0, 20))));
  return hit || cands[0];
}

async function calibrate(items) {
  const next = [...ASC_EL];
  const errors = [];
  const missed = [];
  T.forEach(([x, b, size, font, text], i) => {
    const it = findPiece(items, i);
    if (!it) { missed.push(`${i}:${text.slice(0, 30)}`); return; }
    const measured = 792 - it.y;                        // baseline from page top
    errors.push({ i, d: measured - b, text: text.slice(0, 30) });
    const topUsed = T[i][1] - (ASC_EL[i] ?? ASC_FONT[font]) * size;
    next[i] = Math.round(((measured - topUsed) / size) * 10000) / 10000;
  });
  return { next, errors, missed };
}

async function diff(items) {
  const { getDocument } = await loadPdfjs();
  const doc = await getDocument({ data: new Uint8Array(fs.readFileSync(SAMPLE)) }).promise;
  const page = await doc.getPage(1);
  const orig = (await page.getTextContent()).items.map(i => ({
    str: i.str, x: Math.round(i.transform[4] * 10) / 10, y: Math.round(i.transform[5] * 10) / 10,
  }));
  const norm = (s) => s.replace(/\s+/g, '');
  const used = new Set();
  let matched = 0, maxD = 0; const shifts = [];
  for (const o of orig) {
    const s = o.str.trim();
    if (!s) continue;
    // our long runs may be split: accept a piece whose normalized text is a prefix
    let best = null, bd = 1e9;
    items.forEach((n, i) => {
      if (used.has(i)) return;
      const ns = norm(n.str), os = norm(s);
      if (!(ns === os || os.startsWith(ns) || ns.startsWith(os))) return;
      if (Math.abs(n.y - o.y) > 1.2) return;
      const d = Math.abs(n.x - o.x);
      if (d < bd) { bd = d; best = { i, n }; }
    });
    if (best) {
      used.add(best.i); matched++;
      const dx = Math.round((best.n.x - o.x) * 10) / 10;
      const dy = Math.round((best.n.y - o.y) * 10) / 10;
      maxD = Math.max(maxD, Math.abs(dx), Math.abs(dy));
      if (Math.abs(dx) > 1 || Math.abs(dy) > 1) shifts.push(`  dx=${dx} dy=${dy} |${s.slice(0, 60)}|`);
    }
  }
  console.log(`diff vs sample: matched ${matched}/${orig.filter(o => o.str.trim()).length}, max delta ${maxD}pt`);
  if (shifts.length) console.log(shifts.join('\n'));
  return maxD;
}

async function renderPng(pdfPath) {
  const { getDocument } = await loadPdfjs();
  const { createCanvas } = await import(pathToFileURL(path.join(PDFWORK, 'node_modules', '@napi-rs', 'canvas', 'index.js')).href);
  const doc = await getDocument({ data: new Uint8Array(fs.readFileSync(pdfPath)) }).promise;
  const page = await doc.getPage(1);
  const vp = page.getViewport({ scale: 2 });
  const canvas = createCanvas(vp.width, vp.height);
  await page.render({ canvasContext: canvas.getContext('2d'), viewport: vp }).promise;
  const out = path.join(OUT, 'proforma_html.png');
  fs.writeFileSync(out, canvas.toBuffer('image/png'));
  console.log('png:', out);
}

// ---------------------------------------------------------------- main loop
let tops = writeHtml();
for (let pass = 1; pass <= 5; pass++) {
  const pdf = printPdf();
  const items = await extract(pdf);
  const { next, errors, missed } = await calibrate(items);
  const worst = errors.reduce((m, e) => Math.max(m, Math.abs(e.d)), 0);
  console.log(`pass ${pass}: worst baseline error ${worst.toFixed(3)}pt, elements ${errors.length}/${T.length}, missed ${missed.length}${missed.length ? ' -> ' + missed.join(' | ') : ''}`);
  const changed = next.some((v, i) => v !== ASC_EL[i]);
  if (changed && pass < 5) {
    ASC_EL = next;
    fs.writeFileSync(ASC_PATH, JSON.stringify(ASC_EL));
    tops = writeHtml();
    continue;
  }
  if (changed) console.log('note: reached final pass with residual adjustment pending');
  // converged (or out of passes): verify against sample + render preview
  if (worst > 0.35) console.log('warning: not fully converged; residuals:',
    errors.filter(e => Math.abs(e.d) > 0.35).map(e => `${e.text}=${e.d.toFixed(2)}`).join(', '));
  await diff(items);
  await renderPng(pdf);
  console.log('pdf :', pdf);
  break;
}
