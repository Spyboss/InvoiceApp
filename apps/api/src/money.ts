/** Exact money math on decimal strings — cents integers, never floats. */
export function toCents(v: string | number): number {
  const s = typeof v === 'number' ? v.toFixed(2) : v;
  const [w, f = '0'] = s.split('.');
  return Number(w) * 100 + Number((f + '00').slice(0, 2));
}

export function fromCents(c: number): string {
  const sign = c < 0 ? '-' : '';
  const n = Math.abs(c);
  return `${sign}${Math.floor(n / 100)}.${String(n % 100).padStart(2, '0')}`;
}
