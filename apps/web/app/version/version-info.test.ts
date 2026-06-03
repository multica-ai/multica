import { describe, expect, it } from "vitest";
import { readVersionInfo } from "./version-info";

describe("readVersionInfo", () => {
  it("returns the commit and builtAt when both env vars are set", () => {
    expect(
      readVersionInfo({
        COMMIT_SHA: "dcec48f8747a0c32f55ed409810241750caf16ad",
        BUILD_TIME: "2026-05-19T08:09:19Z",
      }),
    ).toEqual({
      commit: "dcec48f8747a0c32f55ed409810241750caf16ad",
      builtAt: "2026-05-19T08:09:19Z",
    });
  });

  it("falls back to commit='unknown' when COMMIT_SHA is missing", () => {
    expect(readVersionInfo({})).toEqual({ commit: "unknown", builtAt: null });
  });

  it("treats an empty COMMIT_SHA as missing", () => {
    expect(readVersionInfo({ COMMIT_SHA: "" })).toEqual({
      commit: "unknown",
      builtAt: null,
    });
  });

  it("trims surrounding whitespace on both fields", () => {
    expect(
      readVersionInfo({
        COMMIT_SHA: "  dcec48f8  \n",
        BUILD_TIME: " 2026-05-19T08:09:19Z\n",
      }),
    ).toEqual({ commit: "dcec48f8", builtAt: "2026-05-19T08:09:19Z" });
  });

  it("returns builtAt=null when BUILD_TIME is whitespace-only", () => {
    expect(
      readVersionInfo({ COMMIT_SHA: "abc1234", BUILD_TIME: "   " }),
    ).toEqual({ commit: "abc1234", builtAt: null });
  });
});
