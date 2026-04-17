import { describe, expect, it } from "vitest";
import { isSafeExternalHttpUrl } from "./external-url";

describe("isSafeExternalHttpUrl", () => {
  it("allows http and https URLs", () => {
    expect(isSafeExternalHttpUrl("https://multica.ai")).toBe(true);
    expect(isSafeExternalHttpUrl("http://localhost:3000/auth")).toBe(true);
  });

  it("rejects invalid and non-http schemes", () => {
    expect(isSafeExternalHttpUrl("not a url")).toBe(false);
    expect(isSafeExternalHttpUrl("file:///etc/passwd")).toBe(false);
    expect(isSafeExternalHttpUrl("vscode://file/test")).toBe(false);
    expect(isSafeExternalHttpUrl("mailto:test@example.com")).toBe(false);
  });
});
