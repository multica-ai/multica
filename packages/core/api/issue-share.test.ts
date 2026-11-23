// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

afterEach(() => {
  vi.unstubAllGlobals();
});

it("distinguishes sharing being off from malformed status and creation responses", async () => {
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(Response.json({ token: null }))
    .mockResolvedValueOnce(Response.json({ token: 123 }))
    .mockResolvedValueOnce(Response.json({ token: null }));
  vi.stubGlobal("fetch", fetchMock);
  const client = new ApiClient("https://example.test");

  await expect(client.getIssueShare("workspace", "issue")).resolves.toEqual({ token: null });
  await expect(client.getIssueShare("workspace", "issue")).rejects.toThrow("Malformed issue share response");
  await expect(client.createIssueShare("workspace", "issue")).rejects.toThrow("Malformed issue share creation response");
});

it("projects only the shared fields and keeps the token out of the request URL", async () => {
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(Response.json({
      identifier: "DEV-42",
      title: "Review the release checklist",
      description: null,
      status: "done",
      workspace_id: "private",
    }))
    .mockResolvedValueOnce(Response.json({
      identifier: "DEV-42",
      title: "Review the release checklist",
      status: "done",
      comments: [{
        id: "comment", author_type: "agent", content: "The export passed.",
        created_at: "2026-11-23T07:00:00+00:00", author_id: "private",
        author_name: "Export verifier", author_avatar_url: "emoji:🦊", parent_id: null, source_task_id: "run",
      }],
      runs: [{
        id: "run", status: "completed", completed_at: "2026-11-23T07:00:00+00:00",
        agent_name: "Export verifier", agent_avatar_url: "emoji:🦊", result: { output: "private" },
      }],
      deliverables: [{
        id: "attachment", filename: "report.csv", content_type: "text/csv", size: 256,
        storage_key: "private",
      }],
    }))
    .mockResolvedValueOnce(Response.json({
      identifier: "DEV-42", title: "Review the release checklist", status: "done",
      comments: [{ id: "comment", author_type: "agent", content: "The export passed.", created_at: "invalid" }],
    }))
    .mockResolvedValueOnce(Response.json({ title: 345 }));
  vi.stubGlobal("fetch", fetchMock);
  const client = new ApiClient("https://example.test");
  const token = "0123456789abcdef01234567";

  await expect(client.getSharedIssue(token)).resolves.toEqual({
    identifier: "DEV-42", title: "Review the release checklist", description: "", status: "done",
    comments: [], runs: [], deliverables: [],
  });
  await expect(client.getSharedIssue(token)).resolves.toEqual({
    identifier: "DEV-42", title: "Review the release checklist", description: "", status: "done",
    comments: [{
      id: "comment", author_type: "agent", content: "The export passed.", created_at: "2026-11-23T07:00:00+00:00",
      author_name: "Export verifier", author_avatar_url: "emoji:🦊", parent_id: null, source_task_id: "run",
    }],
    runs: [{
      id: "run", status: "completed", completed_at: "2026-11-23T07:00:00+00:00",
      agent_name: "Export verifier", agent_avatar_url: "emoji:🦊",
    }],
    deliverables: [{ id: "attachment", filename: "report.csv", content_type: "text/csv", size: 256 }],
  });
  expect(fetchMock.mock.calls[0]?.[0]).toBe("https://example.test/api/share/issue");
  expect(fetchMock.mock.calls[0]?.[1].headers).toHaveProperty("X-Issue-Share-Token", token);
  await expect(client.getSharedIssue(token)).rejects.toThrow("Malformed shared issue response");
  await expect(client.getSharedIssue(token)).rejects.toThrow("Malformed shared issue response");
});

it("requests transcript pages and deliverable bytes inside the authenticated share", async () => {
  const message = { seq: 4, type: "text", content: "Checked the export.", created_at: "2026-11-23T07:00:00Z" };
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(Response.json({ messages: [{ ...message, metadata: "private" }], next_after: 4 }))
    .mockResolvedValueOnce(Response.json({ messages: [], next_after: "invalid" }))
    .mockResolvedValueOnce(new Response("result\npass", { headers: { "Content-Type": "text/csv" } }));
  vi.stubGlobal("fetch", fetchMock);
  const client = new ApiClient("https://example.test");
  const token = "0123456789abcdef01234567";

  await expect(client.listSharedIssueRunMessages(token, "run")).resolves.toEqual({ messages: [message], next_after: 4 });
  await expect(client.listSharedIssueRunMessages(token, "run", 4)).rejects.toThrow("Malformed shared issue run messages response");
  await expect((await client.getSharedIssueDeliverable(token, "attachment")).text()).resolves.toBe("result\npass");
  expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
    "https://example.test/api/share/issue/runs/run/messages",
    "https://example.test/api/share/issue/runs/run/messages?after=4",
    "https://example.test/api/share/issue/deliverables/attachment",
  ]);
  for (const [, init] of fetchMock.mock.calls) {
    expect(init).toEqual(expect.objectContaining({ cache: "no-store", redirect: "error" }));
    expect(init.headers).toHaveProperty("X-Issue-Share-Token", token);
  }
});
