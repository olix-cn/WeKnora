/** Interpret historical no-op records for presentation without rewriting stored audits. */
export function userAlreadyExisted(row: { action: string; details?: unknown }): boolean {
  return row.action === 'system.user_already_exists' || (
    row.action === 'system.user_created' && !!row.details && typeof row.details === 'object'
    && (row.details as Record<string, unknown>).idempotent === true
  )
}

export function userCreationAuditAction<T extends string>(row: { action: T; details?: unknown }): T | 'system.user_already_exists' {
  return userAlreadyExisted(row) ? 'system.user_already_exists' : row.action
}
