import { and, eq } from 'drizzle-orm';
import { invoiceCounters } from '@erp/db';
import type { InvoiceType } from '@erp/shared';

// tx typed loosely on purpose: the Postgres and PGlite drizzle transactions
// have identical query APIs but distinct static types (Db is a union).
type Tx = any;

/**
 * Allocate the next invoice number for (type, year) inside an EXISTING
 * transaction — the caller owns atomicity (number + invoice row + stock flip
 * commit or roll back together). 4-digit zero-padded per type per year.
 * Returns e.g. "0001". The `for('update')` lock serialises concurrent
 * allocations of the same (type, year).
 */
export async function allocateInvoiceNumber(tx: Tx, type: InvoiceType, year: number): Promise<string> {
  const key = and(eq(invoiceCounters.invoiceType, type), eq(invoiceCounters.year, year));
  const [row] = await tx.select().from(invoiceCounters).where(key).for('update');
  const last = row?.lastNo ?? 0;
  const next = last + 1;
  if (row) {
    await tx.update(invoiceCounters).set({ lastNo: next }).where(key);
  } else {
    await tx.insert(invoiceCounters).values({ invoiceType: type, year, lastNo: next });
  }
  return String(next).padStart(4, '0');
}
