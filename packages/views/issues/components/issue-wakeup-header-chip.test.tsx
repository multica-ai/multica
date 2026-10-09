import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import type { IssueWakeup, SystemWakeup } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { IssueWakeupHeaderChip, primaryWakeup } from "./issue-wakeup-header-chip";

let rules: IssueWakeup[] = [];
let system: SystemWakeup[] = [];
vi.mock("./wakeup-condition-names", () => ({
  useConditionNames: () => ({ status: (key: string) => key, label: () => undefined, property: () => undefined, actor: (_type: string, id: string) => id }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/issues", () => ({
  issueWakeupsOptions: () => ({ queryKey: ["wakeups"] }),
  issueSystemWakeupsOptions: () => ({ queryKey: ["system"] }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: string[] }) => ({ data: queryKey[0] === "wakeups" ? rules : system }),
}));
vi.mock("../../common/use-viewing-timezone", () => ({ useViewingTimezone: () => "UTC" }));

const rule = (patch: Partial<IssueWakeup>): IssueWakeup => ({
  id: "w", issue_id: "issue", agent_id: "a", agent_name: "Emacs", instruction: "", kind: "event", mode: "once",
  event_types: ["comment.created"], filter_agent_id: null, filter_task_id: null, interval_seconds: null,
  cron_expression: null, timezone: "UTC", next_fire_at: null, enabled: true, disabled_at: null,
  last_task_id: null, last_error: null, ...patch,
});
const childDone: SystemWakeup = {
  id: "rule", revision: 1, default_instruction: "", customized: false, paused_reason: null,
  rule: "child_done", enabled: true, instruction: "", staged: true, stage: 2, total: 2, remaining: 1,
  waiting: [], target: { type: "agent", id: "a", name: "Grok" }, blocked: "", workspace_default: true,
};

beforeEach(() => {
  rules = [];
  system = [];
});

describe("primaryWakeup", () => {
  it("prefers a waiting rule, then the soonest schedule, then the system rule", () => {
    const later = rule({ id: "later", kind: "at", next_fire_at: "2030-01-02T00:00:00Z" });
    const sooner = rule({ id: "sooner", kind: "at", next_fire_at: "2030-01-01T00:00:00Z" });
    const reply = rule({ id: "reply" });
    expect(primaryWakeup([later, sooner, reply], [childDone])).toMatchObject({ kind: "rule", rule: { id: "reply" }, count: 4 });
    expect(primaryWakeup([later, sooner], [])).toMatchObject({ rule: { id: "sooner" }, count: 2 });
    expect(primaryWakeup([], [childDone])).toMatchObject({ kind: "system", count: 1 });
    expect(primaryWakeup([], [{ ...childDone, blocked: "backlog" }])).toBeNull();
  });

  it("falls back to a paused rule only when nothing else waits", () => {
    const paused = rule({ enabled: false, paused_reason: "loop" });
    expect(primaryWakeup([paused], [])).toMatchObject({ kind: "paused" });
    expect(primaryWakeup([paused, rule({ id: "live" })], [])).toMatchObject({ kind: "rule", rule: { id: "live" } });
  });
});

describe("IssueWakeupHeaderChip", () => {
  it("says who waits for what, counts the rest and opens the wakeups", () => {
    rules = [rule({ filter_actor_type: "member", filter_actor_id: "u", filter_actor_name: "Jiayuan" }), rule({ id: "w2", kind: "at", next_fire_at: "2030-01-01T00:00:00Z" })];
    system = [childDone];
    const onOpen = vi.fn();
    renderWithI18n(<IssueWakeupHeaderChip issueId="issue" onOpen={onOpen} />, { locale: "zh-Hans" });
    const chip = screen.getByRole("button", { name: /Emacs 在等 Jiayuan 回复 \+2/ });
    expect(chip).toHaveTextContent("Emacs 在等 Jiayuan 回复+2");
    fireEvent.click(chip);
    expect(onOpen).toHaveBeenCalled();
  });

  it("names the system rule's target stage", () => {
    system = [childDone];
    renderWithI18n(<IssueWakeupHeaderChip issueId="issue" onOpen={() => {}} />, { locale: "zh-Hans" });
    expect(screen.getByRole("button", { name: /Grok 在等第 2 阶段完成/ })).toBeVisible();
  });

  it("renders nothing when the issue waits for nothing", () => {
    const { container } = renderWithI18n(<IssueWakeupHeaderChip issueId="issue" onOpen={() => {}} />);
    expect(container).toBeEmptyDOMElement();
  });
});
