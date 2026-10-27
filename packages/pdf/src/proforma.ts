/**
 * Proforma invoice — Singer (SRI LANKA) PLC. format.
 *
 * Ground truth is BIBILE 01.docx (word/document.xml). There is no rendered
 * PDF of that document anywhere on this machine, so the geometry below is
 * derived from the OOXML directly rather than measured off a reference render:
 *
 *   page 612x792pt, margins 36 / 72 / 22.5 / 72
 *   grid 566.5pt wide at tblInd -905 twips (-45.25pt), columns 257.45 + 309.05
 *   7 rows, trHeight atLeast 302/1637/233/1863/143/1269/302 twips
 *   every edge <w:tcBorders w:val="single" w:sz="4"> = 0.5pt, except the
 *   nil edges that merge rows 2+3 and 4+5
 *   <w:tblStyle w:val="TableGrid"/> carries NO borders and zero cell padding;
 *   padding is <w:tblPr><w:tblCellMar> left 108 / right 57 twips
 *
 * Word's "auto" line spacing multiplies the font's natural line box, which CSS
 * `line-height` does not — so every height is written out in pt from the
 * measured Calibri ratio instead of a unitless multiplier.
 */
import fs from 'node:fs';
import path from 'node:path';
import { wordmarkSvg } from './proforma.wordmark';

export interface ProformaData {
  invoiceNo: string;          // printed as-is, e.g. "0001"
  date: string;               // "DD/MM/YYYY"
  customer: string;
  nic: string;
  contact: string;            // <w:t>07</w:t><w:t>88585327</w:t> on the sample
  financeCompany: string;
  financeAddress: string;     // leasing company's address, not the customer's
  make: string;
  model: string;
  type: string;
  color: string;
  yom: string;
  engine: string;
  chassis: string;
  price: string;              // "980000" -> printed "LKR 980000/-"
  down: string;
  balance: string;
  vatNo: string;
  remarks: string;
}

/** Calibri's natural line box / font-size, measured in Edge — see header. */
const CAL_LINE = 1.2225;
const TWIP = 20;

/** Carried over verbatim from the BIBILE 01.docx body. */
export const PROFORMA_VAT_NO = '124008026-7000';
export const PROFORMA_REMARKS =
  'Please note that the above price offered is on prevailing rates of exchange, import duties, ' +
  'other Government levies & any variations to the above will be adjusted in the final invoice.';

const tw = (t: number) => t / TWIP;
const lh = (size: number, line = 240) => +(size * CAL_LINE * (line / 240)).toFixed(4);
const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

const ASSET = (name: string) => path.join(import.meta.dir, '..', 'assets', name);

function assetDataUrl(name: string): string {
  const mime = name.endsWith('.jpg') || name.endsWith('.jpeg') ? 'jpeg' : 'png';
  return `data:image/${mime};base64,${fs.readFileSync(ASSET(name)).toString('base64')}`;
}

type Para = {
  size?: number;              // w:sz — Normal style is sz=20 (10pt)
  line?: number;              // w:spacing/@w:line, 240 = single
  after?: number;             // w:spacing/@w:after, twips
  bold?: boolean;
  left?: number;              // w:ind/@w:left, twips
  right?: number;             // w:ind/@w:right, twips
  firstLine?: number;         // w:ind/@w:firstLine, twips
  jc?: 'center' | 'right' | 'both';
};

/** One <w:p>. Indents use padding so wrapped lines stay at the left indent —
 *  these paragraphs carry no w:hanging, so Word gives them no hanging indent. */
function para(html: string, s: Para = {}): string {
  const size = s.size ?? 10;
  const css = [`font-size:${size}pt`, `line-height:${lh(size, s.line ?? 240)}pt`];
  if (s.after) css.push(`margin-bottom:${tw(s.after)}pt`);
  if (s.left) css.push(`padding-left:${tw(s.left)}pt`);
  if (s.right) css.push(`padding-right:${tw(s.right)}pt`);
  if (s.firstLine) css.push(`text-indent:${tw(s.firstLine)}pt`);
  if (s.jc) css.push(`text-align:${s.jc}`);
  if (s.bold) css.push('font-weight:700');
  // a <w:p> with no runs still occupies a full line in Word — a bare empty div
  // has no line box in CSS, so force one
  return `<div class="p" style="${css.join(';')}">${html || '&#160;'}</div>`;
}

/** Word draws subscript at ~65% of the specified size, lowered. CSS
 *  vertical-align:sub only lowers the baseline, so the size has to shrink here. */
const SUB_SCALE = 0.65;
const run = (text: string, size = 10, bold = false, sub = false) =>
  `<span style="font-size:${+(size * (sub ? SUB_SCALE : 1)).toFixed(2)}pt${bold ? ';font-weight:700' : ''}${sub ? ';vertical-align:sub' : ''}">${esc(text)}</span>`;

const COL0 = 257.45;
const COL1 = 309.05;
const td = (html: string, w: number, cls = '') =>
  `<td class="${cls}" style="width:${w}pt">${html}</td>`;
/** trHeight is atLeast, which is exactly how CSS treats a row height. */
const tr = (h: number, html: string) => `<tr style="height:${h}pt">${html}</tr>`;

/** List marker sits at the 0.5pt indent; the tab that follows lands on the
 *  default 0.5in tab stop = 36pt from the text column. */
const LIST_TAB = 36;
// The docx list has no hanging indent and no explicit tab stop, so Word puts the
// number at the 0.5pt indent and the body on the 36pt default stop; wrapped lines
// fall back to the 0.5pt indent. Making the marker a fixed-width inline-block
// pins the body to the tab stop no matter how wide the digits are.
const num = (i: number, text: string, bold = false) =>
  para(
    `<span style="display:inline-block;width:${(LIST_TAB - tw(10)).toFixed(2)}pt">${i}.</span>${text}`,
    { size: 11, line: 246, after: 30, left: 10, bold },
  );

export function buildProformaHtml(d: ProformaData): string {
  const parts: string[] = [];

  // wordmark — v:group of 12 vector shapes, page-anchored (see proforma.wordmark.ts)
  parts.push(
    `<img class="wm" src="${wordmarkInline()}" style="left:433.2pt;top:21.45pt;width:154.45pt;height:24.6pt">`,
  );

  const flow: string[] = [];

  // p0 (empty, anchors the wordmark) + p1 (title) — both sz=32, line 276
  flow.push(para('', { size: 16, line: 276, after: 4, jc: 'center' }));
  flow.push(para(run('PROFORMA INVOICE', 16), { size: 16, line: 276, after: 4, jc: 'center' }));

  // ---------------------------------------------------------------- grid
  const rows: string[] = [];

  // row 0 — 14pt, line 276
  rows.push(
    tr(15.1,
      td(para(run(`Proforma Invoice. No.  ${d.invoiceNo}`, 14), { size: 14, line: 276 }), COL0) +
      td(para(run(`DATE:         ${d.date}       `, 14), { size: 14, line: 276 }), COL1)),
  );

  // row 1 left — Normal 10pt, after 32; right — after 35, inheriting Normal's
  // ind left=355 twips, so it sits 17.75pt in while the left cell is flush.
  const r1l = [
    para('', { after: 32 }),
    para(run(`Customer Name     - ${d.customer} `), { after: 32 }),
    para('', { after: 32 }),
    para(run(`Customer NIC- ${d.nic}`), { after: 32 }),
    para('', { after: 32 }),
    para(run(`Contact no     - ${d.contact}`), { after: 32 }),
  ].join('');
  const r1r = [
    para(run(`Leasing company- ${d.financeCompany} `), { after: 35, left: 355 }),
    para('', { after: 35, left: 355 }),
    para(run(`Address- ${d.financeAddress} `), { after: 35, left: 355 }),
    para(run('     '), { after: 35, left: 355 }),
    para('', { after: 0, left: 355 }),
    para('', { after: 2, left: 355 }),
    para(run('            '), { after: 2, line: 240 }),
  ].join('');
  rows.push(tr(81.85, td(r1l, COL0) + td(r1r, COL1)));

  // row 2 — merged into row 3 below it (bottom border nil)
  rows.push(
    tr(11.65,
      td(para(run(' DESCRIPTION  ', 14), { size: 14, line: 276 }), COL0, 'nb') +
      td(para(run(' ', 12), { size: 12, line: 276 }), COL1, 'nb')),
  );

  // row 3 left — 11pt spec block; chassis value is 16pt subscript on the docx.
  // line spacing is computed from the LARGEST run on the line, so these
  // paragraphs are declared at 11pt even though Normal is 10pt.
  const r3l = [
    para(run(`MAKE                      - ${d.make}`, 11), { size: 11, after: 0 }),
    para(run(`MODEL                    - ${d.model}`, 11), { size: 11, after: 0 }),
    para(run(`TYPE                       - ${d.type}`, 11), { size: 11, after: 0 }),
    para(run(`COLOUR                  - ${d.color}`, 11), { size: 11, after: 32 }),
    para(run(`EN/MOTOR NO       - ${d.engine}`, 11), { size: 11, after: 32 }),
    para('', { size: 11, after: 32 }),
    para(
      run('CHASSIS NO', 11) + run('        ', 16, false, true) + run(' -', 16, false, true) + run(d.chassis, 16, false, true),
      { size: 11, after: 33 },
    ),
    para(run(`YOM                      - ${d.yom}`, 11), { size: 11, after: 33 }),
    para('', { size: 11, after: 33 }),
  ].join('');
  const r3r = [
    para('', { line: 246 }),
    para(run(`Selling price         - LKR ${d.price}/-`), { line: 246 }),
    para(run(`Cash Advance      -LKR ${d.down}/-`), { line: 246 }),
    para(run(`Balance Amount -LKR ${d.balance}/-`), { line: 246 }),
  ].join('');
  rows.push(tr(93.15, td(r3l, COL0, 'nt') + td(r3r, COL1, 'nt')));

  // row 4 — merged into row 5 below it (bottom border nil)
  rows.push(
    tr(7.15,
      td(para(run('  ', 14), { size: 14, line: 276 }), COL0, 'nb') +
      td(para(run(' ', 14), { size: 14, line: 276 }), COL1, 'nb')),
  );

  // row 5 left — merged into row 4 above (top border nil); right — REMARKS
  rows.push(
    tr(63.45,
      td(para('', { after: 32 }) + para('', { after: 36, line: 248 }), COL0, 'nt') +
      td(
        para(run('REMARKS:  '), { after: 36 }) +
          para(run(d.remarks), { line: 276, firstLine: 120, jc: 'both' }),
        COL1,
        'nt',
      )),
  );

  // row 6
  rows.push(
    tr(15.1,
      td(para(run(`                 VAT REG. NO. ${d.vatNo} `, 14), { size: 14, line: 276 }), COL0) +
      td(para(run(' ', 14), { size: 14, line: 276 }), COL1)),
  );

  flow.push(`<table class="g">${rows.join('')}</table>`);

  // ------------------------------------------------------------- terms
  // Body paragraphs inherit Normal (after=30 twips, line=246), not the
  // TableGrid spacing the cells use.
  flow.push(para(run(' ', 9), { size: 9, line: 240, after: 31 }));
  flow.push(para(run('VALIDITY \u2013 07 DAYS    ', 11, true), { size: 11, line: 246, after: 30, left: 10, bold: true }));
  flow.push(
    para(
      run('PAYMENT TERMS:  All payments should be made in favor of \u2018SINGER (SRI LANKA) PLC, ', 11) +
        run('112, Havelock Rd, Colombo 06, Sri Lanka. ', 12),
      { size: 12, line: 259, after: 30, left: 10, right: 3610 },
    ),
  );
  flow.push(
    para(
      run('DELIVERY \u2013 ', 12) + run('Within ', 11) + run('14 ', 12) + run('30 ', 11) + run('DAYS ', 12),
      { size: 12, line: 240, after: 32, left: 10 },
    ),
  );

  // numId=1 -> abstractNum 0 -> decimal "%1.", no hanging indent
  const terms = [
    run('Prices & Specifications subject to change without prior notice. '),
    run('RMV free of Charge', 10, true),
    run('Goods being quoted are subject to prior sales and will only be issued if available at time of receiving your confirmed order. '),
    run('Model of the vehicle has to be mentioned clearly on your purchase order.   '),
    run('Seller shall not be responsible for non-fulfillment or delayed performance of all or part of the order due, directly or indirectly to Act of good, Government order or restriction, war, threat of war, warlike conditions and hostilities.  '),
  ];
  terms.forEach((t, i) => flow.push(num(i + 1, t, i === 1)));

  flow.push(para('', { line: 246, after: 30, left: 10 }));
  flow.push(para('', { line: 246, after: 30, left: 0 }));
  flow.push(para(run('-------------------------'), { line: 246, after: 30, left: 0 }));
  flow.push(para(run('Authorized dealer                              '), { line: 246, after: 30, left: 0 }));

  // letterhead — anchored to this paragraph, H relativeFrom=page 112.5pt,
  // V relativeFrom=paragraph +11.8pt, shown at 323.25x36pt
  flow.push(
    `<div style="position:relative"><img class="lh" src="${assetDataUrl('proforma_letterhead.jpeg')}" style="left:40.5pt;top:11.8pt;width:323.25pt;height:36pt"></div>`,
  );

  parts.push(`<div class="flow">${flow.join('')}</div>`);

  const tpl = fs.readFileSync(path.join(import.meta.dir, '..', 'proforma.template.html'), 'utf8');
  return tpl.replace('{{BODY}}', parts.join('\n'));
}

let cachedWordmark = '';
function wordmarkInline(): string {
  if (!cachedWordmark) {
    // bake to a self-contained data URL so the img is layout-independent
    cachedWordmark = `data:image/svg+xml;base64,${Buffer.from(wordmarkSvg(), 'utf8').toString('base64')}`;
  }
  return cachedWordmark;
}
