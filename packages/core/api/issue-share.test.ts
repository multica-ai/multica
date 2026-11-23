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
      }],
      runs: [{
        id: "run", status: "completed", completed_at: "2026-11-23T07:00:00+00:00",
        result: { output: "private" },
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
    comments: [], runs: [],
  });
  await expect(client.getSharedIssue(token)).resolves.toEqual({
    identifier: "DEV-42", title: "Review the release checklist", description: "", status: "done",
    comments: [{ id: "comment", author_type: "agent", content: "The export passed.", created_at: "2026-11-23T07:00:00+00:00" }],
    runs: [{ id: "run", status: "completed", completed_at: "2026-11-23T07:00:00+00:00" }],
  });
  expect(fetchMock.mock.calls[0]?.[0]).toBe("https://example.test/api/share/issue");
  expect(fetchMock.mock.calls[0]?.[1].headers).toHaveProperty("X-Issue-Share-Token", token);
  await expect(client.getSharedIssue(token)).rejects.toThrow("Malformed shared issue response");
  await expect(client.getSharedIssue(token)).rejects.toThrow("Malformed shared issue response");
});
