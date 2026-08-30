import type { ZodType } from "zod";
import { type Logger, noopLogger } from "../logger";

// Module-level logger for schema warnings. Defaults to no-op so test
// runs don't spam stderr; the platform layer wires a real logger via
// `setSchemaLogger` at app boot.
let schemaLogger: Logger = noopLogger;

export function setSchemaLogger(logger: Logger): void {
  schemaLogger = logger;
}

export interface ParseOptions {
  /** Endpoint identifier used in the warning log so we can grep for which
   *  contract drifted in production telemetry. */
  endpoint: string;
}

export class ProtectedResponseValidationError extends Error {
  readonly endpoint: string;

  constructor(endpoint: string) {
    super(`Protected API response failed schema validation: ${endpoint}`);
    this.name = "ProtectedResponseValidationError";
    this.endpoint = endpoint;
  }
}

/**
 * Validate a JSON value parsed from an API response against a zod schema,
 * returning the parsed value on success or `fallback` on failure.
 *
 * On failure we log a warning with the endpoint and zod's structured error,
 * but never throw — the UI layer must keep rendering. This is the boundary
 * defense that turns "API contract drifted" from a white-screen incident
 * into a degraded-but-rendering page.
 *
 * The return type is anchored to `T` (inferred from `fallback`), not to the
 * schema's `z.infer` type. Schemas are intentionally **lenient** — string
 * enums kept as `z.string()` so an unknown enum value still parses, etc. —
 * so the parsed runtime value can be wider than the strict TS type at the
 * call site. The caller asserts compatibility by typing the fallback to the
 * expected `T`; downstream code is already responsible for handling unknown
 * enum values via `default`-bearing switches and optional chaining.
 *
 * See CLAUDE.md "API Response Compatibility" for when to reach for this.
 */
export function parseWithFallback<T>(
  data: unknown,
  schema: ZodType,
  fallback: T,
  opts: ParseOptions,
): T {
  const result = schema.safeParse(data);
  if (result.success) return result.data as T;
  schemaLogger.warn(
    `API response failed schema validation: ${opts.endpoint}`,
    {
      endpoint: opts.endpoint,
      issues: result.error.issues,
      received: data,
    },
  );
  return fallback;
}

/**
 * Validates authorization-sensitive responses without retaining or logging the
 * received body. A malformed policy, approval, capability, or operations
 * response must fail closed, and diagnostics must remain metadata-only even
 * when the bad body contains credentials or customer values.
 */
export function parseProtectedResponse<T>(
  data: unknown,
  schema: ZodType<T>,
  opts: ParseOptions,
): T {
  const result = schema.safeParse(data);
  if (result.success) return result.data;

  schemaLogger.warn("API response failed protected schema validation", {
    endpoint: opts.endpoint,
    issue_count: result.error.issues.length,
    issue_codes: [...new Set(result.error.issues.map((issue) => issue.code))],
  });
  throw new ProtectedResponseValidationError(opts.endpoint);
}
