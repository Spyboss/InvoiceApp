import { PGlite } from '@electric-sql/pglite';
import { drizzle as drizzlePglite } from 'drizzle-orm/pglite';
import { drizzle as drizzlePg } from 'drizzle-orm/postgres-js';
import type { PostgresJsDatabase } from 'drizzle-orm/postgres-js';
import type { PgliteDatabase } from 'drizzle-orm/pglite';
import postgres from 'postgres';
import * as schema from './schema';

export * from './schema';

export type Schema = typeof schema;
export type Db = PostgresJsDatabase<Schema> | PgliteDatabase<Schema>;

/**
 * Create the database handle.
 *  - `url` starting with `postgres` -> production Postgres (postgres-js driver)
 *  - otherwise an embedded PGlite (real Postgres, in-process) — used for dev
 *    tests and the zero-setup local edition of the product.
 */
export function createDb(opts: { url?: string; dataDir?: string } = {}): Db {
  const url = opts.url ?? process.env.DATABASE_URL ?? '';
  if (url.startsWith('postgres')) {
    return drizzlePg(postgres(url, { max: 10 }), { schema });
  }
  // Embedded PGlite: file-backed when a data dir is given, in-memory otherwise.
  const pglite = new PGlite(opts.dataDir || undefined);
  return drizzlePglite(pglite, { schema });
}

export async function migrate(db: Db, migrationsFolder: string): Promise<void> {
  if ('transaction' in db && 'client' in db) {
    const { migrate } = await import('drizzle-orm/postgres-js/migrator');
    await migrate(db as PostgresJsDatabase<Schema>, { migrationsFolder });
  } else {
    const { migrate } = await import('drizzle-orm/pglite/migrator');
    await migrate(db as PgliteDatabase<Schema>, { migrationsFolder });
  }
}
