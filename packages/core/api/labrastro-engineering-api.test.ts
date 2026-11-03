// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

afterEach(() => vi.unstubAllGlobals());

it("sends exact reviewed revision IDs and workspace, and does not retry a conflict", async () => {
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: "candidate closed", code: "baseline_candidate_closed" }), { status: 409 }));
  vi.stubGlobal("fetch", fetcher);
  const api = new ApiClient("https://example.test");
  await expect(api.decideProjectBaseline("workspace", "project/id", "adopt", [{ revisionId: "reviewed", expectedAdoptedRevisionId: "base" }])).rejects.toMatchObject({ status: 409 });
  expect(fetcher).toHaveBeenCalledTimes(1);
  const [url, init] = fetcher.mock.calls[0] as [string, RequestInit];
  expect(url).toBe("https://example.test/api/projects/project%2Fid/baseline/decisions");
  expect(new Headers(init.headers).get("X-Workspace-ID")).toBe("workspace");
  expect(JSON.parse(String(init.body))).toEqual({ action: "adopt", revisions: [{ revision_id: "reviewed", expected_adopted_revision_id: "base" }] });
});

it("returns null for malformed baseline responses through the installed client", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ engineering: null, chapters: [] }), { status: 200, headers: { "Content-Type": "application/json" } })));
  expect(await new ApiClient("https://example.test").getProjectEngineering("workspace", "project")).toBeNull();
});

it("sends stages as a replacement array and omits unspecified fields", async () => {
  const id = "00000000-0000-4000-8000-000000000001";
  const fetcher = vi.fn().mockImplementation(() => Promise.resolve(new Response(JSON.stringify({ engineering: { project_id: id, workspace_id: id, stages: [] } }))));
  vi.stubGlobal("fetch", fetcher);
  const api = new ApiClient("https://example.test");
  const stages = [{ stage: 9, title: "Release" }];
  await api.updateProjectEngineering(id, id, { stages, currentStage: 9 });
  await api.updateProjectEngineering(id, id, { stages: [] });
  await api.updateProjectEngineering(id, id, { modeSummary: "Iterate" });
  expect(fetcher.mock.calls.map((call) => JSON.parse(call[1].body))).toEqual([
    { stages, current_stage: 9 }, { stages: [] }, { mode_summary: "Iterate" },
  ]);
});
