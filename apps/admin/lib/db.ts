import "server-only";
import { Pool, type QueryResultRow } from "pg";
import { assertReadOnlyStatement } from "./sql-guard";

// This app has direct read-only access to agentfarm's Postgres database
// (bypassing the Go REST API by design — see the admin dashboard plan).
// It must NEVER issue writes or run migrations against the shared schema;
// `query()` below is the only entry point and is documented as SELECT-only.
// No DB-level read-only role exists yet for this connection string, so
// `query()` also runs a runtime statement guard (lib/sql-guard.ts) as a
// backstop — real enforcement still relies on review + that guard, not a
// database-level grant.

let pool: Pool | undefined;

function getPool(): Pool {
  if (!pool) {
    const connectionString = process.env.DATABASE_URL;
    if (!connectionString) {
      throw new Error("DATABASE_URL is not set — cannot connect to Postgres");
    }
    pool = new Pool({ connectionString, max: 10 });
  }
  return pool;
}

/**
 * Run a parameterized, read-only SQL query. Callers in lib/queries.ts must
 * only ever pass SELECT statements — this app has no business writing to
 * agentfarm's shared schema. `assertReadOnlyStatement` (lib/sql-guard.ts)
 * enforces that at runtime as a backstop — see that file for why it isn't
 * a substitute for a real read-only DB role.
 */
export async function query<T extends QueryResultRow = QueryResultRow>(
  text: string,
  params: unknown[] = [],
): Promise<T[]> {
  assertReadOnlyStatement(text);
  const client = await getPool().connect();
  try {
    const result = await client.query<T>(text, params);
    return result.rows;
  } finally {
    client.release();
  }
}
