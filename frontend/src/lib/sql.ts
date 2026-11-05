const READ = /^(select|with|show|describe|desc|explain|pragma|values)\b/i
const WRITE = /\b(insert|update|delete|replace|merge|upsert|truncate|drop|alter|create)\b/i

// isReadOnlySQL tells whether the SQL console can run text without asking.
// Every statement must start with a read keyword, so `select 1; delete …`
// asks. A CTE or EXPLAIN can still write (`with x as (…) delete …`,
// Postgres' `explain analyze delete …` runs the delete), so those ask
// whenever a write keyword appears. Asking too often is the safe mistake.
export function isReadOnlySQL(sql: string): boolean {
  const stmts = sql.split(';').map((s) => s.trim()).filter(Boolean)
  return stmts.length > 0 && stmts.every((s) => READ.test(s) && !(/^(with|explain)\b/i.test(s) && WRITE.test(s)))
}
