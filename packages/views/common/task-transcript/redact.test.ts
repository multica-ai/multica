import { describe, it, expect } from "vitest";
import { redactSecrets } from "./redact";

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
