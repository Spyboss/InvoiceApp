/**
 * HTML -> PDF via headless Chromium (or Edge/Chrome on Windows dev machines).
 * Uses the same `--headless --print-to-pdf` pipeline for dev and production;
 * set CHROMIUM_PATH on Linux servers.
 */
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';

const CANDIDATES = [
  process.env.CHROMIUM_PATH,
  'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
  'C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe',
  'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe',
  'C:\\Program Files\\Microsoft\\Edge\\Application\\msedge.exe',
  '/usr/bin/chromium',
  '/usr/bin/chromium-browser',
  '/usr/bin/google-chrome',
].filter(Boolean) as string[];

export function findBrowser(): string {
  for (const c of CANDIDATES) {
    try { if (fs.existsSync(c)) return c; } catch { /* keep looking */ }
  }
  throw new Error('No Chromium/Chrome/Edge found — set CHROMIUM_PATH');
}

/** Render an HTML document to a PDF buffer (US Letter pages, no header/footer). */
export function renderPdf(html: string): Uint8Array<ArrayBuffer> {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'erp-pdf-'));
  const htmlPath = path.join(dir, 'doc.html');
  const pdfPath = path.join(dir, 'doc.pdf');
  try {
    fs.writeFileSync(htmlPath, html, 'utf8');
    const r = spawnSync(findBrowser(), [
      '--headless', '--disable-gpu', '--no-first-run',
      `--user-data-dir=${path.join(dir, 'profile')}`,
      '--no-pdf-header-footer',
      `--print-to-pdf=${pdfPath}`,
      `file:///${htmlPath.replace(/\\/g, '/')}`,
    ], { encoding: 'utf8', timeout: 60_000 });
    if (!fs.existsSync(pdfPath)) {
      throw new Error(`browser failed to print PDF: ${r.stderr || r.error?.message || 'unknown'}`);
    }
    return new Uint8Array(fs.readFileSync(pdfPath));
  } finally {
    try { fs.rmSync(dir, { recursive: true, force: true }); } catch { /* best effort */ }
  }
}
