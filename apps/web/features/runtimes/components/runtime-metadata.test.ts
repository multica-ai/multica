import { describe, expect, it } from "vitest";
import {
  getMetadataString,
  getMulticaCliVersion,
  getRuntimeCliVersion,
} from "./runtime-metadata";

describe("runtime metadata helpers", () => {
  it("returns null for missing or invalid values", () => {
    expect(getMetadataString({}, "version")).toBeNull();
    expect(getMetadataString({ version: 123 }, "version")).toBeNull();
    expect(getMetadataString({ version: "" }, "version")).toBeNull();
  });

  it("extracts runtime and multica CLI versions separately", () => {
    const metadata = {
      version: "codex-cli 0.118.0",
      cli_version: "multica 0.1.19",
    };

    expect(getRuntimeCliVersion(metadata)).toBe("codex-cli 0.118.0");
    expect(getMulticaCliVersion(metadata)).toBe("multica 0.1.19");
  });
});
