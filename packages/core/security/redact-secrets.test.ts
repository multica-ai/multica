// @vitest-environment node
import { describe, it, expect } from "vitest";
import { redactSecrets } from "./redact-secrets";

describe("redactSecrets", () => {
  const multicaTokenBody = "0123456789abcdef".repeat(2) + "01234567";

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

  it("redacts GitHub fine-grained PATs (github_pat_)", () => {
    const key = ["github_", "pat_", "11ABCDEFG0aBcDeFgHiJkL_mNoPqRsTuVwXyZ0123456789AbCdEfGhIjKlMnOpQrSt"].join("");
    const result = redactSecrets(`the build left ${key} in the log`);
    expect(result).not.toContain("github_pat_");
    expect(result).toContain("[REDACTED GITHUB TOKEN]");
  });

  it("redacts Google API keys (AIza...)", () => {
    const key = ["AIza", "SyD1234567890abcdefghijklmnopqrstuv"].join("");
    const result = redactSecrets(`the config still had ${key} in it`);
    expect(result).not.toContain("AIzaSy");
    expect(result).toContain("[REDACTED GOOGLE API KEY]");
  });

  it("redacts Google API keys ending with dash", () => {
    const key = ["AIza", "SyB1cD3fGhIjKlMnOpQrStUvWxYz012345-"].join("");
    const result = redactSecrets(`the config still had "${key}" in it`);
    expect(result).not.toContain("AIzaSy");
    expect(result).toContain('"[REDACTED GOOGLE API KEY]" in it');
  });

  it("redacts GitLab tokens", () => {
    const result = redactSecrets("glpat-AbCdEfGhIjKlMnOpQrStUvWx");
    expect(result).not.toContain("glpat-");
    expect(result).toContain("[REDACTED GITLAB TOKEN]");
  });

  it.each(["mat_", "mul_", "mdt_", "mcn_"])("redacts %s credentials in stored output", (prefix) => {
    const token = prefix + multicaTokenBody;
    expect(redactSecrets(`retrying with --token ${token} and moving on`))
      .toBe("retrying with --token [REDACTED_MULTICA_TOKEN] and moving on");
  });

  it("redacts Multica credentials after escaped JSON whitespace", () => {
    const token = "mat_" + multicaTokenBody;
    expect(redactSecrets(JSON.stringify({ value: `line1\n${token}\t${token}` })))
      .toBe(JSON.stringify({ value: "line1\n[REDACTED_MULTICA_TOKEN]\t[REDACTED_MULTICA_TOKEN]" }));
  });

  it("preserves the generic credential marker for Multica environment assignments", () => {
    expect(redactSecrets("MULTICA_TOKEN=mat_" + multicaTokenBody))
      .toBe("MULTICA_[REDACTED CREDENTIAL]");
  });

  it("preserves incomplete Multica credentials and identifiers containing a token prefix", () => {
    const input = "mat_" + multicaTokenBody.slice(1) + " prefix_mat_" + multicaTokenBody;
    expect(redactSecrets(input)).toBe(input);
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
