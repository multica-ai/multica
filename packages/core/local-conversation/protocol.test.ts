// @vitest-environment node
import { describe, expect, it } from "vitest";
import { localMessageDisplayContent, localReasoningLevels, sideWorkspaceSyncSchema } from "./protocol";

describe("local image message display", () => {
  it("hides standalone image markers while retaining authored paragraphs and the stored message", () => {
    const message = { role: "user" as const, attachmentIds: ["image-1", "image-2"], content: "Look at this\n\n[Image attachment: first.png]\n\n[Image attachment: second.jpg]\n\nKeep this paragraph" };
    expect(localMessageDisplayContent(message)).toBe("Look at this\n\nKeep this paragraph");
    expect(message.content).toContain("[Image attachment: first.png]");
    expect(localMessageDisplayContent({ ...message, content: "[Image attachment: first.png]" })).toBe("");
    expect(localMessageDisplayContent({ ...message, content: "[Image attachment: first.png]\n\nA question" })).toBe("A question");
  });
  it("preserves ordinary text, inline examples, assistant replies and unattached messages", () => {
    const content = "[Image attachment: example.png]";
    expect(localMessageDisplayContent({ role: "user", content })).toBe(content);
    expect(localMessageDisplayContent({ role: "assistant", content, attachmentIds: ["image"] })).toBe(content);
    const authored = "\n\nExample: [Image attachment: example.png]\n\n\nSecond paragraph";
    expect(localMessageDisplayContent({ role: "user", content: authored, attachmentIds: ["image"] })).toBe(authored);
    expect(localMessageDisplayContent()).toBe("");
  });
});

const snapshot = { conversationId: "main", revision: 1, panelOpen: true, selectedTabId: "web:one", tabs: [{ id: "web:one", kind: "web", url: "https://example.test" }] };
describe("Side workspace snapshot boundary", () => {
  it("defaults visibility for snapshots that only include panel state", () => {
    expect(sideWorkspaceSyncSchema.parse(snapshot).visible).toBe(false);
  });
  it("rejects duplicate IDs, foreign selection and executable URLs", () => {
    expect(() => sideWorkspaceSyncSchema.parse({ ...snapshot, tabs: [...snapshot.tabs, ...snapshot.tabs] })).toThrow();
    expect(() => sideWorkspaceSyncSchema.parse({ ...snapshot, selectedTabId: "foreign" })).toThrow();
    expect(() => sideWorkspaceSyncSchema.parse({ ...snapshot, tabs: [{ ...snapshot.tabs[0], url: "javascript:alert(1)" }] })).toThrow();
    expect(() => sideWorkspaceSyncSchema.parse({ ...snapshot, tabs: [{ ...snapshot.tabs[0], url: "https://user:secret@example.test" }] })).toThrow();
  });
  it("rejects malformed revisions, overfull layouts and invalid file metadata", () => {
    expect(() => sideWorkspaceSyncSchema.parse({ ...snapshot, revision: -1 })).toThrow();
    expect(() => sideWorkspaceSyncSchema.parse({ ...snapshot, tabs: Array.from({ length: 21 }, (_, id) => ({ ...snapshot.tabs[0], id: `web:${id}` })) })).toThrow();
    expect(() => sideWorkspaceSyncSchema.parse({ ...snapshot, tabs: [{ id: "file:one", kind: "file", path: "/tmp/test" }] })).toThrow();
  });
});

describe("following the default model", () => {
  const levels = [{ value: "high", label: "High" }];
  it("reads the row a daemon marks as the default, and tolerates a daemon that marks none", () => {
    expect(localReasoningLevels([{ id: "a", label: "A" }, { id: "b", label: "B", isDefault: true, thinking: { supported_levels: levels } }], null)).toEqual(levels);
    expect(localReasoningLevels([{ id: "a", label: "A", thinking: { supported_levels: levels } }], null)).toEqual([]);
  });
});
