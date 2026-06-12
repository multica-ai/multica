import { describe, expect, it } from "vitest";
import { preprocessJsonLiterals } from "./preprocess-json";

describe("preprocessJsonLiterals", () => {
  it("wraps a bare JSON object line in a code block", () => {
    const input = '{"error":{"message":"openai_error","type":"api_error"},"status":400}';
    const out = preprocessJsonLiterals(input);
    expect(out).toMatch(/^```json\n/);
    expect(out).toMatch(/```$/);
    expect(out).toContain('"error"');
    expect(out).toContain('"openai_error"');
  });

  it("wraps a bare JSON array line in a code block", () => {
    const input = '[{"id":1,"name":"foo"},{"id":2,"name":"bar"}]';
    const out = preprocessJsonLiterals(input);
    expect(out).toMatch(/^```json\n/);
    expect(out).toMatch(/```$/);
  });

  it("leaves JSON that is already in a code block unchanged", () => {
    const input = '```json\n{"key":"value"}\n```';
    const out = preprocessJsonLiterals(input);
    expect(out).toBe(input);
  });

  it("leaves JSON inside inline code unchanged", () => {
    const input = 'See `{"key":"value"}` for details';
    const out = preprocessJsonLiterals(input);
    expect(out).toBe(input);
  });

  it("does not modify plain prose", () => {
    const input = "This is a normal comment with no JSON.";
    expect(preprocessJsonLiterals(input)).toBe(input);
  });

  it("does not modify short JSON-like text (< 10 chars)", () => {
    const input = '{"a":1}';
    expect(preprocessJsonLiterals(input)).toBe(input);
  });

  it("wraps only the JSON line in a mixed markdown comment", () => {
    const input =
      "Here is the error response:\n" +
      '{"error":{"message":"rate_limit_exceeded"},"code":429}\n' +
      "Please retry later.";
    const out = preprocessJsonLiterals(input);
    expect(out).toContain("Here is the error response:");
    expect(out).toContain("```json");
    expect(out).toContain("Please retry later.");
  });

  it("leaves invalid JSON-like text unchanged", () => {
    const input = "{this is not valid json: 42}";
    expect(preprocessJsonLiterals(input)).toBe(input);
  });

  it("formats JSON with pretty-print indentation", () => {
    const input = '{"a":1,"b":{"c":2}}';
    const out = preprocessJsonLiterals(input);
    expect(out).toContain("\n");
    expect(out).toContain('  "a": 1');
  });

  it("returns empty string unchanged", () => {
    expect(preprocessJsonLiterals("")).toBe("");
  });
});
