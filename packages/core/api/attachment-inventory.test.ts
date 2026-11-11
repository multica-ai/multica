// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import {
  collectDeliverableFiles,
  deliverableInventoryFromAttachments,
  mergeDeliverableSources,
} from "../attachments/deliverables";

afterEach(() => vi.unstubAllGlobals());

const attachment = {
  id: "attachment-1", workspace_id: "workspace-1", issue_id: "issue-1",
  comment_id: "comment-1", uploader_type: "member", uploader_id: "member-1",
  filename: "historical.png", content_type: "image/png", size_bytes: 123,
  url: "/uploads/historical.png", download_url: "/uploads/historical.png",
  markdown_url: "/api/attachments/attachment-1/download",
  created_at: "2026-11-09T00:00:00Z", comment_author_type: "agent",
  comment_author_id: "agent-1", comment_created_at: "2026-11-09T00:00:00Z",
};

async function listAttachments(body: unknown) {
  const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(body), {
    status: 200, headers: { "Content-Type": "application/json" },
  }));
  vi.stubGlobal("fetch", fetchMock);
  const result = await new ApiClient("https://api.example.test").listAttachments("issue-1");
  expect(fetchMock.mock.calls[0]?.[0]).toBe("https://api.example.test/api/issues/issue-1/attachments");
  return result;
}

it("keeps rendering when optional comment origin metadata has the wrong JSON type", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify([
    attachment,
    { ...attachment, id: "attachment-2", comment_id: "comment-2", comment_created_at: 123 },
  ]), { status: 200, headers: { "Content-Type": "application/json" } })));
  const attachments = await new ApiClient("https://api.example.test").listAttachments("issue-1");
  expect(() => mergeDeliverableSources(deliverableInventoryFromAttachments(attachments), [])).not.toThrow();
  expect(attachments).toHaveLength(2);
  expect(attachments[1]?.comment_created_at).toBeUndefined();
  expect(deliverableInventoryFromAttachments(attachments).map((entry) => entry.id)).toEqual(["comment-1"]);
});

it("preserves historical origins, same-name versions, and unknown server fields", async () => {
  const attachments = await listAttachments([
    { ...attachment, future_field: "preserved" },
    { ...attachment, id: "attachment-2", comment_id: "comment-2", comment_author_type: "future-author",
      created_at: "2026-11-10T00:00:00Z", comment_created_at: "2026-11-10T00:00:00Z" },
  ]);
  expect(attachments[0]).toMatchObject({ ...attachment, future_field: "preserved" });
  const inventory = mergeDeliverableSources(deliverableInventoryFromAttachments(attachments), []);
  expect(inventory.map((entry) => [entry.id, entry.actor_type, entry.actor_id])).toEqual([
    ["comment-1", "agent", "agent-1"],
    ["comment-2", "future-author", "agent-1"],
  ]);
  expect(collectDeliverableFiles(inventory)[0]?.versions.map((file) => file.id)).toEqual([
    "attachment-1", "attachment-2",
  ]);
});

it.each([
  ["comment_author_type", 123],
  ["comment_author_type", null],
  ["comment_author_id", { id: "agent-1" }],
  ["comment_author_id", null],
  ["comment_created_at", null],
  ["comment_created_at", "not-a-date"],
  ["comment_created_at", ""],
])("drops invalid optional %s (%j) without discarding the file or its siblings", async (field, value) => {
  const attachments = await listAttachments([
    attachment,
    { ...attachment, id: "attachment-2", comment_id: "comment-2", [field]: value },
  ]);
  expect(attachments.map((file) => file.id)).toEqual(["attachment-1", "attachment-2"]);
  expect(attachments[1]).toMatchObject({ filename: "historical.png", download_url: attachment.download_url });
  expect(attachments[1]?.[field as keyof typeof attachment]).toBeUndefined();
  expect(mergeDeliverableSources(deliverableInventoryFromAttachments(attachments), []).map((entry) => entry.id))
    .toEqual(["comment-1"]);
});

it("accepts attachments without the additive comment origin fields", async () => {
  const legacy = { ...attachment };
  Reflect.deleteProperty(legacy, "comment_author_type");
  Reflect.deleteProperty(legacy, "comment_author_id");
  Reflect.deleteProperty(legacy, "comment_created_at");
  const attachments = await listAttachments([legacy, { ...legacy, id: "description", comment_id: null }]);
  expect(attachments.map((file) => file.id)).toEqual(["attachment-1", "description"]);
  expect(deliverableInventoryFromAttachments(attachments)).toEqual([]);
});

it.each([
  null,
  { attachments: [] },
  [null],
  [{ ...attachment, filename: 123 }],
  [{ ...attachment, content_type: 123 }],
])("falls back to an empty list for an invalid response shape: %j", async (body) => {
  expect(await listAttachments(body)).toEqual([]);
});
