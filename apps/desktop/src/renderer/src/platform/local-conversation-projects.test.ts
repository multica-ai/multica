// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { LocalFolderProject } from "@multica/core/local-conversation/protocol";
import { localV2SummarySchema } from "@multica/core/local-conversation/v2";
import { compareLocalConversations, groupLocalConversations, projectForDirectory, sessionEntry, type LocalSessionEntry } from "./local-conversation-projects";

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
  it("orders by send time through late output, completion, and a new turn while keeping pinned and archive boundaries", () => {
    const row = (id: string, sent: string, activity = sent, pinned = false, archived = false) => sessionEntry(localV2SummarySchema.parse({
      conversation_id: id, summary_seq: 1, provider: "codex", title: id, cwd: "/repo-a", status: "idle", pinned, archived,
      turn_count: 1, last_turn_sent_at: sent, updated_at: activity,
    }));
    const a = row("a", "2026-11-03T09:00:00Z", "2026-11-03T09:02:00Z");
    const b = row("b", "2026-11-03T09:01:00Z");
    const pinned = row("pinned", "2026-11-02T00:00:00Z", undefined, true);
    const archived = row("archived", "2026-11-03T10:00:00Z", undefined, false, true);
    const ids = (rows: LocalSessionEntry[], archive = false) => groupLocalConversations(rows.sort(compareLocalConversations), projects, archive, "")[0]!.sessions.map((entry) => entry.id);
    expect(ids([a, b, pinned, archived])).toEqual(["pinned", "b", "a"]);
    expect(ids([a, b, pinned, archived], true)).toEqual(["archived"]);
    expect(ids([{ ...a, lastTurnSentAt: "2026-11-03T09:03:00Z" }, b, pinned])).toEqual(["pinned", "a", "b"]);
  });
  it("compares instants with fractional seconds and offsets, with deterministic ties", () => {
    const rows = [
      { ...session("a"), lastTurnSentAt: "2026-11-03T09:00:00Z" },
      { ...session("b"), lastTurnSentAt: "2026-11-03T09:00:00.100Z" },
      { ...session("c"), lastTurnSentAt: "2026-11-03T10:00:00.100+01:00" },
    ];
    expect(rows.sort(compareLocalConversations).map((entry) => entry.id)).toEqual(["b", "c", "a"]);
  });
  it("preserves nanosecond send order within a millisecond", () => {
    const rows = [
      { ...session("a"), lastTurnSentAt: "2026-11-06T09:00:00Z" },
      { ...session("b"), lastTurnSentAt: "2026-11-06T09:00:00.000000001Z" },
      { ...session("c"), lastTurnSentAt: "2026-11-06T09:00:00.000000100Z" },
    ];
    expect(rows.sort(compareLocalConversations).map((entry) => entry.id)).toEqual(["c", "b", "a"]);
    expect(compareLocalConversations(
      { ...session("a"), lastTurnSentAt: "2026-11-06T09:00:00.1Z" },
      { ...session("b"), lastTurnSentAt: "2026-11-06T10:00:00.100000000+01:00" },
    )).toBeLessThan(0);
  });
  it("uses activity time only for older daemons and creation time for an explicit empty user history", () => {
    const summary = localV2SummarySchema.parse({ conversation_id: "c", summary_seq: 1, provider: "claude", title: "", status: "idle", pinned: false, archived: false,
      created_at: "2026-11-01T00:00:00Z", updated_at: "2026-11-03T00:00:00Z" });
    expect(sessionEntry(summary).lastTurnSentAt).toBe(summary.updated_at);
    expect(sessionEntry({ ...summary, last_turn_sent_at: null }).lastTurnSentAt).toBe(summary.created_at);
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
