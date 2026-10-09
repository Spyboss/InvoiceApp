import path from 'node:path';
import { createDb, migrate } from './index';

const folder = path.join(import.meta.dir, '..', 'drizzle');
const db = createDb({
  url: process.env.DATABASE_URL,
  dataDir: process.env.DATABASE_URL?.startsWith('postgres') ? undefined : (process.env.DB_DATA_DIR ?? '.data'),
});
await migrate(db, folder);
console.log('migrations applied from', folder);
process.exit(0);
