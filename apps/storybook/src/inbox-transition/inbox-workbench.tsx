import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import {
  AlarmClock,
  Archive,
  ArrowDown,
  BellOff,
  Bot,
  Check,
  CheckCircle2,
  ChevronDown,
  Circle,
  CircleDot,
  Folder,
  Gauge,
  Inbox,
  MoreHorizontal,
  PanelRight,
  Pin,
  RotateCcw,
  Send,
  Sparkles,
  Undo2,
  X,
} from "@multica/ui/icons";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { MulticaBleedMark } from "@multica/ui/components/common/multica-graphics";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Kbd } from "@multica/ui/components/ui/kbd";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import type { Lang } from "../voice-input/copy";
import { useStoryEnv } from "../voice-input/env";
import { ACTORS, createSeedIssues, type DemoInboxIssue, type IssuePriority, type IssueStatus } from "./data";
import { DIRECTION_BY_ID, DIRECTIONS, type DirectionId } from "./directions";
import { DelayedCommentImage } from "./mock-images";
import "./inbox-transition.css";

type LatencyPreset = 0 | 250 | 600;

const COPY = {
  work: { zh: "工作", en: "Work" },
  pinned: { zh: "置顶", en: "Pinned" },
  inbox: { zh: "收件箱", en: "Inbox" },
  projects: { zh: "项目", en: "Projects" },
  inboxEmpty: { zh: "暂无通知", en: "No notifications" },
  archive: { zh: "归档", en: "Archive" },
  snooze: { zh: "延后", en: "Snooze" },
  unsubscribe: { zh: "取消订阅并归档", en: "Unsubscribe & archive" },
  markDone: { zh: "标记完成", en: "Mark Done" },
  assignAgent: { zh: "指派 Emacs", en: "Assign Emacs" },
  undo: { zh: "撤销", en: "Undo" },
  next: { zh: "下一条", en: "Next" },
  stayHere: { zh: "留在这里", en: "Stay here" },
  reset: { zh: "重置", en: "Reset" },
  slowMo: { zh: "慢动作 0.4x", en: "0.4x" },
  latencyLabel: { zh: "请求延迟", en: "Latency" },
  prefetchWarm: { zh: "下一条已预热", en: "Next pre-warmed" },
  prefetchCold: { zh: "下一条未预热（会冷加载）", en: "Next cold (not prefetched)" },
  prefetchWarming: { zh: "后台预热下一条中…", en: "Pre-warming next…" },
  clearedTitle: { zh: "收件箱已清空", en: "Inbox cleared" },
  clearedSub: { zh: "刚归档了", en: "Just archived" },
  snoozedSummary: { zh: "2 条延后通知 · 最早 明天 09:00 回来", en: "2 snoozed · next returns tomorrow 9:00 AM" },
  subIssues: { zh: "子任务", en: "Sub-issues" },
  activity: { zh: "动态与评论", en: "Activity" },
  targetTag: { zh: "通知定位评论", en: "Notification comment" },
  composerPlaceholder: { zh: "留下评论，输入 @ 唤起智能体...", en: "Leave a comment, type @ to trigger an agent..." },
  quickAgentReply: { zh: "快捷发送 @Emacs 评论（触发自动归档）", en: "Send @Emacs comment (auto-archive)" },
  send: { zh: "发送", en: "Send" },
  status: { zh: "状态", en: "Status" },
  priority: { zh: "优先级", en: "Priority" },
  assignee: { zh: "负责人", en: "Assignee" },
  project: { zh: "项目", en: "Project" },
  labels: { zh: "标签", en: "Labels" },
  running: { zh: "运行中", en: "Running" },
  bufferAdvancing: { zh: "即将打开下一条", en: "opening next" },
  bufferPaused: { zh: "已留在当前任务", en: "staying on this issue" },
  toastArchived: { zh: "已归档通知", en: "Notification archived" },
  toastSnoozed: { zh: "已延后至明天 09:00", en: "Snoozed until tomorrow 9:00 AM" },
  toastAutoArchived: { zh: "已自动归档通知", en: "Auto-archived notification" },
  doneCause: { zh: "已改为已完成 · 通知已归档", en: "Changed to Done · Notification archived" },
  assignCause: { zh: "已改派给 Emacs · 通知已归档", en: "Reassigned to Emacs · Notification archived" },
  commentCause: { zh: "已发送并唤醒 Emacs · 通知已归档", en: "Sent & woke Emacs · Notification archived" },
  loadingTimeline: { zh: "正在加载时间线与评论…", en: "Loading timeline & comments…" },
  shortcutsHint: {
    zh: "E 归档 · H 延后 · Z 撤销 · Esc 停住缓冲 · R 重置",
    en: "E archive · H snooze · Z undo · Esc stay · R reset",
  },
};

const STATUS_META: Record<IssueStatus, { label: Record<Lang, string>; color: string }> = {
  todo: { label: { zh: "待办", en: "Todo" }, color: "text-muted-foreground" },
  in_progress: { label: { zh: "进行中", en: "In Progress" }, color: "text-amber-500" },
  in_review: { label: { zh: "待审核", en: "In Review" }, color: "text-blue-500" },
  done: { label: { zh: "已完成", en: "Done" }, color: "text-emerald-500" },
};

const PRIORITY_LABEL: Record<IssuePriority, Record<Lang, string>> = {
  urgent: { zh: "紧急", en: "Urgent" },
  high: { zh: "高", en: "High" },
  medium: { zh: "中", en: "Medium" },
  low: { zh: "低", en: "Low" },
};

function StatusGlyph({ status, className }: { status: IssueStatus; className?: string }) {
  const color = STATUS_META[status].color;
  if (status === "done") return <CheckCircle2 className={cn("size-3.5 shrink-0", color, className)} />;
  if (status === "in_progress" || status === "in_review") return <CircleDot className={cn("size-3.5 shrink-0", color, className)} />;
  return <Circle className={cn("size-3.5 shrink-0", color, className)} />;
}

interface HistoryEntry {
  item: DemoInboxIssue;
  index: number;
  kind: "archived" | "snoozed";
}

interface BufferState {
  issueId: string;
  causeText: string;
  progress: number;
  paused: boolean;
}

type LoadStage =
  | "ready"
  | "current-bar-gone"
  | "current-skeleton"
  | "current-issue-only"
  | "current-top-blank-sidebar"
  | "steady-loading-body";

interface LeavingSnapshot {
  issue: DemoInboxIssue;
  direction: "next" | "prev";
  seq: number;
}

/** First 3 unread rows that production's `useInboxIssuePrefetch` warms on idle. */
function initialWarmSet(list: DemoInboxIssue[]): Set<string> {
  return new Set(list.filter((i) => !i.coldInCurrent).map((i) => i.id));
}

export function InboxWorkbench({
  initialDirection = "buffer",
  allowSwitchDirection = true,
  heightClass = "h-screen",
}: {
  initialDirection?: DirectionId;
  allowSwitchDirection?: boolean;
  heightClass?: string;
}) {
  const { lang } = useStoryEnv();
  const [direction, setDirection] = useState<DirectionId>(initialDirection);
  const [slowMo, setSlowMo] = useState(false);
  const [latencyMs, setLatencyMs] = useState<LatencyPreset>(250);
  const [sidebarOpen, setSidebarOpen] = useState(true);

  const [items, setItems] = useState<DemoInboxIssue[]>(() => createSeedIssues());
  const [selectedId, setSelectedId] = useState<string | null>("mul-9865");
  const [collapsingIds, setCollapsingIds] = useState<string[]>([]);
  const [enteringRowId, setEnteringRowId] = useState<string | null>(null);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const [toastMsg, setToastMsg] = useState<{ id: number; text: string } | null>(null);
  const [composerText, setComposerText] = useState("");

  // Realistic prefetch cache:
  // - `warmIds`: issues whose detail + timeline are in TanStack Query cache.
  // - `warmImageIds`: issues whose images have already been decoded in browser cache (only reused in B/C/D/E or repeat visits).
  const [warmIds, setWarmIds] = useState<Set<string>>(() => initialWarmSet(createSeedIssues()));
  const [warmImageIds, setWarmImageIds] = useState<Set<string>>(() => new Set(["mul-9865"]));
  const [warmingNextId, setWarmingNextId] = useState<string | null>(null);

  // Transition & network load state
  const [navSeq, setNavSeq] = useState(0);
  const [navDir, setNavDir] = useState<"next" | "prev" | "jump">("jump");
  const [loadStage, setLoadStage] = useState<LoadStage>("ready");
  const [buffer, setBuffer] = useState<BufferState | null>(null);
  const [leavingCard, setLeavingCard] = useState<LeavingSnapshot | null>(null);
  const [flashComment, setFlashComment] = useState(false);

  // Live telemetry so reviewers can verify scroll depth, cache hits, and image CLS
  const [telemetry, setTelemetry] = useState<{
    scrollTop: number;
    cacheHit: boolean;
    clsPx: number;
    loadMs: number;
  }>({ scrollTop: 0, cacheHit: true, clsPx: 0, loadMs: 0 });

  const scrollRef = useRef<HTMLDivElement | null>(null);
  const timersRef = useRef<number[]>([]);
  const bufferRafRef = useRef<number | null>(null);
  const toastTimerRef = useRef<number | null>(null);

  const motionScale = slowMo ? 2.5 : 1;

  const clearPendingTimers = useCallback(() => {
    for (const id of timersRef.current) window.clearTimeout(id);
    timersRef.current = [];
    if (bufferRafRef.current !== null) {
      window.cancelAnimationFrame(bufferRafRef.current);
      bufferRafRef.current = null;
    }
  }, []);

  const schedule = useCallback(
    (fn: () => void, ms: number) => {
      const id = window.setTimeout(fn, Math.round(ms * motionScale));
      timersRef.current.push(id);
      return id;
    },
    [motionScale],
  );

  const showToast = useCallback((text: string) => {
    if (toastTimerRef.current !== null) window.clearTimeout(toastTimerRef.current);
    setToastMsg({ id: Date.now(), text });
    toastTimerRef.current = window.setTimeout(() => setToastMsg(null), 6500);
  }, []);

  const resetWorkbench = useCallback(
    (nextDir?: DirectionId) => {
      clearPendingTimers();
      if (nextDir) setDirection(nextDir);
      const seed = createSeedIssues();
      setItems(seed);
      setSelectedId(seed[0]!.id);
      setCollapsingIds([]);
      setEnteringRowId(null);
      setHistory([]);
      setToastMsg(null);
      setComposerText("");
      setWarmIds(initialWarmSet(seed));
      setWarmImageIds(new Set([seed[0]!.id]));
      setWarmingNextId(null);
      setLoadStage("ready");
      setBuffer(null);
      setLeavingCard(null);
      setNavDir("jump");
      setNavSeq((s) => s + 1);
      setTelemetry({ scrollTop: 0, cacheHit: true, clsPx: 0, loadMs: 0 });
    },
    [clearPendingTimers],
  );

  useEffect(() => {
    setDirection(initialDirection);
  }, [initialDirection]);

  const visibleRows = items;
  const activeIssue = items.find((i) => i.id === selectedId) ?? null;

  const pickNextId = useCallback(
    (list: DemoInboxIssue[], removingId: string): string | null => {
      const idx = list.findIndex((i) => i.id === removingId);
      if (idx === -1) return list[0]?.id ?? null;
      for (let i = idx + 1; i < list.length; i++) {
        const candidate = list[i]!;
        if (!collapsingIds.includes(candidate.id)) return candidate.id;
      }
      for (let i = idx - 1; i >= 0; i--) {
        const candidate = list[i]!;
        if (!collapsingIds.includes(candidate.id)) return candidate.id;
      }
      return null;
    },
    [collapsingIds],
  );

  const nextIdForCurrent = selectedId ? pickNextId(visibleRows, selectedId) : null;
  const nextIsWarm = !nextIdForCurrent || latencyMs === 0 || warmIds.has(nextIdForCurrent);

  // Anchor prefetch in B / C / D / E: automatically warms `next` and `next+1` (including read rows!)
  // in the background after `latencyMs`, mirroring the real solution. Rapidly pressing E outruns it!
  useEffect(() => {
    if (direction === "current" || !selectedId || latencyMs === 0) {
      setWarmingNextId(null);
      return;
    }
    const nextId = pickNextId(items, selectedId);
    if (!nextId) {
      setWarmingNextId(null);
      return;
    }
    const nextNextId = pickNextId(
      items.filter((i) => i.id !== selectedId),
      nextId,
    );
    const pending: number[] = [];

    if (!warmIds.has(nextId)) {
      setWarmingNextId(nextId);
      pending.push(
        window.setTimeout(() => {
          setWarmIds((prev) => new Set(prev).add(nextId));
          setWarmImageIds((prev) => new Set(prev).add(nextId));
          setWarmingNextId((curr) => (curr === nextId ? null : curr));
        }, Math.round(latencyMs * 0.85 * motionScale)),
      );
    } else {
      setWarmingNextId(null);
    }

    if (nextNextId && !warmIds.has(nextNextId)) {
      pending.push(
        window.setTimeout(() => {
          setWarmIds((prev) => new Set(prev).add(nextNextId));
          setWarmImageIds((prev) => new Set(prev).add(nextNextId));
        }, Math.round(latencyMs * 1.65 * motionScale)),
      );
    }

    return () => {
      for (const id of pending) window.clearTimeout(id);
    };
  }, [direction, selectedId, items, latencyMs, warmIds, pickNextId, motionScale]);

  // Scroll landing alignment:
  // - In `current-issue-only` or `current-top-blank-sidebar`: deliberately paints scrollTop = 0 first!
  // - In `ready`: aligns to the deep target comment (~3,000px down) BEFORE paint on B/C/D/E,
  //   and teleports there on A once `ready` lands!
  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (!el || !activeIssue) return;
    if (
      loadStage === "current-issue-only" ||
      loadStage === "current-top-blank-sidebar" ||
      loadStage === "current-skeleton" ||
      loadStage === "steady-loading-body"
    ) {
      el.scrollTop = 0;
      setTelemetry((t) => ({ ...t, scrollTop: 0 }));
      return;
    }
    const target = el.querySelector<HTMLElement>("[data-target-comment='true']");
    if (target) {
      el.scrollTop = 0;
      const containerRect = el.getBoundingClientRect();
      const targetRect = target.getBoundingClientRect();
      const alignedTop = Math.max(0, Math.round(targetRect.top - containerRect.top - 110));
      el.scrollTop = alignedTop;
      setTelemetry((t) => ({ ...t, scrollTop: alignedTop }));
    } else {
      el.scrollTop = 0;
      setTelemetry((t) => ({ ...t, scrollTop: 0 }));
    }
  }, [activeIssue, navSeq, loadStage]);

  // Flash target comment once settled.
  useEffect(() => {
    if (!activeIssue || loadStage !== "ready") return;
    setFlashComment(true);
    const id = window.setTimeout(() => setFlashComment(false), Math.round(1600 * motionScale));
    return () => window.clearTimeout(id);
  }, [activeIssue, navSeq, loadStage, motionScale]);

  const handleUnreservedImageExpand = useCallback((heightPx: number) => {
    setTelemetry((t) => ({ ...t, clsPx: t.clsPx + heightPx }));
  }, []);

  const navigateToIssue = useCallback(
    (targetIssue: DemoInboxIssue | null, dir: "next" | "prev" | "jump", fromIssue: DemoInboxIssue | null) => {
      if (!targetIssue) {
        setSelectedId(null);
        setLoadStage("ready");
        setNavDir(dir);
        setNavSeq((s) => s + 1);
        setTelemetry({ scrollTop: 0, cacheHit: true, clsPx: 0, loadMs: 0 });
        return;
      }

      const effectiveLatency = latencyMs;
      const isWarm = effectiveLatency === 0 || warmIds.has(targetIssue.id);

      if (direction === "current") {
        // Production multi-stage cascade on real latency:
        // 1) Bar drops on old issue (-40px jump)
        setLoadStage("current-bar-gone");
        const barDropMs = Math.max(75, Math.round(effectiveLatency * 0.3));

        if (!isWarm) {
          // 2) Cold miss: Skeleton WITHOUT 40px InboxBar
          schedule(() => {
            setSelectedId(targetIssue.id);
            setLoadStage("current-skeleton");
            setNavDir(dir);
            setNavSeq((s) => s + 1);
            setTelemetry({ scrollTop: 0, cacheHit: false, clsPx: 0, loadMs: effectiveLatency });
          }, barDropMs);

          // 3) Issue detail JSON arrives (+40px bar pops back, right sidebar blanks, scrollTop=0, timeline still loading)
          schedule(() => {
            setLoadStage("current-issue-only");
          }, barDropMs + Math.round(effectiveLatency * 0.65));

          // 4) Timeline JSON arrives: still flashes top + blank sidebar briefly, then teleports 3,500px down!
          schedule(() => {
            setLoadStage("current-top-blank-sidebar");
          }, barDropMs + Math.round(effectiveLatency * 1.15));

          schedule(() => {
            setWarmIds((prev) => new Set(prev).add(targetIssue.id));
            setLoadStage("ready");
            setNavSeq((s) => s + 1);
          }, barDropMs + Math.round(effectiveLatency * 1.15) + 145);
        } else {
          // Warm cache in Current STILL suffers: -40px bar drop -> top-of-page + blank sidebar -> teleport down -> unreserved image CLS!
          schedule(() => {
            setSelectedId(targetIssue.id);
            setLoadStage("current-top-blank-sidebar");
            setNavDir(dir);
            setNavSeq((s) => s + 1);
            setTelemetry({ scrollTop: 0, cacheHit: true, clsPx: 0, loadMs: 0 });
          }, barDropMs);

          schedule(() => {
            setLoadStage("ready");
            setNavSeq((s) => s + 1);
          }, barDropMs + 150);
        }
        return;
      }

      // Directions B / C / D / E:
      if (direction === "card-stack" && fromIssue && (dir === "next" || dir === "prev")) {
        const snapSeq = Date.now();
        setLeavingCard({ issue: fromIssue, direction: dir, seq: snapSeq });
        schedule(() => {
          setLeavingCard((curr) => (curr?.seq === snapSeq ? null : curr));
        }, 220);
      } else {
        setLeavingCard(null);
      }

      setSelectedId(targetIssue.id);
      setNavDir(dir);
      setNavSeq((s) => s + 1);

      if (isWarm) {
        // Warm hit: 0ms instant aligned switch, zero skeleton!
        setLoadStage("ready");
        setTelemetry({ scrollTop: 0, cacheHit: true, clsPx: 0, loadMs: 0 });
      } else {
        // Outran prefetch (rapid E-E-E on slow network):
        // Keep BreadcrumbHeader + 40px InboxBar populated immediately from Inbox row metadata!
        // Show calm body skeleton with thin 2px top progress bar, then align scroll BEFORE reveal.
        setLoadStage("steady-loading-body");
        setTelemetry({ scrollTop: 0, cacheHit: false, clsPx: 0, loadMs: effectiveLatency });
        schedule(() => {
          setWarmIds((prev) => new Set(prev).add(targetIssue.id));
          setLoadStage("ready");
          setNavSeq((s) => s + 1);
        }, effectiveLatency);
      }
    },
    [direction, latencyMs, warmIds, schedule],
  );

  const performAdvance = useCallback(
    (targetId: string, kind: "archived" | "snoozed", toastLabel: string) => {
      clearPendingTimers();
      setBuffer(null);
      const idx = items.findIndex((i) => i.id === targetId);
      if (idx === -1) return;
      const removing = items[idx]!;
      const nextId = pickNextId(items, targetId);
      const nextIssue = nextId ? items.find((i) => i.id === nextId) ?? null : null;

      setHistory((prev) => [...prev, { item: removing, index: idx, kind }]);
      setComposerText("");
      showToast(toastLabel);

      if (direction === "current") {
        setItems((prev) => prev.filter((i) => i.id !== targetId));
        navigateToIssue(nextIssue, "next", removing);
        return;
      }

      // Smoothly collapse row in left Work panel
      setCollapsingIds((prev) => (prev.includes(targetId) ? prev : [...prev, targetId]));
      window.setTimeout(() => {
        setItems((prev) => prev.filter((i) => i.id !== targetId));
        setCollapsingIds((prev) => prev.filter((id) => id !== targetId));
      }, Math.round(165 * motionScale));

      navigateToIssue(nextIssue, "next", removing);
    },
    [clearPendingTimers, items, pickNextId, showToast, direction, navigateToIssue, motionScale],
  );

  const cancelBufferAndStay = useCallback(() => {
    if (!buffer || buffer.paused) return;
    if (bufferRafRef.current !== null) {
      window.cancelAnimationFrame(bufferRafRef.current);
      bufferRafRef.current = null;
    }
    setBuffer((curr) => (curr ? { ...curr, paused: true, progress: 0 } : null));
  }, [buffer]);

  const handleManualArchive = useCallback(() => {
    if (!activeIssue) return;
    performAdvance(activeIssue.id, "archived", COPY.toastArchived[lang]);
  }, [activeIssue, performAdvance, lang]);

  const handleManualSnooze = useCallback(() => {
    if (!activeIssue) return;
    performAdvance(activeIssue.id, "snoozed", COPY.toastSnoozed[lang]);
  }, [activeIssue, performAdvance, lang]);

  const triggerAutoArchive = useCallback(
    (kind: "done" | "assign" | "comment", causeText: string) => {
      if (!activeIssue) return;
      const targetId = activeIssue.id;

      setItems((prev) =>
        prev.map((issue) => {
          if (issue.id !== targetId) return issue;
          if (kind === "done") return { ...issue, status: "done" };
          if (kind === "assign") return { ...issue, assignee: ACTORS.emacs, agentRunning: true };
          return {
            ...issue,
            agentRunning: true,
            comments: [
              ...issue.comments,
              {
                id: `sent-${Date.now()}`,
                author: ACTORS.jiayuan,
                time: { zh: "刚刚", en: "just now" },
                body: {
                  zh: [composerText.trim() || "@Emacs 帮我看一下多图弱网下的锚点偏移，确认没问题就提 PR。"],
                  en: [composerText.trim() || "@Emacs please verify multi-image anchor shift on slow networks, then open a PR."],
                },
              },
            ],
          };
        }),
      );
      if (kind === "comment") setComposerText("");

      if (direction === "current") {
        // Reproduce HTTP + WS inbox:batch-archived + /api/inbox refetch round-trip rug-pull
        clearPendingTimers();
        const serverRoundTrip = Math.max(260, Math.round(latencyMs * 1.45));
        schedule(() => {
          performAdvance(targetId, "archived", COPY.toastAutoArchived[lang]);
        }, serverRoundTrip);
        return;
      }

      if (direction === "buffer") {
        clearPendingTimers();
        const totalMs = Math.round(1400 * motionScale);
        const start = performance.now();
        setBuffer({ issueId: targetId, causeText, progress: 0, paused: false });
        const tick = (now: number) => {
          const p = Math.min(1, (now - start) / totalMs);
          setBuffer((curr) => (curr && curr.issueId === targetId && !curr.paused ? { ...curr, progress: p } : curr));
          if (p < 1) {
            bufferRafRef.current = window.requestAnimationFrame(tick);
          } else {
            bufferRafRef.current = null;
            performAdvance(targetId, "archived", COPY.toastAutoArchived[lang]);
          }
        };
        bufferRafRef.current = window.requestAnimationFrame(tick);
        return;
      }

      clearPendingTimers();
      schedule(() => {
        performAdvance(targetId, "archived", COPY.toastAutoArchived[lang]);
      }, 180);
    },
    [activeIssue, composerText, direction, clearPendingTimers, latencyMs, schedule, performAdvance, lang, motionScale],
  );

  const handleUndo = useCallback(() => {
    if (buffer) {
      clearPendingTimers();
      setBuffer(null);
      if (activeIssue?.status === "done") {
        setItems((prev) => prev.map((i) => (i.id === activeIssue.id ? { ...i, status: "in_review" } : i)));
      }
      return;
    }
    const last = history[history.length - 1];
    if (!last) return;
    clearPendingTimers();
    setLoadStage("ready");
    setHistory((prev) => prev.slice(0, -1));
    setToastMsg(null);

    if (direction === "card-stack" && activeIssue) {
      const snapSeq = Date.now();
      setLeavingCard({ issue: activeIssue, direction: "prev", seq: snapSeq });
      schedule(() => {
        setLeavingCard((curr) => (curr?.seq === snapSeq ? null : curr));
      }, 220);
    } else {
      setLeavingCard(null);
    }

    setItems((prev) => {
      if (prev.some((i) => i.id === last.item.id)) return prev;
      const copy = [...prev];
      copy.splice(Math.min(last.index, copy.length), 0, last.item);
      return copy;
    });
    if (direction !== "current") {
      setEnteringRowId(last.item.id);
      schedule(() => setEnteringRowId((curr) => (curr === last.item.id ? null : curr)), 200);
    }
    setSelectedId(last.item.id);
    setNavDir("prev");
    setNavSeq((s) => s + 1);
    setTelemetry((t) => ({ ...t, cacheHit: true, clsPx: 0, loadMs: 0 }));
  }, [buffer, history, clearPendingTimers, activeIssue, direction, schedule]);

  // Global Inbox keyboard shortcuts
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      const editable =
        !!target &&
        (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.isContentEditable);
      if (event.key === "Escape" && buffer && !buffer.paused) {
        event.preventDefault();
        cancelBufferAndStay();
        return;
      }
      if (editable || event.metaKey || event.ctrlKey || event.altKey) return;
      const k = event.key.toLowerCase();
      if (k === "e") {
        event.preventDefault();
        handleManualArchive();
      } else if (k === "h") {
        event.preventDefault();
        handleManualSnooze();
      } else if (k === "z") {
        event.preventDefault();
        handleUndo();
      } else if (k === "r") {
        event.preventDefault();
        resetWorkbench();
      } else if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        if (visibleRows.length === 0) return;
        event.preventDefault();
        const currIdx = visibleRows.findIndex((r) => r.id === selectedId);
        const step = event.key === "ArrowDown" ? 1 : -1;
        const nextIdx =
          currIdx < 0
            ? step === 1
              ? 0
              : visibleRows.length - 1
            : Math.min(Math.max(currIdx + step, 0), visibleRows.length - 1);
        const targetRow = visibleRows[nextIdx];
        if (targetRow && targetRow.id !== selectedId) {
          clearPendingTimers();
          setBuffer(null);
          navigateToIssue(targetRow, step === 1 ? "next" : "prev", activeIssue);
        }
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [
    buffer,
    cancelBufferAndStay,
    handleManualArchive,
    handleManualSnooze,
    handleUndo,
    resetWorkbench,
    visibleRows,
    selectedId,
    clearPendingTimers,
    navigateToIssue,
    activeIssue,
  ]);

  const unreadCount = visibleRows.filter((i) => i.unread && !collapsingIds.includes(i.id)).length;
  const activeMeta = DIRECTION_BY_ID[direction];

  const bodyEnterClass =
    direction === "current" || loadStage !== "ready"
      ? ""
      : direction === "crossfade"
        ? "inbox-stage-crossfade-enter"
        : direction === "glide" || direction === "buffer"
          ? navDir === "prev"
            ? "inbox-stage-glide-prev"
            : navDir === "next"
              ? "inbox-stage-glide-next"
              : ""
          : direction === "card-stack"
            ? navDir === "prev"
              ? "inbox-card-enter-prev"
              : navDir === "next"
                ? "inbox-card-enter-next"
                : ""
            : "";

  const hideInboxBarForGlitch =
    direction === "current" && (loadStage === "current-bar-gone" || loadStage === "current-skeleton");
  const showCurrentFullSkeleton = direction === "current" && loadStage === "current-skeleton";
  const showCurrentTimelineSkeleton = direction === "current" && loadStage === "current-issue-only";
  const showSteadyBodySkeleton = direction !== "current" && loadStage === "steady-loading-body";
  const blankSidebarForGlitch =
    direction === "current" &&
    (loadStage === "current-skeleton" ||
      loadStage === "current-issue-only" ||
      loadStage === "current-top-blank-sidebar");

  const reserveImages = direction !== "current";

  const renderIssueBody = (issue: DemoInboxIssue, interactive: boolean) => {
    const imagesAlreadyWarm = reserveImages && (latencyMs === 0 || warmImageIds.has(issue.id));
    const targetIdx = issue.comments.findIndex((c) => c.isTarget);

    return (
      <div className="mx-auto max-w-[740px] px-8 py-7">
        <h1 className="text-display-sm font-semibold leading-snug tracking-tight text-foreground">
          {issue.title[lang]}
        </h1>

        <div className="mt-4 space-y-3 text-body leading-relaxed text-foreground/90">
          {issue.description[lang].map((para, i) => (
            <p key={i}>{para}</p>
          ))}
        </div>

        {issue.subIssues.length > 0 && (
          <div className="mt-6 rounded-lg border border-border bg-card/50 p-3">
            <div className="mb-2 flex items-center justify-between text-caption font-medium text-muted-foreground">
              <span>{COPY.subIssues[lang]}</span>
              <span className="tabular-nums">
                {issue.subIssues.filter((s) => s.done).length}/{issue.subIssues.length}
              </span>
            </div>
            <div className="space-y-1.5">
              {issue.subIssues.map((sub) => (
                <div key={sub.id} className="flex items-center gap-2 text-body">
                  <StatusGlyph status={sub.done ? "done" : "todo"} />
                  <span className="text-caption tabular-nums text-muted-foreground">{sub.id}</span>
                  <span className={cn("truncate", sub.done && "text-muted-foreground line-through")}>
                    {sub.title[lang]}
                  </span>
                </div>
              ))}
            </div>
          </div>
        )}

        <div className="my-7 border-t border-border" />

        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-label font-medium text-muted-foreground">{COPY.activity[lang]}</h2>
          <span className="text-caption tabular-nums text-faint-foreground">
            {issue.comments.length} {lang === "zh" ? "条评论" : "comments"}
          </span>
        </div>

        {interactive && showCurrentTimelineSkeleton ? (
          <div className="space-y-4 pb-8">
            <p className="text-caption text-muted-foreground">{COPY.loadingTimeline[lang]}</p>
            {Array.from({ length: 4 }).map((_, i) => (
              <div key={i} className="rounded-lg border border-border p-4 space-y-2.5">
                <div className="flex items-center gap-2">
                  <Skeleton className="size-6 rounded-full" />
                  <Skeleton className="h-3.5 w-28" />
                </div>
                <Skeleton className="h-3.5 w-full" />
                <Skeleton className="h-3.5 w-4/5" />
              </div>
            ))}
          </div>
        ) : (
          <div className="space-y-3 pb-8">
            {issue.comments.map((comment, idx) => {
              const isTarget = !!comment.isTarget;
              const isAboveTarget = targetIdx >= 0 && idx < targetIdx;
              const imgDelayMs =
                !comment.image || latencyMs === 0 || imagesAlreadyWarm
                  ? 0
                  : Math.round(Math.max(180, latencyMs * comment.image.delayMultiplier) * motionScale);

              return (
                <div
                  key={comment.id}
                  data-target-comment={interactive && isTarget ? "true" : undefined}
                  className={cn(
                    "rounded-lg border border-border bg-card px-4 py-3 transition-colors",
                    isTarget && interactive && flashComment && "inbox-comment-flash",
                  )}
                >
                  <div className="flex flex-wrap items-center gap-2">
                    <ActorAvatar
                      name={comment.author.name}
                      initials={comment.author.initials}
                      isAgent={comment.author.isAgent}
                      size="sm"
                    />
                    <span className="text-body font-medium text-foreground">{comment.author.name}</span>
                    <span className="text-caption text-muted-foreground">{comment.time[lang]}</span>
                    {comment.runBadge && (
                      <Badge variant="outline" className="text-micro font-normal text-muted-foreground">
                        {comment.runBadge[lang]}
                      </Badge>
                    )}
                    <span className="ml-auto flex items-center gap-1.5">
                      {isTarget && (
                        <Badge variant="secondary" className="text-micro font-normal text-brand">
                          {COPY.targetTag[lang]}
                        </Badge>
                      )}
                      <span className="text-micro tabular-nums text-faint-foreground">#{idx + 1}</span>
                    </span>
                  </div>

                  <div className="mt-2 space-y-2 text-body leading-relaxed text-foreground/90">
                    {comment.body[lang].map((p, pIdx) => (
                      <p key={pIdx}>{p}</p>
                    ))}
                  </div>

                  {comment.code && (
                    <pre className="mt-2.5 overflow-x-auto rounded-md border border-border/70 bg-muted/60 px-3 py-2 font-mono text-caption text-foreground/90">
                      <code>{comment.code}</code>
                    </pre>
                  )}

                  {comment.image && (
                    <DelayedCommentImage
                      key={`${issue.id}-${comment.id}-${navSeq}-${reserveImages ? "r" : "u"}`}
                      kind={comment.image.kind}
                      caption={comment.image.caption}
                      delayMs={interactive ? imgDelayMs : 0}
                      reserveAspectRatio={reserveImages}
                      loadedInitially={!interactive || imagesAlreadyWarm}
                      onExpandedWithoutReserve={
                        interactive && isAboveTarget ? handleUnreservedImageExpand : undefined
                      }
                      lang={lang}
                    />
                  )}
                </div>
              );
            })}
          </div>
        )}

        {interactive && !showCurrentTimelineSkeleton && (
          <div
            className="rounded-xl border border-border bg-card p-3 shadow-xs transition-colors focus-within:border-ring"
            onFocus={cancelBufferAndStay}
          >
            <textarea
              value={composerText}
              onChange={(e) => setComposerText(e.target.value)}
              rows={2}
              placeholder={COPY.composerPlaceholder[lang]}
              className="block w-full resize-none bg-transparent text-body text-foreground outline-none placeholder:text-muted-foreground"
            />
            <div className="mt-2 flex items-center justify-between gap-2 border-t border-border/60 pt-2">
              <Button
                size="sm"
                variant="ghost"
                className="h-7 gap-1.5 text-caption text-muted-foreground hover:text-foreground"
                onClick={() => triggerAutoArchive("comment", COPY.commentCause[lang])}
              >
                <Sparkles className="size-3.5 text-brand" />
                {COPY.quickAgentReply[lang]}
              </Button>
              <Button
                size="sm"
                className="h-7 gap-1.5"
                disabled={!composerText.trim()}
                onClick={() => triggerAutoArchive("comment", COPY.commentCause[lang])}
              >
                <Send className="size-3" />
                {COPY.send[lang]}
              </Button>
            </div>
          </div>
        )}
      </div>
    );
  };

  const lastCleared = history[history.length - 1] ?? null;
  const targetCommentNumber = activeIssue
    ? activeIssue.comments.findIndex((c) => c.isTarget) + 1
    : 0;
  const imageCount = activeIssue ? activeIssue.comments.filter((c) => !!c.image).length : 0;

  return (
    <div
      style={{ ["--inbox-motion-scale" as string]: String(motionScale) }}
      className={cn("flex w-full flex-col overflow-hidden bg-background text-foreground", heightClass)}
    >
      {/* Prototype control strip: Direction switcher + Network latency + Prefetch status + Slow-mo + Reset */}
      <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-b border-border bg-surface px-3 py-1.5">
        <div className="flex min-w-0 items-center gap-2">
          {allowSwitchDirection ? (
            <div className="inline-flex items-center gap-0.5 rounded-lg bg-muted p-0.5">
              {DIRECTIONS.map((d) => {
                const active = d.id === direction;
                return (
                  <button
                    key={d.id}
                    type="button"
                    onClick={() => resetWorkbench(d.id)}
                    className={cn(
                      "inline-flex cursor-pointer items-center gap-1 rounded-md px-2.5 py-1 text-caption font-medium transition-colors",
                      active
                        ? "bg-background text-foreground shadow-xs"
                        : "text-muted-foreground hover:text-foreground",
                    )}
                  >
                    <span className={cn("text-micro", active ? "text-brand" : "opacity-60")}>{d.letter}</span>
                    <span>{d.shortName[lang]}</span>
                  </button>
                );
              })}
            </div>
          ) : (
            <div className="flex items-center gap-2 text-caption">
              <span className="font-semibold text-brand">{activeMeta.letter}</span>
              <span className="font-semibold text-foreground">{activeMeta.name[lang]}</span>
            </div>
          )}

          {/* Live prefetch status pill for the next item */}
          {nextIdForCurrent && latencyMs > 0 && (
            <span className="hidden items-center gap-1.5 rounded-md border border-border bg-background px-2 py-0.5 text-micro text-muted-foreground xl:inline-flex">
              <span
                className={cn(
                  "size-1.5 rounded-full",
                  nextIsWarm
                    ? "bg-emerald-500"
                    : warmingNextId
                      ? "animate-pulse bg-amber-500"
                      : "bg-red-500",
                )}
              />
              {nextIsWarm
                ? COPY.prefetchWarm[lang]
                : warmingNextId
                  ? COPY.prefetchWarming[lang]
                  : COPY.prefetchCold[lang]}
            </span>
          )}
        </div>

        <div className="flex items-center gap-1.5">
          {/* Network request latency preset */}
          <div className="inline-flex items-center gap-0.5 rounded-lg bg-muted p-0.5 text-caption">
            <span className="px-1.5 text-micro text-muted-foreground">{COPY.latencyLabel[lang]}</span>
            {([0, 250, 600] as LatencyPreset[]).map((ms) => (
              <button
                key={ms}
                type="button"
                onClick={() => {
                  setLatencyMs(ms);
                  setWarmIds(initialWarmSet(items));
                  setWarmImageIds(new Set(selectedId ? [selectedId] : []));
                }}
                className={cn(
                  "cursor-pointer rounded-md px-2 py-0.5 text-micro font-medium tabular-nums transition-colors",
                  latencyMs === ms
                    ? "bg-background text-foreground shadow-xs"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {ms}ms
              </button>
            ))}
          </div>

          <Button
            size="sm"
            variant={slowMo ? "secondary" : "ghost"}
            className="h-7 gap-1 text-caption"
            onClick={() => setSlowMo((v) => !v)}
          >
            <Gauge className="size-3.5" />
            {COPY.slowMo[lang]}
          </Button>
          <Button size="sm" variant="outline" className="h-7 gap-1 text-caption" onClick={() => resetWorkbench()}>
            <RotateCcw className="size-3.5" />
            {COPY.reset[lang]}
            <Kbd>R</Kbd>
          </Button>
        </div>
      </div>

      {/* Main Multica desktop shell */}
      <div className="relative flex min-h-0 flex-1 overflow-hidden">
        {/* Left Work Panel */}
        <aside className="flex w-[268px] shrink-0 flex-col border-r border-border bg-app-shell">
          <div className="flex h-12 shrink-0 items-center justify-between pl-4 pr-2.5">
            <span className="text-title-sm font-semibold text-foreground">{COPY.work[lang]}</span>
            <span className="text-caption text-faint-foreground">{activeMeta.letter}</span>
          </div>

          <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-4">
            <section className="mb-3">
              <div className="flex h-7 items-center gap-1 px-2 text-caption font-medium text-muted-foreground">
                <ChevronDown className="size-3 text-faint-foreground" />
                <span>{COPY.pinned[lang]}</span>
              </div>
              <div className="space-y-0.5">
                <div className="flex h-7 items-center gap-2 rounded-md px-2 text-caption text-muted-foreground">
                  <Pin className="size-3.5 shrink-0 text-faint-foreground" />
                  <span className="truncate">Q4 Roadmap · Multica 0.13</span>
                </div>
              </div>
            </section>

            <section className="mb-3">
              <div className="flex h-7 items-center justify-between px-2">
                <div className="flex items-center gap-1 text-caption font-medium text-muted-foreground">
                  <ChevronDown className="size-3 text-faint-foreground" />
                  <span>{COPY.inbox[lang]}</span>
                </div>
                {unreadCount > 0 && (
                  <span className="text-caption font-medium tabular-nums text-brand">{unreadCount}</span>
                )}
              </div>

              {visibleRows.length === 0 ? (
                <p className="px-6 py-2 text-caption text-faint-foreground">{COPY.inboxEmpty[lang]}</p>
              ) : (
                <ul className="space-y-0.5">
                  {visibleRows.map((item) => {
                    const isSelected = item.id === selectedId;
                    const isCollapsing = collapsingIds.includes(item.id);
                    const isEntering = enteringRowId === item.id;
                    const isWarmRow = latencyMs === 0 || warmIds.has(item.id);
                    return (
                      <li
                        key={item.id}
                        className="inbox-row-wrap"
                        data-motion={direction === "current" ? "instant" : "smooth"}
                        data-state={isCollapsing ? "collapsing" : isEntering ? "entering" : "idle"}
                      >
                        <div className="overflow-hidden">
                          <div
                            role="button"
                            tabIndex={0}
                            onClick={() => {
                              clearPendingTimers();
                              setBuffer(null);
                              navigateToIssue(item, "jump", activeIssue);
                            }}
                            onKeyDown={(e) => {
                              if (e.key === "Enter" || e.key === " ") {
                                e.preventDefault();
                                clearPendingTimers();
                                setBuffer(null);
                                navigateToIssue(item, "jump", activeIssue);
                              }
                            }}
                            className={cn(
                              "group/inbox relative flex cursor-default select-none items-start gap-2 rounded-md px-2 py-1.5 text-left transition-colors",
                              isSelected
                                ? "bg-surface-selected text-foreground"
                                : "text-foreground/85 hover:bg-surface-hover",
                            )}
                          >
                            <span className="mt-[3px] flex shrink-0">
                              <StatusGlyph status={item.status} />
                            </span>
                            <div className="min-w-0 flex-1">
                              <div className="flex items-center gap-1.5 leading-5">
                                <span
                                  className={cn(
                                    "min-w-0 flex-1 truncate text-body",
                                    item.unread ? "font-medium text-foreground" : "text-muted-foreground",
                                  )}
                                >
                                  {item.title[lang]}
                                </span>
                                <span className="shrink-0 text-caption tabular-nums text-faint-foreground group-hover/inbox:invisible">
                                  {item.timeAgo[lang]}
                                </span>
                              </div>
                              <div className="mt-0.5 flex items-center gap-1.5 text-caption leading-4 text-muted-foreground">
                                <ActorAvatar
                                  name={item.actor.name}
                                  initials={item.actor.initials}
                                  isAgent={item.actor.isAgent}
                                  size="xs"
                                />
                                <span className="min-w-0 flex-1 truncate">{item.reason[lang]}</span>
                                {!isWarmRow && latencyMs > 0 && (
                                  <span
                                    title={lang === "zh" ? "未缓存" : "Cold"}
                                    className="text-micro text-faint-foreground"
                                  >
                                    {lang === "zh" ? "冷" : "cold"}
                                  </span>
                                )}
                                {item.agentRunning && <Bot className="size-3 shrink-0 text-brand" />}
                                {item.unread && <span className="size-1.5 shrink-0 rounded-full bg-brand" />}
                              </div>
                            </div>

                            <div className="absolute top-1.5 right-1.5 hidden items-center gap-0.5 rounded-md bg-app-shell/90 group-hover/inbox:flex">
                              <TooltipHint label={COPY.snooze[lang]} side="bottom">
                                <button
                                  type="button"
                                  aria-label={COPY.snooze[lang]}
                                  onClick={(e) => {
                                    e.stopPropagation();
                                    performAdvance(item.id, "snoozed", COPY.toastSnoozed[lang]);
                                  }}
                                  className="flex size-5 items-center justify-center rounded-md text-muted-foreground hover:bg-surface-selected hover:text-foreground"
                                >
                                  <AlarmClock className="size-3" />
                                </button>
                              </TooltipHint>
                              <TooltipHint label={COPY.archive[lang]} side="bottom">
                                <button
                                  type="button"
                                  aria-label={COPY.archive[lang]}
                                  onClick={(e) => {
                                    e.stopPropagation();
                                    performAdvance(item.id, "archived", COPY.toastArchived[lang]);
                                  }}
                                  className="flex size-5 items-center justify-center rounded-md text-muted-foreground hover:bg-surface-selected hover:text-foreground"
                                >
                                  <Archive className="size-3" />
                                </button>
                              </TooltipHint>
                            </div>
                          </div>
                        </div>
                      </li>
                    );
                  })}
                </ul>
              )}
            </section>

            <section>
              <div className="flex h-7 items-center gap-1 px-2 text-caption font-medium text-muted-foreground">
                <ChevronDown className="size-3 text-faint-foreground" />
                <span>{COPY.projects[lang]}</span>
              </div>
              <div className="space-y-0.5 text-caption text-muted-foreground">
                <div className="flex h-7 items-center gap-2 rounded-md px-2">
                  <Folder className="size-3.5 text-faint-foreground" />
                  <span className="truncate">{lang === "zh" ? "收件箱体验" : "Inbox Experience"}</span>
                </div>
                <div className="flex h-7 items-center gap-2 rounded-md px-2">
                  <Folder className="size-3.5 text-faint-foreground" />
                  <span className="truncate">{lang === "zh" ? "工作区壳层" : "Workspace Shell"}</span>
                </div>
              </div>
            </section>
          </div>
        </aside>

        {/* Center Issue Detail */}
        <main className="relative flex min-w-0 flex-1 flex-col bg-background">
          {!activeIssue ? (
            <>
              <div className="flex h-12 shrink-0 items-center border-b border-border px-5">
                <span className="flex items-center gap-1.5 text-body font-semibold">
                  <Inbox className="size-4 text-muted-foreground" />
                  {COPY.inbox[lang]}
                </span>
              </div>
              <div
                key={`zero-${navSeq}`}
                className={cn(
                  "relative flex min-h-0 flex-1 flex-col items-center justify-center overflow-hidden px-6 text-center",
                  bodyEnterClass,
                )}
              >
                <MulticaBleedMark
                  enterDelay={0.15}
                  className="pointer-events-none absolute -right-[12%] -bottom-[24%] w-[min(460px,52%)] text-muted"
                />
                <div className="relative z-10 -mt-6 flex max-w-md flex-col items-center">
                  <div className="mb-4 flex size-11 items-center justify-center rounded-xl border border-surface-border bg-surface text-success shadow-xs">
                    <Check className="size-5" />
                  </div>
                  <h2 className="text-title font-semibold text-foreground">{COPY.clearedTitle[lang]}</h2>
                  {lastCleared && (
                    <p className="mt-1.5 flex items-center gap-1.5 text-label text-muted-foreground">
                      <span>
                        {COPY.clearedSub[lang]} {lastCleared.item.identifier}
                      </span>
                      <span aria-hidden className="text-faint-foreground">·</span>
                      <button
                        type="button"
                        onClick={handleUndo}
                        className="inline-flex cursor-pointer items-center gap-1 rounded-md px-1.5 py-0.5 text-caption font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                      >
                        <Undo2 className="size-3" />
                        {COPY.undo[lang]}
                        <Kbd>Z</Kbd>
                      </button>
                    </p>
                  )}
                  <div className="mt-2 inline-flex items-center gap-1.5 text-caption text-faint-foreground">
                    <AlarmClock className="size-3.5" />
                    <span>{COPY.snoozedSummary[lang]}</span>
                  </div>
                </div>
              </div>
            </>
          ) : (
            <>
              {/* 48px BreadcrumbHeader — immediately populated from Inbox row metadata in B/C/D/E even on cold miss */}
              <header className="flex h-12 shrink-0 items-center justify-between gap-3 border-b border-border px-6">
                <div className="flex min-w-0 items-center gap-1.5 text-body">
                  {showCurrentFullSkeleton ? (
                    <>
                      <Skeleton className="h-4 w-16" />
                      <Skeleton className="h-4 w-4" />
                      <Skeleton className="h-4 w-32" />
                    </>
                  ) : (
                    <>
                      <span className="shrink-0 text-muted-foreground">{activeIssue.project[lang]}</span>
                      <span className="text-faint-foreground">/</span>
                      <span className="shrink-0 tabular-nums text-muted-foreground">{activeIssue.identifier}</span>
                      <span className="truncate font-medium text-foreground">{activeIssue.title[lang]}</span>
                    </>
                  )}
                </div>
                <div className="flex shrink-0 items-center gap-1">
                  <IconButton label="More" className="text-muted-foreground">
                    <MoreHorizontal />
                  </IconButton>
                  <IconButton
                    label="Toggle sidebar"
                    variant={sidebarOpen ? "secondary" : "ghost"}
                    className={sidebarOpen ? "" : "text-muted-foreground"}
                    onClick={() => setSidebarOpen((v) => !v)}
                  >
                    <PanelRight />
                  </IconButton>
                </div>
              </header>

              {/* 40px InboxBar — rock-steady in B/C/D/E; drops out mid-switch in A */}
              {!hideInboxBarForGlitch && (
                <div
                  className={cn(
                    "relative flex h-10 shrink-0 items-center gap-1 border-b border-border px-6 transition-colors",
                    buffer?.issueId === activeIssue.id && "bg-muted/40",
                  )}
                >
                  {buffer?.issueId === activeIssue.id ? (
                    <>
                      <span className="mr-1 flex min-w-0 flex-1 items-center gap-1.5 text-caption text-muted-foreground">
                        <CheckCircle2 className="size-3.5 shrink-0 text-emerald-500" />
                        <span className="truncate font-medium text-foreground">{buffer.causeText}</span>
                        <span aria-hidden className="text-faint-foreground">·</span>
                        <span className="shrink-0">
                          {buffer.paused ? COPY.bufferPaused[lang] : COPY.bufferAdvancing[lang]}
                        </span>
                      </span>
                      {!buffer.paused && (
                        <Button
                          size="sm"
                          variant="ghost"
                          className="h-7 gap-1 text-caption text-muted-foreground"
                          onClick={cancelBufferAndStay}
                        >
                          <X className="size-3.5" />
                          {COPY.stayHere[lang]}
                          <Kbd>Esc</Kbd>
                        </Button>
                      )}
                      <Button
                        size="sm"
                        variant="ghost"
                        className="h-7 gap-1 text-caption text-muted-foreground"
                        onClick={handleUndo}
                      >
                        <Undo2 className="size-3.5" />
                        {COPY.undo[lang]}
                        <Kbd>Z</Kbd>
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        className="h-7 gap-1 text-caption"
                        onClick={handleManualArchive}
                      >
                        {COPY.next[lang]}
                        <ArrowDown className="size-3.5" />
                        <Kbd>E</Kbd>
                      </Button>
                      {!buffer.paused && (
                        <div
                          className="pointer-events-none absolute right-0 bottom-0 left-0 h-[2px] origin-left bg-brand"
                          style={{ transform: `scaleX(${buffer.progress})` }}
                        />
                      )}
                    </>
                  ) : (
                    <>
                      <span className="mr-1 flex min-w-0 flex-1 items-center gap-1.5 text-caption text-muted-foreground">
                        <ActorAvatar
                          name={activeIssue.actor.name}
                          initials={activeIssue.actor.initials}
                          isAgent={activeIssue.actor.isAgent}
                          size="xs"
                        />
                        <span className="truncate">{activeIssue.reason[lang]}</span>
                        <span className="shrink-0 tabular-nums text-faint-foreground">
                          · {activeIssue.timeAgo[lang]}
                        </span>
                      </span>

                      <Button
                        size="sm"
                        variant="ghost"
                        className="h-7 gap-1.5 text-caption"
                        onClick={() => triggerAutoArchive("done", COPY.doneCause[lang])}
                      >
                        <CheckCircle2 className="size-3.5 text-emerald-500" />
                        {COPY.markDone[lang]}
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        className="h-7 gap-1.5 text-caption text-muted-foreground hover:text-foreground"
                        onClick={() => triggerAutoArchive("assign", COPY.assignCause[lang])}
                      >
                        <Bot className="size-3.5" />
                        {COPY.assignAgent[lang]}
                      </Button>

                      <span aria-hidden className="mx-1 h-4 w-px shrink-0 bg-border" />

                      <TooltipHint label={COPY.unsubscribe[lang]} side="bottom">
                        <IconButton
                          label={COPY.unsubscribe[lang]}
                          className="text-muted-foreground"
                          onClick={handleManualArchive}
                        >
                          <BellOff />
                        </IconButton>
                      </TooltipHint>
                      <Button
                        size="sm"
                        variant="ghost"
                        className="h-7 gap-1 text-caption text-muted-foreground hover:text-foreground"
                        onClick={handleManualSnooze}
                      >
                        <AlarmClock className="size-3.5" />
                        {COPY.snooze[lang]}
                        <Kbd>H</Kbd>
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        className="h-7 gap-1 text-caption text-muted-foreground hover:text-foreground"
                        onClick={handleManualArchive}
                      >
                        <Archive className="size-3.5" />
                        {COPY.archive[lang]}
                        <Kbd>E</Kbd>
                      </Button>

                      {/* Calm loading hairline when rapid E outruns anchor prefetch */}
                      {showSteadyBodySkeleton && (
                        <div className="pointer-events-none absolute right-0 bottom-0 left-0 h-[2px] overflow-hidden bg-muted">
                          <div className="h-full w-2/3 animate-pulse bg-brand" />
                        </div>
                      )}
                    </>
                  )}
                </div>
              )}

              {/* Scrollable Reading Column */}
              <div className="relative min-h-0 flex-1 overflow-hidden">
                {direction === "card-stack" && leavingCard && (
                  <div
                    key={`leave-${leavingCard.seq}`}
                    className={cn(
                      "pointer-events-none absolute inset-0 z-10 overflow-hidden bg-background",
                      leavingCard.direction === "next" ? "inbox-card-leave-next" : "inbox-card-leave-prev",
                    )}
                  >
                    {renderIssueBody(leavingCard.issue, false)}
                  </div>
                )}

                <div
                  ref={scrollRef}
                  onWheel={cancelBufferAndStay}
                  onMouseDown={cancelBufferAndStay}
                  onScroll={(e) => {
                    const top = Math.round(e.currentTarget.scrollTop);
                    setTelemetry((t) => (Math.abs(t.scrollTop - top) > 12 ? { ...t, scrollTop: top } : t));
                  }}
                  className="h-full overflow-y-auto [scrollbar-gutter:stable_both-edges]"
                >
                  {showCurrentFullSkeleton || showSteadyBodySkeleton ? (
                    <div className="mx-auto max-w-[740px] space-y-6 px-8 py-7">
                      {showSteadyBodySkeleton ? (
                        <h1 className="text-display-sm font-semibold leading-snug tracking-tight text-foreground">
                          {activeIssue.title[lang]}
                        </h1>
                      ) : (
                        <Skeleton className="h-8 w-3/4" />
                      )}
                      <div className="space-y-2.5">
                        <Skeleton className="h-4 w-full" />
                        <Skeleton className="h-4 w-5/6" />
                        <Skeleton className="h-4 w-2/3" />
                      </div>
                      <Skeleton className="h-px w-full" />
                      <div className="space-y-4">
                        <div className="flex items-center justify-between">
                          <Skeleton className="h-4 w-24" />
                          {showSteadyBodySkeleton && (
                            <span className="text-caption tabular-nums text-muted-foreground">
                              {COPY.loadingTimeline[lang]} ({latencyMs}ms)
                            </span>
                          )}
                        </div>
                        {Array.from({ length: 3 }).map((_, i) => (
                          <div key={i} className="rounded-lg border border-border p-4 space-y-2.5">
                            <div className="flex items-center gap-2">
                              <Skeleton className="size-6 rounded-full" />
                              <Skeleton className="h-3.5 w-32" />
                            </div>
                            <Skeleton className="h-3.5 w-full" />
                            <Skeleton className="h-3.5 w-4/5" />
                            {i === 0 && <Skeleton className="mt-2 h-44 w-full rounded-md" />}
                          </div>
                        ))}
                      </div>
                    </div>
                  ) : (
                    <div
                      key={`${activeIssue.id}-${navSeq}`}
                      className={cn(bodyEnterClass, direction === "card-stack" && "bg-background")}
                    >
                      {renderIssueBody(activeIssue, true)}
                    </div>
                  )}
                </div>
              </div>
            </>
          )}
        </main>

        {/* Right Properties Sidebar */}
        {sidebarOpen && activeIssue && (
          <aside className="hidden w-[256px] shrink-0 flex-col border-l border-border bg-background px-4 py-4 lg:flex">
            <div
              className={cn(
                "space-y-4 transition-opacity duration-100",
                blankSidebarForGlitch ? "opacity-0" : "opacity-100",
              )}
            >
              <PropRow label={COPY.status[lang]}>
                <button
                  type="button"
                  onClick={() => triggerAutoArchive("done", COPY.doneCause[lang])}
                  className="inline-flex cursor-pointer items-center gap-1.5 rounded-md px-1.5 py-0.5 text-body font-medium hover:bg-muted"
                >
                  <StatusGlyph status={activeIssue.status} />
                  <span>{STATUS_META[activeIssue.status].label[lang]}</span>
                </button>
              </PropRow>

              <PropRow label={COPY.priority[lang]}>
                <span className="px-1.5 text-body text-foreground">
                  {PRIORITY_LABEL[activeIssue.priority][lang]}
                </span>
              </PropRow>

              <PropRow label={COPY.assignee[lang]}>
                <button
                  type="button"
                  onClick={() => triggerAutoArchive("assign", COPY.assignCause[lang])}
                  className="inline-flex cursor-pointer items-center gap-1.5 rounded-md px-1.5 py-0.5 text-body hover:bg-muted"
                >
                  <ActorAvatar
                    name={activeIssue.assignee.name}
                    initials={activeIssue.assignee.initials}
                    isAgent={activeIssue.assignee.isAgent}
                    size="xs"
                  />
                  <span>{activeIssue.assignee.name}</span>
                  {activeIssue.agentRunning && (
                    <Badge variant="secondary" className="text-micro font-normal text-brand">
                      {COPY.running[lang]}
                    </Badge>
                  )}
                </button>
              </PropRow>

              <PropRow label={COPY.project[lang]}>
                <span className="inline-flex items-center gap-1.5 px-1.5 text-body text-foreground">
                  <Folder className="size-3.5 text-muted-foreground" />
                  {activeIssue.project[lang]}
                </span>
              </PropRow>

              <PropRow label={COPY.labels[lang]}>
                <div className="flex flex-wrap gap-1 px-1">
                  {activeIssue.labels[lang].map((lb) => (
                    <Badge key={lb} variant="secondary" className="font-normal">
                      {lb}
                    </Badge>
                  ))}
                </div>
              </PropRow>
            </div>
          </aside>
        )}

        {/* Bottom-right Undo Toast */}
        {toastMsg && (
          <div
            key={toastMsg.id}
            className="absolute right-4 bottom-4 z-30 flex items-center gap-3 rounded-lg border border-border bg-popover px-3.5 py-2.5 text-caption text-popover-foreground shadow-md animate-in fade-in slide-in-from-bottom-2 duration-150"
          >
            <CheckCircle2 className="size-4 text-emerald-500" />
            <span>{toastMsg.text}</span>
            <button
              type="button"
              onClick={handleUndo}
              className="inline-flex cursor-pointer items-center gap-1 rounded-md border border-border bg-background px-2 py-0.5 text-caption font-medium text-foreground hover:bg-muted"
            >
              {COPY.undo[lang]}
              <Kbd>Z</Kbd>
            </button>
          </div>
        )}
      </div>

      {/* Telemetry footer showing live scroll depth, comment #, cache hit, and image CLS */}
      <footer className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-t border-border bg-surface px-4 py-1.5 text-caption text-muted-foreground">
        <div className="flex min-w-0 items-center gap-2 tabular-nums">
          {activeIssue && (
            <>
              <span className="font-medium text-foreground">
                {lang === "zh"
                  ? `定位评论 #${targetCommentNumber}/${activeIssue.comments.length}`
                  : `Target #${targetCommentNumber}/${activeIssue.comments.length}`}
              </span>
              <span aria-hidden className="text-faint-foreground">·</span>
              <span>
                {imageCount} {lang === "zh" ? "张图" : "images"}
              </span>
              <span aria-hidden className="text-faint-foreground">·</span>
              <span>scrollTop {telemetry.scrollTop}px</span>
              <span aria-hidden className="text-faint-foreground">·</span>
              <span className={telemetry.cacheHit ? "text-emerald-600 dark:text-emerald-400" : "text-amber-600 dark:text-amber-400"}>
                {telemetry.cacheHit
                  ? lang === "zh"
                    ? "缓存命中 0ms"
                    : "Cache hit 0ms"
                  : lang === "zh"
                    ? `冷请求 ${telemetry.loadMs}ms`
                    : `Cold fetch ${telemetry.loadMs}ms`}
              </span>
              {telemetry.clsPx > 0 && (
                <>
                  <span aria-hidden className="text-faint-foreground">·</span>
                  <span className="font-medium text-red-600 dark:text-red-400">
                    {lang === "zh"
                      ? `图片展开偏移 +${telemetry.clsPx}px`
                      : `Image shift +${telemetry.clsPx}px`}
                  </span>
                </>
              )}
            </>
          )}
        </div>
        <span className="shrink-0 text-faint-foreground">{COPY.shortcutsHint[lang]}</span>
      </footer>
    </div>
  );
}

function PropRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[64px_1fr] items-center gap-2">
      <span className="text-caption text-muted-foreground">{label}</span>
      <div className="min-w-0">{children}</div>
    </div>
  );
}
