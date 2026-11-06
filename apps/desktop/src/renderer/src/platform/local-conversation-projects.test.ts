// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { LocalFolderProject } from "@multica/core/local-conversation/protocol";
import { groupLocalConversations, projectForDirectory, type LocalSessionEntry } from "./local-conversation-projects";

const projects = [{ id: "a", title: "Shared title", path: "/repo-a" }, { id: "b", title: "Shared title", path: "/repo-b" }] as LocalFolderProject[];
const session = (id: string, cwd = "/unregistered", archived = false) => ({ id, title: id, provider: "codex", cwd, archived, pinned: false, projectId: null, running: false, waitingForAnswer: false, preview: "", turnCount: 0, updatedAt: "" }) as LocalSessionEntry;
describe("local conversation folder grouping", () => {
  const defaultProject = { id: "unassigned:1", title: "未分配项目", path: "/profile/projects/1/unassigned", createdAt: "2026-11-06" };
  it("shows the bound default folder in a fresh scope without a duplicate unassigned group", () => {
    expect(groupLocalConversations([], [defaultProject], false, "")).toEqual([{ id: null, project: defaultProject, sessions: [] }]);
    expect(groupLocalConversations([], [defaultProject], true, "")).toEqual([]);
    expect(groupLocalConversations([], [defaultProject], false, "missing")).toEqual([]);
  });
  it("keeps legacy directories unchanged in the same default group while matching explicit projects", () => {
    const rows = [session("default", defaultProject.path), session("legacy", "/old/private"), session("explicit", "/repo-a")];
    const original = structuredClone(rows);
    const groups = groupLocalConversations(rows, [defaultProject, ...projects], false, "");
    expect(groups.map((group) => [group.id, group.sessions.map((entry) => entry.id)])).toEqual([["a", ["explicit"]], ["b", []], [null, ["default", "legacy"]]]);
    expect(groups.at(-1)?.project).toBe(defaultProject);
    expect(rows).toEqual(original);
  });
  it("groups by working directory, preserves session ordering, and puts unregistered directories last", () => {
    const groups = groupLocalConversations([session("b-pinned", "/repo-b"), session("legacy"), session("a", "/repo-a"), session("b-recent", "/repo-b/"), session("cleared"), session("archived", "/repo-a", true)], projects, false, "");
    expect(groups.map((group) => [group.id, group.sessions.map((entry) => entry.id)])).toEqual([["a", ["a"]], ["b", ["b-pinned", "b-recent"]], [null, ["legacy", "cleared"]]]);
  });
  it("ignores cloud task project IDs without changing stored task associations or working directories", () => {
    const linked = { ...session("linked", "/repo-b"), projectId: "a" };
    const original = structuredClone(linked);
    const groups = groupLocalConversations([linked], projects, false, "");
    expect(groups.find((group) => group.id === "b")?.sessions).toEqual([linked]);
    expect(groups.find((group) => group.id === "a")?.sessions).toEqual([]);
    expect(linked).toEqual(original);
    expect(projectForDirectory(projects, "/repo-b/")?.id).toBe("b");
  });
  it("keeps empty workspace projects available after loading while archives only show projects with archived sessions", () => {
    expect(groupLocalConversations([], projects, false, "").map((group) => [group.id, group.sessions.length])).toEqual([["a", 0], ["b", 0], [null, 0]]);
    expect(groupLocalConversations([], projects, true, "")).toEqual([]);
  });
  it("searches project names while preserving archive filtering and hiding empty search groups", () => {
    const rows = [session("active", "/repo-a"), session("archived", "/repo-a", true), session("other")];
    expect(groupLocalConversations(rows, projects, false, "shared").map((group) => group.sessions.map((entry) => entry.id))).toEqual([["active"]]);
    expect(groupLocalConversations(rows, projects, true, "shared")[0]?.sessions[0]?.id).toBe("archived");
    expect(groupLocalConversations(rows, projects, false, "missing")).toEqual([]);
    expect(groupLocalConversations(rows, projects, false, "/repo-a")[0]?.sessions[0]?.id).toBe("active");
  });
});
