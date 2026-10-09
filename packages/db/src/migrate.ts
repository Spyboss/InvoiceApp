import path from 'node:path';
import { createDb, migrate } from './index';

const folder = path.join(import.meta.dir, '..', 'drizzle');
const db = createDb();
await migrate(db, folder);
console.log('migrations applied from', folder);
process.exit(0);
