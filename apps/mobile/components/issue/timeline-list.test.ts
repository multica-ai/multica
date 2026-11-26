// @vitest-environment node
import type { ReactElement } from "react";
import type { Issue, TimelineEntry } from "@multica/core/types";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TimelineList } from "./timeline-list";

// Mobile has no React renderer, so a minimal hook runner drives the real
// component: state, refs, memos and effects with dependency checks, and
// re-renders until state settles.
const runner = vi.hoisted(() => {
  type Slot = { value?: unknown; deps?: readonly unknown[]; cleanup?: (() => void) | void };
  const slots: Slot[] = [];
  let cursor = 0;
  let dirty = false;
  let pending: { slot: Slot; effect: () => (() => void) | void }[] = [];

  const changed = (slot: Slot, deps?: readonly unknown[]) =>
    !deps || !slot.deps || deps.length !== slot.deps.length || deps.some((dep, i) => !Object.is(dep, slot.deps![i]));
  const next = (): Slot => (slots[cursor++] ??= {});

  const hooks = {
    useState<T>(initial: T | (() => T)) {
      const slot = next();
      if (!("value" in slot)) slot.value = typeof initial === "function" ? (initial as () => T)() : initial;
      const set = (update: T | ((prev: T) => T)) => {
        const value = typeof update === "function" ? (update as (prev: T) => T)(slot.value as T) : update;
        if (Object.is(value, slot.value)) return;
        slot.value = value;
        dirty = true;
      };
      return [slot.value as T, set] as const;
    },
    useRef<T>(initial: T) {
      const slot = next();
      if (!("value" in slot)) slot.value = { current: initial };
      return slot.value as { current: T };
    },
    useMemo<T>(create: () => T, deps: readonly unknown[]) {
      const slot = next();
      if (changed(slot, deps)) {
        slot.value = create();
        slot.deps = deps;
      }
      return slot.value as T;
    },
    useCallback<T>(callback: T, deps: readonly unknown[]) {
      return hooks.useMemo(() => callback, deps);
    },
    useEffect(effect: () => (() => void) | void, deps?: readonly unknown[]) {
      const slot = next();
      if (!changed(slot, deps)) return;
      slot.deps = deps;
      pending.push({ slot, effect });
    },
  };

  function render<P>(component: (props: P) => unknown, props: P) {
    let output: unknown;
    for (let pass = 0; pass < 10; pass++) {
      cursor = 0;
      dirty = false;
      output = component(props);
      const effects = pending;
      pending = [];
      for (const { slot, effect } of effects) {
        slot.cleanup?.();
        slot.cleanup = effect();
      }
      if (!dirty) return output;
    }
    throw new Error("render did not settle");
  }

  function reset() {
    for (const slot of slots) slot.cleanup?.();
    slots.length = 0;
    pending = [];
  }

  return { hooks, render, reset };
});

vi.mock("react", async (importOriginal) => ({
  ...await importOriginal<typeof import("react")>(),
  ...runner.hooks,
}));
vi.mock("react-native", () => ({
  ActivityIndicator: "ActivityIndicator", Pressable: "Pressable", RefreshControl: "RefreshControl", View: "View",
}));
vi.mock("@shopify/flash-list", () => ({ FlashList: "FlashList" }));
vi.mock("@expo/vector-icons", () => ({ Ionicons: "Ionicons" }));
vi.mock("@tanstack/react-query", () => ({ useQuery: () => ({ data: [] }) }));
vi.mock("@/components/ui/text", () => ({ Text: "Text" }));
vi.mock("./issue-header-card", () => ({ IssueHeaderCard: "IssueHeaderCard" }));
vi.mock("./issue-description", () => ({ IssueDescription: "IssueDescription" }));
vi.mock("./issue-reaction-row", () => ({ IssueReactionRow: "IssueReactionRow" }));
vi.mock("./activity-row", () => ({ ActivityRow: "ActivityRow" }));
vi.mock("./comment-card", () => ({ CommentCard: "CommentCard" }));
vi.mock("@/data/stores/last-viewed-store", () => ({
  useLastViewedStore: Object.assign(
    (select: (state: unknown) => unknown) => select({ markViewed: vi.fn() }),
    { getState: () => ({ getLastViewed: () => undefined }) },
  ),
}));
vi.mock("@/lib/markdown/image-sequence", () => ({ ImageSequenceProvider: "ImageSequenceProvider" }));
vi.mock("@/data/queries/issues", () => ({ issueAttachmentsOptions: () => ({}) }));
vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: (select: (state: unknown) => unknown) => select({ currentWorkspaceId: "workspace-1" }),
}));
vi.mock("@/lib/use-color-scheme", () => ({ useColorScheme: () => ({ colorScheme: "light" }) }));
vi.mock("@/lib/theme", () => ({ THEME: { light: {} } }));
vi.mock("@/lib/i18n", () => ({ useT: () => ({ t: (key: string) => key }) }));
vi.mock("@/data/comment-select-store", () => ({
  useCommentSelectStore: (select: (state: unknown) => unknown) => select({ selectingId: null }),
}));

type Element = ReactElement<{ children?: unknown; data?: unknown[]; renderItem?: (info: { item: unknown }) => Element; highlightedCommentId?: string | null }>;

const root: TimelineEntry = {
  type: "comment", id: "root", actor_type: "member", actor_id: "user-1", content: "Earlier comment",
  parent_id: null, created_at: "2026-11-15T17:00:00Z", updated_at: "2026-11-15T17:00:00Z",
};
const notice: TimelineEntry = {
  ...root, id: "failure-notice", actor_type: "agent", actor_id: "agent-1", comment_type: "system",
  source_task_id: "task-1", parent_id: "root", created_at: "2026-11-15T17:01:00Z",
};

function findList(node: unknown): Element | undefined {
  if (!node || typeof node !== "object") return undefined;
  if (Array.isArray(node)) {
    for (const child of node) {
      const found = findList(child);
      if (found) return found;
    }
    return undefined;
  }
  const element = node as Element;
  if (element.type === "FlashList") return element;
  return findList(element.props?.children);
}

function open(entries: TimelineEntry[], nonce = "tap-1") {
  const tree = runner.render(TimelineList, {
    issue: { id: "issue-1", identifier: "MUL-1", description: "" } as Issue,
    entries,
    timelineLoading: false,
    refreshing: false,
    onRefresh: () => {},
    highlightCommentId: "task-1",
    highlightNonce: nonce,
  });
  const list = findList(tree)!;
  return {
    rows: list.props.data!.length,
    highlight: list.props.renderItem!({ item: list.props.data![0] }).props.highlightedCommentId,
  };
}

describe("TimelineList deep-link highlight", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => {
    runner.reset();
    vi.useRealTimers();
  });

  it("highlights a failure notice that arrives in a thread after a cached timeline opens", () => {
    expect(open([root])).toEqual({ rows: 1, highlight: null });
    expect(open([root, notice])).toEqual({ rows: 1, highlight: "failure-notice" });
  });

  it("highlights once per opening", () => {
    expect(open([root, notice]).highlight).toBe("failure-notice");
    vi.advanceTimersByTime(5000);
    expect(open([root, notice]).highlight).toBeNull();
    expect(open([root, { ...notice }]).highlight).toBeNull();
    expect(open([root, notice], "tap-2").highlight).toBe("failure-notice");
  });
});
