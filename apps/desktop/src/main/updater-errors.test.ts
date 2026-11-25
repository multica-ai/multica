// @vitest-environment node
import { describe, expect, it } from "vitest";
import { classifyUpdaterError } from "./updater-errors";

describe("classifyUpdaterError", () => {
  it.each([
    [{ code: "ERR_UPDATER_CHANNEL_FILE_NOT_FOUND", message: "raw headers and stack" }, "unavailable"],
    [{ code: "ERR_UPDATER_LATEST_VERSION_NOT_FOUND" }, "unavailable"],
    [{ statusCode: 503 }, "unavailable"],
    [{ code: "ENOTFOUND" }, "network"],
    [new Error("net::ERR_INTERNET_DISCONNECTED"), "network"],
    [new Error("request failed ETIMEDOUT"), "network"],
    [{ statusCode: 403 }, "unknown"],
    [{ code: "ERR_UPDATER_INVALID_SIGNATURE" }, "unknown"],
    [null, "unknown"],
    ["private raw response", "unknown"],
  ])("classifies %j as %s", (error, expected) => {
    expect(classifyUpdaterError(error)).toBe(expected);
  });
});
