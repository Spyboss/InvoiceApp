import { randomUUID } from 'node:crypto';
import { auditLog, type Db } from '@erp/db';
import type { JwtUser } from './auth';

/** Append-only audit trail entry. */
export async function audit(
  db: Db,
  user: JwtUser | undefined,
  action: 'CREATE' | 'UPDATE' | 'DELETE',
  entity: string,
  entityId: string,
  before: Record<string, unknown> | null = null,
  after: Record<string, unknown> | null = null,
): Promise<void> {
  await db.insert(auditLog).values({
    id: randomUUID(),
    userId: user?.sub ?? null,
    action,
    entity,
    entityId,
    before,
    after,
  });
}
