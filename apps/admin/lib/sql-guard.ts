// Pure SELECT-only SQL guard, split out of db.ts so it's testable without
// pulling in `server-only` (importing db.ts from a test throws by design —
// see lib/db.ts's own comment on that package).
//
// This is a runtime backstop for the read-only contract, not a substitute
// for a real read-only DB role (there isn't one for this connection string
// yet — see lib/db.ts). It only catches an accidental or future write/DDL
// statement making it into lib/queries.ts before it reaches Postgres, and
// rejects multiple statements in one call (defense against a stray trailing
// `; DROP ...` slipping into a query string) since pg's simple query
// protocol would otherwise execute all of them under a single call.

// Matched anywhere in the statement, not just at the start — Postgres allows
// a data-modifying CTE (`WITH x AS (DELETE FROM ... RETURNING ...) SELECT
// ...`), so a leading-keyword-only check would miss a write buried after
// `WITH ... AS (`.
const WRITE_KEYWORD = /\b(insert|update|delete|drop|alter|truncate|create|grant|revoke)\b/i;

export function assertReadOnlyStatement(text: string): void {
  const statements = text.split(";").map((s) => s.trim()).filter(Boolean);
  if (statements.length > 1) {
    throw new Error("query() refuses multi-statement SQL — pass one SELECT at a time");
  }
  const statement = statements[0] ?? "";
  if (!/^\s*(select|with)\b/i.test(statement) || WRITE_KEYWORD.test(statement)) {
    throw new Error("query() refuses non-SELECT SQL — this app is read-only");
  }
}
