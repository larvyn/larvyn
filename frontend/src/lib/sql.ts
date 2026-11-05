const READ = /^(select|with|show|describe|desc|explain|pragma|values)\b/i
// INTO covers SELECT … INTO (Postgres creates a table, MySQL writes a file).
// REPLACE isn't here: as a statement it doesn't start with a read keyword,
// and as a function it's common in plain selects.
const WRITE = /\b(insert|update|delete|merge|upsert|truncate|drop|alter|create|into)\b/i

// isReadOnlySQL tells whether the SQL console can run text without asking.
// Every statement must start with a read keyword (`select 1; delete …`
// asks), and no write keyword may appear anywhere in the text. The keyword
// check runs on the whole text, not per statement, so a semicolon inside a
// string can't hide a data-modifying CTE behind a split. Asking too often
// (`where action = 'delete'`) is the safe mistake.
export function isReadOnlySQL(sql: string): boolean {
  const stmts = sql.split(';').map((s) => s.trim()).filter(Boolean)
  return stmts.length > 0 && stmts.every((s) => READ.test(s)) && !WRITE.test(sql)
}
