/**
 * Sales invoice calibration loop.
 *
 * Renders the HTML sales invoice (cash + leasing), extracts text baselines
 * with pdf.js, matches them against the ReportLab ground truth
 * (reference/sales_{cash,leasing}.json), and iterates sales_ascent.json
 * corrections until every baseline is within ~1pt of the original document.
 *
 * Usage: bun run scripts/calibrate_sales.ts [iterations]
 */
import fs from 'node:fs';
import path from 'node:path';
import { getDocument } from 'pdfjs-dist/legacy/build/pdf.mjs';
import { buildSalesHtml, runKey, salesRuns, type SalesData } from '../src/sales';
import { renderPdf } from '../src/render';

interface Item {
  str: string;
  x: number;
  y: number;
  w: number | null;
}

const r1 = (n: number) => Math.round(n * 10) / 10;
const REF = path.join(import.meta.dir, '..', 'reference');
const ASCENT_PATH = path.join(import.meta.dir, '..', 'sales_ascent.json');
const BASE_ASCENT = 0.905;

async function extractItems(pdf: Uint8Array): Promise<Item[]> {
  const doc = await getDocument({ data: pdf, useSystemFonts: true }).promise;
  const page = await doc.getPage(1);
  const vp = page.getViewport({ scale: 1 });
  const tc = await page.getTextContent();
  const items: Item[] = [];
  for (const it of tc.items as unknown as Array<{ str?: string; transform: number[]; width?: number }>) {
    if (typeof it.str !== 'string' || !it.str) continue;
    const t = it.transform;
    items.push({
      str: it.str,
      x: r1(t[4]!),
      y: r1(vp.height - t[5]!),
      w: it.width ? r1(it.width) : null,
    });
  }
  return items;
}

function loadGt(name: string): Item[] {
  const buf = fs.readFileSync(path.join(REF, name));
  // PowerShell `>` redirection writes UTF-16LE with BOM
  const raw = buf[0] === 0xff && buf[1] === 0xfe
    ? buf.subarray(2).toString('utf16le')
    : buf.toString('utf8');
  const items: Item[] = [];
  for (const line of raw.split(/\r?\n/)) {
    if (!line.startsWith('{')) continue;
    const o = JSON.parse(line);
    if (!o.str) continue;
    items.push({ str: o.str, x: o.x, y: o.y, w: o.w });
  }
  return items;
}

const sameText = (a: string, b: string) => a === b || (a.includes(b) && b.length >= 8) || (b.includes(a) && a.length >= 8);

interface Variant {
  name: string;
  data: SalesData;
}

const base = {
  invoiceNo: '000186',
  date: '2026-10-06',
  dealer: 'Gunawardhana Enterprises, Beliatta Road, Tangalle',
  customer: 'Mudiyanselage Kasun Perera',
  custAddr: 'Wewurukannala, Dikwella',
  nic: '199804702315',
  model: 'APE Classic DX',
  engine: 'A6F0187429',
  chassis: 'MBX1129CDTF771204',
  color: 'Star White',
  year: '2026',
  price: '1795000',
};

const VARIANTS: Variant[] = [
  { name: 'sales_cash', data: { ...base, down: '1795000', balance: '0', isLeasing: false, delivery: '' } },
  { name: 'sales_leasing', data: { ...base, down: '295000', balance: '1500000', isLeasing: true, delivery: 'No. 14, Lake Road, Matara' } },
];

const iterations = Number(process.argv[2] ?? 4);
let ascent: Record<string, number> = fs.existsSync(ASCENT_PATH)
  ? JSON.parse(fs.readFileSync(ASCENT_PATH, 'utf8'))
  : {};

for (let iter = 0; iter < iterations; iter++) {
  const corrections: Record<string, { sum: number; n: number }> = {};
  let worst = 0;
  let worstDesc = '';

  for (const v of VARIANTS) {
    const gt = loadGt(`${v.name.replace('sales_', 'sales_')}.json`);
    const runs = salesRuns(v.data);
    const items = await extractItems(renderPdf(buildSalesHtml(v.data)));

    // match each run to its ground-truth item
    const usedGt = new Set<number>();
    for (const run of runs) {
      const expectedX = run.center !== undefined
        ? run.center // left edge derived below from gt width
        : run.x;
      const key = runKey(run);

      // candidates: same text, plausible x
      let bestGt = -1;
      let bestScore = Infinity;
      for (let gi = 0; gi < gt.length; gi++) {
        const g = gt[gi]!;
        if (usedGt.has(gi)) continue;
        if (g.str !== run.text && !(run.text.includes(g.str) && g.str.length >= 8)) continue;
        const left = run.center !== undefined && g.w ? run.center - g.w / 2 : expectedX;
        const score = Math.abs(g.x - left);
        if (score < bestScore) { bestScore = score; bestGt = gi; }
      }
      if (bestGt < 0 || bestScore > 3) {
        console.log(`  [${v.name}] NO MATCH for "${run.text.slice(0, 40)}" @${run.x}`);
        continue;
      }
      usedGt.add(bestGt);
      const g = gt[bestGt]!;

      // nearest rendered item to that ground-truth position (x AND y — the
      // two signature dot-runs share text and baseline, only x differs)
      let bestIt: Item | null = null;
      let bestDist = Infinity;
      for (const it of items) {
        if (!sameText(it.str, g.str)) continue;
        const score = Math.abs(it.y - g.y) + Math.abs(it.x - g.x);
        if (score < bestDist) { bestDist = score; bestIt = it; }
      }
      if (!bestIt) {
        console.log(`  [${v.name}] NOT RENDERED: "${g.str.slice(0, 40)}"`);
        continue;
      }

      const dy = bestIt.y - g.y;
      const dx = bestIt.x - g.x;
      const absDy = Math.abs(dy);
      if (absDy > worst) { worst = absDy; worstDesc = `"${g.str.slice(0, 35)}" [${v.name}] dy=${dy.toFixed(2)}`; }
      if (Math.abs(dx) > 1.5) console.log(`  [${v.name}] X-SHIFT ${dx.toFixed(1)}pt "${g.str.slice(0, 30)}"`);
      const c = (corrections[key] ??= { sum: 0, n: 0 });
      const used = ascent[key] ?? BASE_ASCENT; // what this render actually used
      c.sum += used + dy / run.size;           // dy>0 (too low) -> larger ascent -> smaller top
      c.n += 1;
    }
  }

  let changed = false;
  for (const [key, c] of Object.entries(corrections)) {
    const next = Math.round((c.sum / c.n) * 10000) / 10000;
    if (ascent[key] !== next) { ascent[key] = next; changed = true; }
  }
  fs.writeFileSync(ASCENT_PATH, JSON.stringify(ascent, null, 1));
  console.log(`iter ${iter + 1}: worst |dy| = ${worst.toFixed(2)}pt  (${worstDesc})  keys=${Object.keys(ascent).length}`);
  if (worst <= 1.0) { console.log('CONVERGED'); break; }
  if (!changed) { console.log('no further change possible (Blink snapping floor)'); break; }
}
