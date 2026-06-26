import { describe, it, expect } from "vitest";
import { maskSecretsDeep, SECRET_MASK } from "./mask-secrets";
import { snapshotToFields } from "./snapshot-fields";
import type { AgentContextSnapshot } from "@multica/core/types";

describe("maskSecretsDeep", () => {
  it("masks values under sensitive keys but keeps structure", () => {
    const input = {
      servers: {
        github: {
          url: "https://api.example.com/mcp",
          headers: { Authorization: "Bearer sk-abc123def456" },
          env: { API_TOKEN: "abc123", DB_PASSWORD: "hunter2" },
        },
      },
    };
    const out = maskSecretsDeep(input) as typeof input;
    expect(out.servers.github.url).toBe("https://api.example.com/mcp");
    expect(out.servers.github.headers.Authorization).toBe(SECRET_MASK);
    expect(out.servers.github.env.API_TOKEN).toBe(SECRET_MASK);
    expect(out.servers.github.env.DB_PASSWORD).toBe(SECRET_MASK);
  });

  it("masks secret-looking string values regardless of key", () => {
    const out = maskSecretsDeep({ note: "use Bearer sk-live-9999 to auth" }) as {
      note: string;
    };
    expect(out.note).not.toContain("sk-live-9999");
    expect(out.note).toContain(SECRET_MASK);
  });

  it("masks inline credentials in a connection URL", () => {
    const out = maskSecretsDeep({
      dsn: "postgres://user:s3cr3t@db.host:5432/app",
    }) as { dsn: string };
    expect(out.dsn).not.toContain("s3cr3t");
    expect(out.dsn).toContain(SECRET_MASK);
  });

  it("does not over-mask benign config", () => {
    const input = { model: "opus", max_tokens: 4096, public_key: "anyone" };
    const out = maskSecretsDeep(input);
    expect(out).toEqual(input);
  });

  it("never mutates the input", () => {
    const input = { headers: { token: "real" } };
    maskSecretsDeep(input);
    expect(input.headers.token).toBe("real");
  });
});

describe("snapshotToFields masking", () => {
  const base: AgentContextSnapshot = {
    instructions: "do the thing",
    description: "",
    model: "opus",
    thinking_level: "",
    persona_sandbox: "",
    skill_ids: [],
    custom_env_keys: ["OPENAI_API_KEY"],
    mcp_config: {
      servers: { x: { headers: { Authorization: "Bearer leak-me-123" } } },
    },
  };

  it("MCP config field never renders a raw token", () => {
    const f = snapshotToFields(base).find((f) => f.key === "mcp_config");
    expect(f?.value).toBeDefined();
    expect(f!.value).not.toContain("leak-me-123");
    expect(f!.value).toContain(SECRET_MASK);
  });
});
