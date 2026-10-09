import { and, eq } from 'drizzle-orm';
import { invoiceCounters, type Db } from '@erp/db';
import type { InvoiceType } from '@erp/shared';

/**
 * Allocate the next invoice number for (type, year) atomically.
 * Mirrors the FastAPI logic: 4-digit zero-padded sequence per type per year.
 * Returns e.g. "0001".
 */
export async function nextInvoiceNumber(db: Db, type: InvoiceType, year: number): Promise<string> {
  // tx typed loosely: the Postgres and PGlite drizzle instances have identical
  // query APIs but distinct static types; revisit when we pin one dialect.
  return (db as any).transaction(async (tx: any) => {
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
  });
}
