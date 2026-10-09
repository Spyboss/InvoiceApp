/**
 * Proforma invoice HTML — pixel-matched to the Singer sample document.
 * Coordinates were measured from the original PDF; baselines are corrected
 * with the calibrated ascent table (ascent.json, regenerated per machine by
 * `bun run reference-render.mjs`).
 */
import fs from 'node:fs';
import path from 'node:path';

export interface ProformaData {
  invoiceNo: string;        // full display number, e.g. "S P G O 0 0 0 1 8 6"
  date: string;             // "DD /MM/YYYY"
  customer: string;         // upper-cased name
  custAddr: string;
  nic: string;
  financeCompany: string;
  financeAddress: string;
  model: string;
  type: string;
  color: string;
  yom: string;
  engine: string;
  chassis: string;
  price: string;            // "RS.1,795,000.00"
  down: string;             // "RS.295,000.00"
  balance: string;          // "RS .1,500,000.00"
  signatory: string;
}

type Font = 'cal' | 'calb' | 'camb' | 'tah' | 'times';
type Run = { x: number; b: number; size: number; font: Font; text: string; center?: boolean };

const CLASS: Record<Font, string> = {
  cal: 'f-cal', calb: 'f-calb', camb: 'f-camb', tah: 'f-tah', times: 'f-times',
};

const ASSET = (name: string) => path.join(import.meta.dir, '..', 'assets', name);

function assetDataUrl(name: string): string {
  return `data:image/png;base64,${fs.readFileSync(ASSET(name)).toString('base64')}`;
}

function loadAscent(): Record<string, number> {
  const p = path.join(import.meta.dir, '..', 'ascent.json');
  if (!fs.existsSync(p)) return {};
  try { return JSON.parse(fs.readFileSync(p, 'utf8')); } catch { return {}; }
}

const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

export function buildProformaHtml(d: ProformaData): string {
  const ASC_EL: number[] = loadAscent() as unknown as number[];
  const ASC_FONT: Record<Font, number> = { cal: 0.95, calb: 0.95, camb: 0.95, tah: 0.98, times: 0.9 };

  const T: Run[] = [
    // header
    { x: 448.1, b: 59, size: 28, font: 'tah', text: 'SINGER' },
    { x: 227.4, b: 82.9, size: 14.05, font: 'cal', text: 'PROFORMA INVOICE' },
    // row 1
    { x: 37.9, b: 101.6, size: 12, font: 'cal', text: `Proforma Invoice. No.  ${d.invoiceNo}` },
    { x: 295.3, b: 101.6, size: 12, font: 'cal', text: `DATE: ${d.date}` },
    // row 2 left
    { x: 37.9, b: 118, size: 11.05, font: 'cal', text: 'MANUFACTURER: INDIA' },
    { x: 37.9, b: 133, size: 11.05, font: 'cal', text: 'PIAGGIO VEHICLES PVT LTD' },
    { x: 37.9, b: 155.9, size: 11.05, font: 'cal', text: 'VIMAN NAGAR' },
    { x: 37.9, b: 172, size: 11.05, font: 'cal', text: 'PUNE 411032,' },
    { x: 37.9, b: 186.9, size: 11.05, font: 'cal', text: 'MAHARASHTRA' },
    { x: 37.9, b: 206.1, size: 11.05, font: 'cal', text: 'INDIA' },
    // row 2 right
    { x: 295.4, b: 117.8, size: 12, font: 'cal', text: 'TO : THE MANAGER' },
    { x: 322.6, b: 131, size: 12, font: 'cal', text: d.financeCompany },
    { x: 322.6, b: 144, size: 12, font: 'cal', text: d.financeAddress },
    { x: 292.6, b: 170.5, size: 11.05, font: 'cal', text: `O/A - ${d.customer}` },
    { x: 301.1, b: 188.2, size: 11.05, font: 'cal', text: d.custAddr },
    { x: 306, b: 206.5, size: 11.05, font: 'cal', text: `ID -${d.nic}` },
    // row 3 left — description
    { x: 40.7, b: 228.8, size: 12, font: 'cal', text: 'DESCRIPTION' },
    { x: 37.9, b: 254.1, size: 11.05, font: 'cal', text: 'MAKE' },
    { x: 109, b: 254.1, size: 11.05, font: 'cal', text: 'PIAGGIO' },
    { x: 37.9, b: 267.4, size: 11.05, font: 'cal', text: 'MODEL' },
    { x: 109, b: 268.5, size: 12, font: 'cal', text: d.model },
    { x: 37.9, b: 289.3, size: 11.05, font: 'cal', text: 'TYPE' },
    { x: 109, b: 291.3, size: 11.05, font: 'cal', text: d.type },
    { x: 37.9, b: 302.2, size: 11.05, font: 'cal', text: 'COLOUR' },
    { x: 109, b: 306.2, size: 11.05, font: 'cal', text: d.color },
    { x: 37.9, b: 319.7, size: 11.05, font: 'cal', text: 'YOM' },
    { x: 109, b: 321.2, size: 11.05, font: 'cal', text: d.yom },
    { x: 37.9, b: 337.1, size: 11.05, font: 'cal', text: 'ENGINE NO' },
    { x: 111.2, b: 337.7, size: 12, font: 'camb', text: d.engine },
    { x: 37.9, b: 354.8, size: 11.05, font: 'cal', text: 'CHASSIS NO' },
    { x: 113.5, b: 357.1, size: 12, font: 'camb', text: d.chassis },
    // row 3 right — prices
    { x: 295.3, b: 254.1, size: 11.05, font: 'cal', text: 'SELLING PRICE' },
    { x: 459.5, b: 254.1, size: 11.05, font: 'cal', text: d.price },
    { x: 295.3, b: 268.9, size: 11.05, font: 'cal', text: 'ADVANCE PAYMENT' },
    { x: 459.5, b: 268.9, size: 11.05, font: 'cal', text: d.down },
    { x: 295.3, b: 302.2, size: 11.05, font: 'cal', text: 'BALANCE AMOUNT' },
    { x: 459.1, b: 302.2, size: 11.05, font: 'calb', text: d.balance },
    { x: 461.7, b: 328.1, size: 11.05, font: 'calb', text: 'RMV CHARGES FREE', center: true },
    // row 4 left
    { x: 37.9, b: 376.5, size: 12, font: 'cal', text: 'BRAND NEW\u2019 DIESEL THREE WHEELER' },
    { x: 40.4, b: 392, size: 11.05, font: 'cal', text: '12V Self-start, four stroke, Air cooled Diesel engine.' },
    { x: 37.9, b: 405.5, size: 11.05, font: 'cal', text: '435CC 8h.p.' },
    { x: 37.9, b: 421.4, size: 12, font: 'cal', text: 'Warranty :' },
    { x: 106.4, b: 421.4, size: 12, font: 'cal', text: '18 months or 25,000kms which ever' },
    { x: 37.9, b: 436.5, size: 12, font: 'cal', text: 'come first on Engine & Gear Box' },
    { x: 37.9, b: 453.4, size: 12, font: 'cal', text: 'Services' },
    { x: 88.4, b: 453.4, size: 12, font: 'cal', text: ':' },
    { x: 102.4, b: 453.4, size: 12, font: 'cal', text: '1 full free services will be provided' },
    // row 4 right — remarks
    { x: 290, b: 376.3, size: 12, font: 'cal', text: 'REMARKS:' },
    { x: 301.3, b: 392.6, size: 11.05, font: 'cal', text: 'Please note that the above price offered is on prevailing' },
    { x: 295.3, b: 409.5, size: 11.05, font: 'cal', text: 'rates of exchange, import duties other Government levies' },
    { x: 295.3, b: 426.3, size: 11.05, font: 'cal', text: '& any variations to the above will be adjusted in the final' },
    { x: 295.3, b: 443.5, size: 11.05, font: 'cal', text: 'invoice.' },
    // row 5 — VAT / dealer
    { x: 161.1, b: 478.5, size: 12, font: 'cal', text: 'VAT REG. NO. 124008026-7000', center: true },
    { x: 290, b: 477.1, size: 11.05, font: 'times', text: 'DEALER - GUNAWARDHANA MOTORS' },
    // terms
    { x: 30.4, b: 494.6, size: 11.05, font: 'calb', text: 'VALIDITY \u2013 14 DAYS' },
    { x: 32, b: 517, size: 11.05, font: 'cal', text: 'PAYMENT TERMS:  All payments should be made in favor of \u2018SINGER (SRI' },
    { x: 32, b: 531.5, size: 11.05, font: 'cal', text: 'LANKA) PLC, 112, Havelock Rd, Colombo 05, Sri Lanka.' },
    { x: 31.6, b: 554.1, size: 11.05, font: 'cal', text: 'DELIVERY \u2013 Within 14 30 DAYS' },
    { x: 48.8, b: 568.9, size: 9.95, font: 'cal', text: '1.' },
    { x: 58.4, b: 569, size: 11.05, font: 'cal', text: 'Prices & Specifications subject to change without prior notice.' },
    { x: 48.8, b: 584.2, size: 11.05, font: 'cal', text: '2.Goods being quoted are subject to prior sales and will only be issued if available at time of receiving your' },
    { x: 48.8, b: 598, size: 11.05, font: 'cal', text: 'confirmed order.' },
    { x: 48.8, b: 613.3, size: 9.95, font: 'cal', text: '2.' },
    { x: 58.4, b: 613.3, size: 11.05, font: 'cal', text: 'Model of the vehicle has to be mentioned clearly on your purchase order.' },
    { x: 48.8, b: 628.5, size: 9.95, font: 'cal', text: '3.' },
    { x: 58.4, b: 628.5, size: 11.05, font: 'cal', text: 'Seller shall not be responsible for non-fulfillment or delayed performance of all or part of the order due,' },
    { x: 58.6, b: 642.2, size: 11.05, font: 'cal', text: 'directly or indirectly to Act of good, Government order or restriction, war, threat of war, warlike conditions' },
    { x: 58.6, b: 655.9, size: 11.05, font: 'cal', text: 'and hostilities' },
    // signature block
    { x: 48.8, b: 733.4, size: 9, font: 'calb', text: 'For ,' },
    { x: 75.5, b: 733.4, size: 12, font: 'calb', text: d.signatory },
    { x: 75, b: 749.1, size: 11.05, font: 'calb', text: 'Admin Executive' },
  ];

  const parts: string[] = [];
  T.forEach((el, i) => {
    const A = ASC_EL[i] ?? ASC_FONT[el.font];
    const top = el.b - A * el.size;
    const centerStyle = el.center ? ';transform:translateX(-50%)' : '';
    const extra = el.font === 'tah' && el.text === 'SINGER' ? ';letter-spacing:4.25pt;color:#FF0000' : '';
    parts.push(
      `<div class="t ${CLASS[el.font]}" style="left:${el.x}pt;top:${top.toFixed(4)}pt;font-size:${el.size}pt${centerStyle}${extra}">${esc(el.text)}</div>`,
    );
  });
  // table borders (0.8pt strokes centred on the coordinates)
  for (const y of [89.45, 107.04, 216.89, 364.49, 466.49, 483.84]) {
    parts.push(`<div class="b" style="left:32.3pt;top:${(y - 0.4).toFixed(3)}pt;width:553.15pt;height:0.8pt"></div>`);
  }
  for (const x of [32.3, 289.85, 585.45]) {
    parts.push(`<div class="b" style="left:${(x - 0.4).toFixed(3)}pt;top:89.45pt;width:0.8pt;height:394.39pt"></div>`);
  }
  parts.push(`<img class="img" src="${assetDataUrl('proforma_signature.png')}" style="left:51.72pt;top:668.16pt;width:76.08pt;height:51.96pt">`);
  parts.push(`<img class="img" src="${assetDataUrl('proforma_footer.png')}" style="left:151.32pt;top:754.2pt;width:253.8pt;height:24.72pt">`);

  const tpl = fs.readFileSync(path.join(import.meta.dir, '..', 'proforma.template.html'), 'utf8');
  return tpl.replace('{{BODY}}', parts.join('\n'));
}
