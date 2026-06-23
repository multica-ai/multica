import { describe, expect, it } from "vitest";
import { redactText, redactExceptionProperties } from "./redact-exception";

describe("redactText", () => {
  it("redacts email addresses", () => {
    expect(redactText("Invalid email: alice@example.com")).toBe(
      "Invalid email: [redacted]",
    );
  });

  it("strips URL query strings that may carry tokens, keeping host + path", () => {
    expect(
      redactText("fetch failed https://api.multica.ai/issues?token=abc123secret"),
    ).toBe("fetch failed https://api.multica.ai/issues?[redacted]");
  });

  it("redacts long opaque tokens (JWT / API key / uuid)", () => {
    expect(redactText("auth header eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9")).toBe(
      "auth header [redacted]",
    );
  });

  it("keeps the non-sensitive part of a message intact", () => {
    expect(redactText("Cannot read property 'x' of undefined")).toBe(
      "Cannot read property 'x' of undefined",
    );
  });

  it("passes through non-strings unchanged", () => {
    expect(redactText(undefined)).toBeUndefined();
    expect(redactText(42)).toBe(42);
  });
});

describe("redactExceptionProperties", () => {
  it("scrubs the message and each $exception_list value, leaving frames untouched", () => {
    const props = {
      $exception_message: "Bad email bob@corp.com",
      $exception_list: [
        {
          type: "TypeError",
          value: "Token leaked: ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
          stacktrace: { frames: [{ filename: "app.tsx", lineno: 5, function: "render" }] },
        },
      ],
    };

    redactExceptionProperties(props);

    const entry = props.$exception_list[0]!;
    expect(props.$exception_message).toBe("Bad email [redacted]");
    expect(entry.value).toBe("Token leaked: [redacted]");
    // Frames are code locations, not user data — left intact.
    expect(entry.stacktrace.frames[0]).toEqual({
      filename: "app.tsx",
      lineno: 5,
      function: "render",
    });
    expect(entry.type).toBe("TypeError");
  });

  it("is safe on undefined / malformed properties", () => {
    expect(redactExceptionProperties(undefined)).toBeUndefined();
    expect(() =>
      redactExceptionProperties({ $exception_list: "not-an-array" as unknown as [] }),
    ).not.toThrow();
  });
});
