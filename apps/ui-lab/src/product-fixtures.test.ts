// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { createFixtureApi, issues } from "./product-fixtures";
import type { LabLocale } from "./locale";
import type { IssueTableQuerySpec } from "@multica/core/types";

const query: IssueTableQuerySpec = {
  scope: { kind: "workspace" },
  filters: {},
  sort: { field: "position", direction: "asc" },
};
afterEach(() => vi.restoreAllMocks());
describe("the UI Lab data boundary", () => {
  it("isolates edits between preview instances", async () => {
    const modified = createFixtureApi();
    const original = createFixtureApi();
    await modified.updateIssue(issues[0]!.id, { title: "Edited locally" });
    expect((await modified.getIssue(issues[0]!.id)).title).toBe(
      "Edited locally",
    );
    expect((await original.getIssue(issues[0]!.id)).title).toBe(
      issues[0]!.title,
    );
  });
  it("serves the status branches requested by the production list", async () => {
    const api = createFixtureApi();
    const response = await api.listIssueTableRows({
      query,
      group: { kind: "status" },
      group_key: "status:todo",
      hierarchy: { enabled: false },
      parent_id: null,
    });
    expect(response.rows.length).toBeGreaterThan(0);
    expect(response.rows.every((row) => row.issue.status === "todo")).toBe(
      true,
    );
    expect(response.branch_total).toBe(response.rows.length);
  });
  it("keeps comments local and available through the real timeline query", async () => {
    const api = createFixtureApi();
    const other = createFixtureApi();
    const comment = await api.createComment(issues[0]!.id, "Local comment");
    expect(
      (await api.listTimeline(issues[0]!.id)).some(
        (row) => row.id === comment.id,
      ),
    ).toBe(true);
    expect(
      (await other.listTimeline(issues[0]!.id)).some(
        (row) => row.id === comment.id,
      ),
    ).toBe(false);
  });
  it("translates sample content without replacing local edits or comments", async () => {
    let locale: LabLocale = "en";
    const api = createFixtureApi(() => locale);
    const first = issues[0]!.id;
    const second = issues[1]!.id;
    await api.updateIssue(first, { title: "Custom title", status: "done" });
    const comment = await api.createComment(first, "My comment");
    locale = "zh";
    expect((await api.getIssue(first)).title).toBe("Custom title");
    expect((await api.getIssue(first)).status).toBe("done");
    expect((await api.getIssue(second)).title).toBe("优化命令菜单的键盘导航");
    expect((await api.getIssue(first)).description).toContain("## 目标");
    expect(
      (await api.listComments(first)).find((row) => row.id === comment.id)
        ?.content,
    ).toBe("My comment");
    const response = await api.listIssueTableRows({
      query: { ...query, search: "键盘导航" },
      group_key: null,
      group: { kind: "status" },
      hierarchy: { enabled: false },
      parent_id: null,
    });
    expect(response.rows.map((row) => row.issue.id)).toEqual([second]);
    locale = "en";
    expect((await api.getIssue(second)).title).toBe(issues[1]!.title);
    expect((await api.getIssue(first)).title).toBe("Custom title");
  });
  it("rejects unconfigured operations instead of reaching the network", async () => {
    const network = vi.spyOn(globalThis, "fetch");
    vi.spyOn(console, "warn").mockImplementation(() => {});
    await expect(createFixtureApi().listSkills()).rejects.toThrow("UI Lab");
    expect(network).not.toHaveBeenCalled();
  });
});
