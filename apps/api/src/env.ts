import { z } from 'zod';

const envSchema = z.object({
  PORT: z.coerce.number().default(8787),
  JWT_SECRET: z.string().min(16).default('dev-only-secret-change-me'),
  DATABASE_URL: z.string().optional(),       // postgres://... in cloud; empty = embedded PGlite
  DB_DATA_DIR: z.string().default('.data'),  // PGlite data dir (local edition / dev)
});

export const env = envSchema.parse(process.env);
export type Env = typeof env;
