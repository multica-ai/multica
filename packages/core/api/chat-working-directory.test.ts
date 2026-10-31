// @vitest-environment node
import { describe, expect, it } from "vitest";
import { ChatSessionSchema, RuntimeDirectoryRequestSchema, EMPTY_RUNTIME_DIRECTORY_REQUEST } from "./schemas";
import { parseWithFallback } from "./schema";

describe("chat working directory response compatibility", () => {
  it("normalizes missing and malformed requested directories without losing the session", () => {
    for (const working_directory of [undefined, 42, { daemon_id: "d" }, { daemon_id: 1, path: "/tmp" }]) {
      expect(ChatSessionSchema.parse({ id: "s", working_directory }).working_directory).toBeNull();
    }
  });
  it("retains spaces and Unicode in a selected directory", () => {
    const working_directory = { daemon_id: "daemon", path: "/home/me/项目 files" };
    expect(ChatSessionSchema.parse({ id: "s", working_directory }).working_directory).toEqual(working_directory);
  });
  it("rejects malformed directory listings and unknown request statuses safely", () => {
    for (const raw of [{ id: "r", runtime_id: "rt", status: "future" }, { id: "r", runtime_id: "rt", status: "completed", result: { entries: [null] } }]) {
      expect(parseWithFallback(raw, RuntimeDirectoryRequestSchema, EMPTY_RUNTIME_DIRECTORY_REQUEST, { endpoint: "directory" })).toEqual(EMPTY_RUNTIME_DIRECTORY_REQUEST);
    }
  });
});
