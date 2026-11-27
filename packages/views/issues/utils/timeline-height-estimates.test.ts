// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { AgentTask, TimelineEntry } from "@multica/core/types";
import type { CommentRun } from "../components/comment-runs";
import {
  estimateMarkdownHeight,
  estimateTimelineItemHeights,
  timelineCharsPerLine,
} from "./timeline-height-estimates";
import type { TimelineFolds, TimelineRowItem } from "./timeline-row-content";
import { MAX_MARKDOWN_CHARS } from "../../rich-content/markdown-limit";

function comment(id: string, content: string, overrides: Partial<TimelineEntry> = {}): TimelineEntry {
  return { id, type: "comment", actor_type: "member", actor_id: "user", created_at: "2026-09-07T00:00:00Z", content, ...overrides };
}
const OPEN: TimelineFolds = { expandedResolved: new Set(), collapsed: new Set() };
const options = { charsPerLine: 80, activityLines: () => 0 };

describe("estimateMarkdownHeight", () => {
  it("wraps prose at the column width", () => {
    const short = estimateMarkdownHeight("a".repeat(80), 80);
    expect(estimateMarkdownHeight("a".repeat(240), 80)).toBeGreaterThan(2.5 * short - 13);
    expect(estimateMarkdownHeight("a".repeat(240), 160)).toBeLessThan(estimateMarkdownHeight("a".repeat(240), 80));
  });

  it("counts code, table and heading lines without wrapping them as prose", () => {
    const code = estimateMarkdownHeight(["```ts", ...Array(10).fill("x".repeat(200)), "```"].join("\n"), 80);
    const prose = estimateMarkdownHeight(Array(10).fill("x".repeat(200)).join("\n"), 80);
    expect(code).toBeLessThan(prose);
    // The delimiter row is not a row.
    expect(estimateMarkdownHeight("| a | b |\n| --- | --- |\n| 1 | 2 |", 80))
      .toBe(estimateMarkdownHeight("| a | b |\n| 1 | 2 |", 80));
  });

  it("sizes a body too long to format as its bounded plain-text box", () => {
    const atLimit = estimateMarkdownHeight("x\n".repeat(MAX_MARKDOWN_CHARS / 2), 80);
    const past = estimateMarkdownHeight("x\n".repeat(MAX_MARKDOWN_CHARS), 80);
    expect(past).toBeLessThan(1_000);
    expect(past).toBeLessThan(atLimit);
  });
});

describe("estimateTimelineItemHeights", () => {
  const long = Array(40).fill("A long agent reply paragraph that wraps over a couple of lines at most widths.").join("\n\n");
  const root = comment("root", "Question");
  const reply = comment("reply", long, { parent_id: root.id, created_at: "2026-09-07T00:01:00Z" });
  const replies = new Map([[root.id, [reply]]]);

  it("sizes a thread by its content and an activity line as a line", () => {
    const items: TimelineRowItem[] = [
      { kind: "activity-group", id: "activity" },
      { kind: "comment", id: root.id, entry: root },
    ];
    const [activity, thread] = estimateTimelineItemHeights(items, replies, new Map(), OPEN, options);
    expect(activity).toBeLessThan(50);
    expect(thread).toBeGreaterThan(40 * 30);
  });

  it("grows an open activity block with its lines", () => {
    const items: TimelineRowItem[] = [{ kind: "activity-group", id: "activity" }];
    const [collapsed] = estimateTimelineItemHeights(items, new Map(), new Map(), OPEN, options);
    const [open] = estimateTimelineItemHeights(items, new Map(), new Map(), OPEN, { ...options, activityLines: () => 5 });
    expect(open).toBeGreaterThan(collapsed! + 100);
  });

  it("sizes a folded thread by what stays on screen", () => {
    const thread: TimelineRowItem[] = [{ kind: "comment", id: root.id, entry: root }];
    const [collapsed] = estimateTimelineItemHeights(thread, replies, new Map(), { ...OPEN, collapsed: new Set([root.id]) }, options);
    const [bar] = estimateTimelineItemHeights([{ kind: "resolved-bar", id: root.id, entry: root }], replies, new Map(), OPEN, options);
    expect(collapsed).toBeLessThan(100);
    expect(bar).toBeLessThan(100);
  });

  it("adds a run's output to its block", () => {
    const task = { id: "run", status: "completed", result: { comment: long } } as unknown as AgentTask;
    const run: CommentRun = { task, hasReply: false };
    const [withOutput] = estimateTimelineItemHeights([{ kind: "run", id: "run", run }], new Map(), new Map(), OPEN, options);
    const [plain] = estimateTimelineItemHeights([{ kind: "run", id: "run", run: { ...run, task: { ...task, result: null } } }],
      new Map(), new Map(), OPEN, options);
    expect(plain).toBeLessThan(100);
    expect(withOutput).toBeGreaterThan(plain! + 40 * 30);
  });
});

describe("timelineCharsPerLine", () => {
  it("follows the column width up to the reading column's cap", () => {
    expect(timelineCharsPerLine(756)).toBeGreaterThan(timelineCharsPerLine(416));
    expect(timelineCharsPerLine(4000)).toBe(timelineCharsPerLine(3000));
    expect(timelineCharsPerLine(0)).toBeGreaterThan(0);
  });
});
