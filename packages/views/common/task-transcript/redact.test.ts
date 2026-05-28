import { describe, it, expect } from "vitest";
import { redactSecrets, redactValue } from "./redact";

describe("redactSecrets", () => {
  it("redacts AWS access key", () => {
    const result = redactSecrets("key: AKIAIOSFODNN7EXAMPLE");
    expect(result).not.toContain("AKIAIOSFODNN7EXAMPLE");
    expect(result).toContain("[REDACTED AWS KEY]");
  });

  it("redacts AWS secret key", () => {
    const result = redactSecrets("aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY");
    expect(result).not.toContain("wJalrXUtnFEMI");
  });

  it("redacts PEM private keys", () => {
    const input = "-----BEGIN RSA PRIVATE KEY-----\nMIIEow...\n-----END RSA PRIVATE KEY-----";
    const result = redactSecrets(input);
    expect(result).not.toContain("MIIEow");
    expect(result).toContain("[REDACTED PRIVATE KEY]");
  });

  it("redacts GitHub tokens", () => {
    const result = redactSecrets("GITHUB_TOKEN=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmn");
    expect(result).not.toContain("ghp_");
  });

  it("redacts GitLab tokens", () => {
    const result = redactSecrets("glpat-AbCdEfGhIjKlMnOpQrStUvWx");
    expect(result).not.toContain("glpat-");
    expect(result).toContain("[REDACTED GITLAB TOKEN]");
  });

  it("redacts OpenAI/Anthropic API keys", () => {
    const result = redactSecrets("sk-proj-abc123def456ghi789jkl012mno345");
    expect(result).not.toContain("sk-proj");
    expect(result).toContain("[REDACTED API KEY]");
  });

  it("redacts Slack tokens", () => {
    const result = redactSecrets("xoxb-123456789012-1234567890123-AbCdEfGhIjKl");
    expect(result).not.toContain("xoxb-");
  });

  it("redacts JWT tokens", () => {
    const result = redactSecrets("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c");
    expect(result).not.toContain("eyJhbGci");
    expect(result).toContain("[REDACTED JWT]");
  });

  it("redacts Bearer tokens", () => {
    const result = redactSecrets("Authorization: Bearer abc123xyz.def456");
    expect(result).toContain("Bearer [REDACTED]");
    expect(result).not.toContain("abc123xyz");
  });

  it("redacts connection strings", () => {
    const result = redactSecrets("postgres://admin:s3cret@db.example.com:5432/mydb");
    expect(result).not.toContain("s3cret");
  });

  it("redacts generic credential env vars", () => {
    for (const key of ["PASSWORD", "SECRET", "TOKEN", "DATABASE_URL", "API_KEY"]) {
      const result = redactSecrets(`${key}=supersecretvalue123`);
      expect(result).toContain("[REDACTED CREDENTIAL]");
      expect(result).not.toContain("supersecretvalue123");
    }
  });

  it("redacts custom_env from agent list JSON without dropping routing metadata", () => {
    const result = redactSecrets(JSON.stringify([
      {
        id: "agent-1",
        name: "Router",
        status: "online",
        runtime_mode: "cloud",
        skills: [{ id: "skill-1", name: "route" }],
        custom_env: {
          SECOND_BRAIN_TOKEN: "token-value-123",
          SEARCH_API_KEY: "key-value-456",
          DB_PASSWORD: "password-value-789",
          SESSION_JWT: "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
        },
        custom_env_redacted: false,
      },
    ]));
    for (const leak of [
      "SECOND_BRAIN_TOKEN",
      "SEARCH_API_KEY",
      "DB_PASSWORD",
      "SESSION_JWT",
      "token-value-123",
      "key-value-456",
      "password-value-789",
      "eyJhbGci",
    ]) {
      expect(result).not.toContain(leak);
    }
    expect(result).toContain('"id":"agent-1"');
    expect(result).toContain('"name":"Router"');
    expect(result).toContain('"status":"online"');
    expect(result).toContain('"runtime_mode":"cloud"');
    expect(result).toContain('"skills"');
    expect(result).toContain('"custom_env_redacted":true');
    expect(result).toContain('"custom_env_key_count":4');
  });

  it("redacts nested custom_env values in structured input", () => {
    const result = redactValue({
      command: "multica agent list --output json",
      payload: {
        name: "Router",
        custom_env: {
          API_TOKEN: "token-value",
          API_KEY: "key-value",
        },
      },
    }) as Record<string, unknown>;
    const payload = result.payload as Record<string, unknown>;
    expect(payload.name).toBe("Router");
    expect(payload.custom_env).toBeUndefined();
    expect(payload.custom_env_redacted).toBe(true);
    expect(payload.custom_env_key_count).toBe(2);
    expect(JSON.stringify(result)).not.toContain("token-value");
    expect(JSON.stringify(result)).not.toContain("API_TOKEN");
  });

  it("redacts multiple secrets in one string", () => {
    const result = redactSecrets("AKIAIOSFODNN7EXAMPLE and ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmn");
    expect(result).not.toContain("AKIAIOSFODNN7EXAMPLE");
    expect(result).not.toContain("ghp_");
  });

  it("does not alter normal text", () => {
    const inputs = [
      "This is a normal commit message about fixing a bug",
      "The function returns skip-navigation as the class name",
      "Created PR #42 for the authentication feature",
      "Running tests in /tmp/test-workspace/project",
      "The API endpoint /api/issues/123 was updated",
    ];
    for (const input of inputs) {
      expect(redactSecrets(input)).toBe(input);
    }
  });
});
