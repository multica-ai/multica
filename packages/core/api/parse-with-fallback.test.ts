// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { z } from "zod";
import { noopLogger } from "../logger";
import { parseWithFallback, setSchemaLogger } from "./schema";

const LoginSchema = z.object({ token: z.string(), user: z.object({ id: z.string() }) });

describe("parseWithFallback", () => {
  afterEach(() => setSchemaLogger(noopLogger));

  it("logs the received payload for ordinary responses", () => {
    const warn = vi.fn();
    setSchemaLogger({ ...noopLogger, warn });

    parseWithFallback({ token: "t", user: null }, LoginSchema, null, { endpoint: "/x" });

    expect(warn.mock.calls[0]?.[1]).toMatchObject({ received: { token: "t", user: null } });
  });

  it("never logs a sensitive payload, only the validation issues", () => {
    const warn = vi.fn();
    setSchemaLogger({ ...noopLogger, warn });

    const result = parseWithFallback(
      { token: "secret-jwt", user: null },
      LoginSchema,
      null,
      { endpoint: "/auth/login-totp", sensitive: true },
    );

    expect(result).toBeNull();
    const meta = warn.mock.calls[0]?.[1];
    expect(meta.received).toBe("[redacted]");
    expect(meta.issues.length).toBeGreaterThan(0);
    expect(JSON.stringify(warn.mock.calls)).not.toContain("secret-jwt");
  });
});
