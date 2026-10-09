import path from 'node:path';
import { createDb, migrate, type Db } from '@erp/db';
import { createApp } from './app';
import { env } from './env';

export async function start(): Promise<void> {
  const db: Db = createDb({ url: env.DATABASE_URL, dataDir: env.DB_DATA_DIR });

  // M1: apply migrations at boot (revisit idempotent migration strategy later).
  const folder = path.join(import.meta.dir, '..', '..', '..', 'packages', 'db', 'drizzle');
  await migrate(db, folder);

  const app = createApp(db);
  Bun.serve({ port: env.PORT, fetch: app.fetch });
  console.log(`erp-api listening on http://localhost:${env.PORT}`);
}

if (import.meta.main) {
  await start();
}
