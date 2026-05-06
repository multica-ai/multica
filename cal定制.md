项目对 `react-big-calendar` 的定制分为以下几类：

---

## 1. 自定义组件（`components` prop）

通过 `DnDCalendar` 的 `components` prop 替换了 4 个内部组件：

### `CalendarEventCard` — 事件卡片 [1](#14-0) 

- 显示 description、project name、task name、tags、时长
- 运行中条目显示斜线条纹背景（`repeating-linear-gradient`）
- hover 时显示"继续"播放按钮
- 右键触发自定义上下文菜单（而非浏览器默认菜单）
- 用 `memo` 包裹，防止 RBC 内部状态更新导致所有卡片重渲染

### `CalendarDayHeader` — 列头 [2](#14-1) 

- 显示日期数字 + 星期缩写 + **当天总时长**（`dailyTotals`）
- 今天高亮（圆形背景 + accent 颜色）
- 用 `memo` 包裹，防止每分钟 `nowMinuteMs` 更新时列头闪烁重渲染

### `CalendarDayColumnWrapper` — 列容器 [3](#14-2) 

- 在"今天"列的 `.rbc-current-time-indicator` 旁边注入一个 SVG 播放按钮
- 用 `MutationObserver` + `ResizeObserver` 实时同步播放按钮的 Y 坐标与时间指示线对齐

### `CalendarZoomControls` — 时间轴左上角缩放控件 [4](#14-3) 

- 替换 `timeGutterHeader` 插槽，放置 +/- 缩放按钮
- 控制 `step`（15/30 分钟）和 `timeslots`（4/2 格）

---

## 2. 自定义布局算法（`dayLayoutAlgorithm` prop）

`calendarDayLayout` 完全替换了 RBC 的默认重叠布局算法： [5](#14-4) 

核心差异：RBC 默认把"A 结束时刻 = B 开始时刻"的两个条目视为重叠并排显示，项目改为**严格重叠检测**（`strictlyInside`），让首尾相接的条目垂直堆叠而非并排，与 Toggl Track 行为一致。

---

## 3. 事件数据预处理（`calendar-events-builder.ts`）

在把数据传给 RBC 之前做了两处特殊处理：

**跨午夜拆分**（`splitAtMidnight`）：把跨越 0 点的条目拆成多个片段，防止 RBC 把它提升为全天事件： [6](#14-5) 

**子分钟条目修正**（`displayEndForCalendar`）：RBC 的 DnD 插件对 start/end 在同一分钟内的事件会错误地加一整天，项目将 `end` 强制推过下一分钟边界（仅影响显示，不修改原始数据）： [7](#14-6) 

---

## 4. CSS 样式覆盖（`calendar.css`）

全量覆盖 RBC 默认样式以匹配暗色主题： [8](#14-7) 

主要覆盖点：

| 覆盖项 | 说明 |
|---|---|
| `.rbc-time-header` | 改为 `position: sticky`，跟随 timer bar 粘性定位 |
| `.rbc-time-content` | `overflow: visible`，禁用内部滚动条，改为 window 滚动 |
| `.rbc-event` | 去掉边框和圆角（圆角移到内层 EventBox），`background: transparent` |
| `.rbc-event-label` | `display: none`，隐藏 RBC 自带的时间范围标签 |
| `.rbc-event-running` | 运行中条目右侧留 10px 间距，叠加斜线条纹 |
| `.rbc-allday-cell` | `display: none`，隐藏全天事件区域 |
| `.rbc-toolbar` | `display: none`，隐藏 RBC 自带导航栏 |
| `.rbc-current-time-indicator` | 改为 3px 高、accent 颜色的圆角线 |
| `.rbc-addons-dnd-resize-ns-icon` | `display: none`，隐藏默认拖拽手柄图标 |

---

## 5. 挂载后滚动定位（`CalendarView` 内部）

RBC 本身不提供"滚动到当前时间"的精确控制，项目用 `MutationObserver` 监听 `.rbc-current-time-indicator` 出现后，将 window 滚动到当前时间指示线居中的位置，并兼顾最早事件卡片不被遮挡： [9](#14-8) 

---

## 6. 拖拽行为修正

RBC 的 `onEventDrop` 和 `onEventResize` 回调在某些情况下会同时触发，导致两次 PUT 请求互相覆盖。项目在 `onEventDrop` 中明确注释并只调用 `onMoveEntry`，不再调用 `onResizeEntry`： [10](#14-9)

### Citations

**File:** apps/website/src/features/tracking/CalendarEventCard.tsx (L26-50)
```typescript
function CalendarEventCardImpl({
  event,
  onContextMenu,
  onContinueEntry,
  onEditEntry,
}: {
  event: CalendarEvent;
  onContextMenu?: (
    entry: GithubComTogglTogglApiInternalModelsTimeEntry,
    x: number,
    y: number,
  ) => void;
  onContinueEntry?: (entry: GithubComTogglTogglApiInternalModelsTimeEntry) => void;
  onEditEntry?: (entry: GithubComTogglTogglApiInternalModelsTimeEntry, anchorRect: DOMRect) => void;
}) {
  const { t } = useTranslation("tracking");
  const { durationFormat } = useUserPreferences();
  const entry = event.entry;
  const durationSeconds = resolveEntryDurationSeconds(entry);
  const color = event.resource.color;
  const isRunning = event.resource.isRunning;
  const cardRef = useRef<HTMLDivElement>(null);
  const entryId = event.id;
  const isDraft = event.resource.isDraft;
  const allowDirectEdit = !event.resource.isLocked && !isRunning && !isDraft;
```

**File:** apps/website/src/features/tracking/CalendarDayHeader.tsx (L17-79)
```typescript
function CalendarDayHeaderImpl({
  date,
  dailyTotals,
  timezone,
  today,
}: {
  date: Date;
  dailyTotals: Map<string, number>;
  timezone: string;
  today: Date;
}) {
  const renderCount = useRenderCount();
  const dayNum = date.getDate();
  const dayName = new Intl.DateTimeFormat(i18n.language, { weekday: "short" })
    .format(date)
    .toUpperCase();
  const dateKey = new Intl.DateTimeFormat("en-CA", {
    day: "2-digit",
    month: "2-digit",
    timeZone: timezone,
    year: "numeric",
  }).format(date);
  const totalSeconds = dailyTotals.get(dateKey) ?? 0;
  const isToday =
    date.getFullYear() === today.getFullYear() &&
    date.getMonth() === today.getMonth() &&
    date.getDate() === today.getDate();

  return (
    <div
      className="flex w-full items-center gap-2 px-2 py-2"
      data-testid={`calendar-day-header-${dayName.toLowerCase()}`}
    >
      <span
        className={`flex h-[32px] w-[36px] items-center justify-center text-[22px] font-semibold leading-none ${
          isToday ? "rounded-full bg-[var(--track-accent)]/30 text-white p-2" : "text-white"
        }`}
      >
        {dayNum}
      </span>
      <span className="flex flex-col items-start leading-tight">
        <span
          className={`text-[11px] font-medium tracking-wide ${
            isToday ? "text-[var(--track-accent)]" : "text-[var(--track-text-soft)]"
          }`}
        >
          {dayName}
        </span>
        <span className="text-[11px] tabular-nums text-[var(--track-text-soft)]">
          {totalSeconds > 0 ? formatDayTotal(totalSeconds) : "0:00:00"}
        </span>
      </span>
      {import.meta.env.DEV ? (
        <span
          className="ml-auto font-mono text-[10px] leading-none text-[var(--track-text-muted)]"
          data-testid={`calendar-day-header-rendercount-${dayName.toLowerCase()}`}
        >
          r:{renderCount}
        </span>
      ) : null}
    </div>
  );
}
```

**File:** apps/website/src/features/tracking/CalendarDayColumnWrapper.tsx (L9-93)
```typescript
export const CalendarDayColumnWrapper = React.forwardRef<
  HTMLDivElement,
  {
    children?: React.ReactNode;
    className?: string;
    style?: React.CSSProperties;
    isNow?: boolean;
    onStartEntry?: () => void;
  }
>(function CalendarDayColumnWrapper({ children, className, style, isNow, onStartEntry }, ref) {
  const columnRef = useRef<HTMLDivElement>(null);
  const playRef = useRef<SVGSVGElement>(null);

  const setRef = (node: HTMLDivElement | null) => {
    (columnRef as { current: HTMLDivElement | null }).current = node;
    if (typeof ref === "function") ref(node);
    else if (ref) (ref as { current: HTMLDivElement | null }).current = node;
  };

  const syncPosition = () => {
    const indicator = columnRef.current?.querySelector<HTMLElement>(".rbc-current-time-indicator");
    if (indicator && playRef.current) {
      playRef.current.style.top = indicator.style.top;
    }
  };

  useLayoutEffect(() => {
    if (!isNow || !columnRef.current || !playRef.current) return;

    const frame = requestAnimationFrame(syncPosition);
    const interval = window.setInterval(syncPosition, 10_000);
    const mutationObserver = new MutationObserver(syncPosition);
    mutationObserver.observe(columnRef.current, {
      attributes: true,
      childList: true,
      subtree: true,
    });

    let resizeObserver: ResizeObserver | null = null;
    if (typeof ResizeObserver !== "undefined") {
      resizeObserver = new ResizeObserver(syncPosition);
      resizeObserver.observe(columnRef.current);
    }

    return () => {
      cancelAnimationFrame(frame);
      window.clearInterval(interval);
      mutationObserver.disconnect();
      resizeObserver?.disconnect();
    };
  }, [children, className, isNow, style, syncPosition]);

  return (
    <div className={className} ref={setRef} style={style}>
      {children}
      {isNow ? (
        <svg
          className="calendar-indicator-play-btn absolute cursor-pointer"
          data-testid="current-time-indicator-play"
          fill="none"
          height="16"
          onMouseDown={(e) => {
            e.stopPropagation();
            e.preventDefault();
          }}
          onClick={(e) => {
            e.stopPropagation();
            onStartEntry?.();
          }}
          ref={playRef}
          style={{ pointerEvents: "all", left: "-7px", marginTop: "-6.5px" }}
          viewBox="0 0 36 36"
          width="16"
          xmlns="http://www.w3.org/2000/svg"
        >
          <rect fill="var(--track-accent)" height="36" rx="18" width="36" />
          <path
            d="M13 11.994c0-1.101.773-1.553 1.745-.997l10.51 6.005c.964.55.972 1.439 0 1.994l-10.51 6.007c-.964.55-1.745.102-1.745-.997V11.994z"
            fill="var(--track-canvas)"
          />
        </svg>
      ) : null}
    </div>
  );
});
```

**File:** apps/website/src/features/tracking/CalendarZoomControls.tsx (L4-39)
```typescript
export function CalendarZoomControls({
  zoom,
  onZoomIn,
  onZoomOut,
}: {
  zoom: number;
  onZoomIn?: () => void;
  onZoomOut?: () => void;
}) {
  const { t } = useTranslation("tracking");
  return (
    <div
      className="flex items-center justify-center gap-1 py-2"
      data-testid="calendar-zoom-controls"
    >
      <button
        aria-label={t("decreaseZoom")}
        className="flex size-6 items-center justify-center rounded text-[var(--track-text-soft)] transition hover:text-white disabled:cursor-not-allowed disabled:opacity-30"
        disabled={zoom <= -1}
        onClick={onZoomOut}
        type="button"
      >
        <MinusIcon className="size-3" />
      </button>
      <button
        aria-label={t("increaseZoom")}
        className="flex size-6 items-center justify-center rounded text-[var(--track-text-soft)] transition hover:text-white disabled:cursor-not-allowed disabled:opacity-30"
        disabled={zoom >= 1}
        onClick={onZoomIn}
        type="button"
      >
        <PlusIcon className="size-3" />
      </button>
    </div>
  );
}
```

**File:** apps/website/src/features/tracking/calendar-day-layout.ts (L1-143)
```typescript
/**
 * Custom day layout algorithm for react-big-calendar.
 *
 * Matches Toggl Track's behavior: uses RBC's default overlap algorithm for
 * initial top/height positioning, then re-computes column assignments with
 * **strict** overlap detection so that sequential entries (A ends exactly
 * when B starts) are stacked vertically instead of side-by-side.
 */
// eslint-disable-next-line @typescript-eslint/ban-ts-comment
// @ts-expect-error — no type declarations for this internal RBC module
import overlapModule from "react-big-calendar/lib/utils/layout-algorithms/overlap";

type StyledEvent = {
  event: unknown;
  friends: StyledEvent[];
  idx: number;
  size: number;
  style: {
    height: number | string;
    left: number | string;
    top: number | string;
    width: number | string;
    xOffset: number | string;
  };
};

// Vite's CJS interop may double-wrap the default export — resolve it
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function resolveDefault(mod: any): (args: any) => StyledEvent[] {
  if (typeof mod === "function") return mod;
  if (typeof mod?.default === "function") return mod.default;
  throw new Error("react-big-calendar overlap layout module could not be resolved");
}
const getDefaultOverlapLayout = resolveDefault(overlapModule);

/** Strict overlap: value is strictly inside (0, rangeEnd). */
function strictlyInside(value: number, rangeEnd: number): boolean {
  return value < rangeEnd;
}

/** Recursively count the deepest chain of friends to determine column count. */
function maxFriendDepth(event: StyledEvent, depth: number, visited: StyledEvent[]): number {
  visited.push(event);
  let max = depth;
  for (const friend of event.friends) {
    if (!visited.includes(friend)) {
      const d = maxFriendDepth(friend, depth + 1, visited);
      if (d > max) max = d;
    }
  }
  return max;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function calendarDayLayout(args: any): StyledEvent[] {
  // Step 1: run RBC's default overlap algorithm for initial top/height
  const items: StyledEvent[] = getDefaultOverlapLayout(args);

  // Step 2: sort by top position, then longest-duration first
  items.sort((a, b) => {
    const aTop = Number(a.style.top);
    const bTop = Number(b.style.top);
    if (aTop !== bTop) return aTop > bTop ? 1 : -1;
    const aBottom = aTop + Number(a.style.height);
    const bBottom = bTop + Number(b.style.height);
    return aBottom < bBottom ? 1 : -1;
  });

  // Step 3: reset column assignments from the default algorithm
  for (const item of items) {
    item.friends = [];
    delete (item.style as Record<string, unknown>).left;
    delete (item as Record<string, unknown>).idx;
    delete (item as Record<string, unknown>).size;
  }

  // Step 4: find "friends" (truly overlapping events) using strict comparison
  for (let i = 0; i < items.length - 1; i++) {
    const a = items[i]!;
    const aTop = Number(a.style.top);
    const aBottom = aTop + Number(a.style.height);
    for (let j = i + 1; j < items.length; j++) {
      const b = items[j]!;
      const bTop = Number(b.style.top);
      const bBottom = bTop + Number(b.style.height);
      if (
        (bTop <= aTop && strictlyInside(aTop, bBottom)) ||
        (aTop <= bTop && strictlyInside(bTop, aBottom))
      ) {
        a.friends.push(b);
        b.friends.push(a);
      }
    }
  }

  // Step 5: greedily assign column indices
  for (const item of items) {
    const taken = Array.from({ length: 100 }, () => 1);
    for (const friend of item.friends) {
      if (friend.idx !== undefined) {
        taken[friend.idx] = 0;
      }
    }
    item.idx = taken.indexOf(1);
  }

  // Step 6: calculate column sizes based on max friend depth
  for (const item of items) {
    if (item.size) continue;
    const visited: StyledEvent[] = [];
    const size = 100 / (maxFriendDepth(item, 0, visited) + 1);
    item.size = size;
    for (const v of visited) {
      v.size = size;
    }
  }

  // Step 7: apply final width/left/xOffset styles
  for (const item of items) {
    item.style.left = item.idx * item.size;

    // If this is the rightmost column among friends, extend to fill remaining space
    let maxFriendIdx = 0;
    for (const friend of item.friends) {
      if (friend.idx > maxFriendIdx) maxFriendIdx = friend.idx;
    }
    if (maxFriendIdx <= item.idx) {
      item.size = 100 - item.idx * item.size;
    }

    const gap = item.idx === 0 ? 0 : 3;
    item.style.width = `calc(${item.size}% - ${gap}px)`;
    // Use max() to guarantee a minimum visual height (~4px at 1440px container).
    // Plain min-height on .rbc-event doesn't prevent content from stretching the
    // absolutely-positioned box, but an inline height with max() does.
    const heightPct = Number(item.style.height);
    const MIN_HEIGHT_PCT = 0.28; // ~4px in a 1440px day column
    item.style.height = `calc(max(${heightPct}%, ${MIN_HEIGHT_PCT}%))`;
    item.style.xOffset = `calc(${item.style.left}% + ${gap}px)`;
  }

  return items;
}
```

**File:** apps/website/src/features/tracking/calendar-types.ts (L68-93)
```typescript
export function splitAtMidnight(start: Date, end: Date): Array<{ end: Date; start: Date }> {
  const segments: Array<{ end: Date; start: Date }> = [];
  let cursor = start;

  while (true) {
    const nextMidnight = new Date(cursor);
    nextMidnight.setHours(24, 0, 0, 0);

    if (nextMidnight > end) {
      segments.push({ end, start: cursor });
      break;
    }

    // End the segment 1ms before midnight so react-big-calendar keeps it
    // in the time grid instead of promoting it to an all-day header event.
    // This also covers the nextMidnight === end case (entry stopping exactly
    // at midnight of the next day): we clip the tail and break without
    // emitting a zero-duration segment on the following day.
    const segmentEnd = new Date(nextMidnight.getTime() - 1);
    segments.push({ end: segmentEnd, start: cursor });
    if (nextMidnight.getTime() === end.getTime()) break;
    cursor = nextMidnight;
  }

  return segments;
}
```

**File:** apps/website/src/features/tracking/calendar-events-builder.ts (L17-23)
```typescript
const MINUTE_MS = 60_000;
function displayEndForCalendar(start: Date, end: Date): Date {
  const startMinute = Math.floor(start.getTime() / MINUTE_MS);
  const endMinute = Math.floor(end.getTime() / MINUTE_MS);
  if (startMinute !== endMinute) return end;
  return new Date((startMinute + 1) * MINUTE_MS);
}
```

**File:** apps/website/src/features/tracking/calendar.css (L1-100)
```css
/* react-big-calendar base + drag-and-drop */
@import "react-big-calendar/lib/css/react-big-calendar.css";
@import "react-big-calendar/lib/addons/dragAndDrop/styles.css";

/*
 * Dark-theme overrides to match Toggl Track calendar.
 * All values sampled from the production Toggl UI.
 */

/* ── Calendar container ── */
.rbc-calendar {
  color: var(--track-text);
  font-family: inherit;
}

.rbc-time-view {
  background: var(--track-surface);
  border: none;
}

/* ── Header row (day columns) ── */
/* Sticky below the timer bar so day names stay visible during window scroll.
   Toggl dark: background var(--track-surface), border-bottom matches var(--track-border).
   The timer bar is sticky at top:0; adjust top value if the bar height changes. */
/* Toggl dark: padding 10px 0 3px, margin 0 0 -1px, only border-bottom var(--track-border).
   No border-left on header-content, no borders between day cells.
   --timer-header-height is set by WorkspaceTimerPage on the timer header element. */
.rbc-time-header {
  position: sticky;
  top: var(--timer-header-height, 70px);
  z-index: 15;
  background: var(--track-surface);
  border-top: 1px solid var(--track-accent-tint-subtle);
  border-bottom: 1px solid var(--track-border);
  padding: 10px 0 3px;
  margin: 0 0 -1px;
}

.rbc-time-header-content {
  border-left: none;
}

.rbc-header {
  background: transparent;
  border-bottom: none;
  color: var(--track-text);
  padding: 0;
  font-weight: 500;
  font-size: 12px;
  text-align: left;
  overflow: visible;
}

/* Toggl: no vertical separators between day header cells */
.rbc-header + .rbc-header {
  border-left: none;
}

.rbc-header.rbc-today {
  color: var(--track-accent);
}

.rbc-button-link {
  color: inherit;
  font: inherit;
}

/* ── Time gutter (left labels) ── */
.rbc-time-header-gutter {
  background: transparent;
}

.rbc-time-gutter .rbc-timeslot-group {
  border-bottom: 1px solid transparent;
  min-height: 60px;
}

/* Toggl dark: 11px, font-weight 600, color var(--track-text-soft), padding 0 6px 0 14px */
.rbc-time-gutter .rbc-label {
  color: var(--track-text-soft);
  font-size: 11px;
  font-weight: 600;
  padding: 0 6px 0 14px;
}

/* ── Time content area ── */
/* Toggl overrides RBC's default overflow-y:auto to visible so the calendar
   grid has no internal scrollbar — all scrolling is via window.scroll. */
.rbc-time-content {
  border-top: 1px solid var(--track-overlay-border);
  overflow: visible;
}

.rbc-time-content > .rbc-time-gutter {
  border-right: none;
}

/* ── Day columns / slot grid ── */
/* Toggl dark: timeslot-group has both border-bottom AND border-left var(--track-border).
   The border-left provides the vertical column separator (not on .rbc-day-slot). */
```

**File:** apps/website/src/features/tracking/CalendarView.tsx (L151-203)
```typescript
  const wrapperRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    const wrapper = wrapperRef.current;
    if (!wrapper) return;

    const EARLIEST_EVENT_HEADER_MARGIN_PX = 80;

    const computeEarliestEventAbsoluteTop = (): number | null => {
      const cards = wrapper.querySelectorAll<HTMLElement>('[data-testid^="calendar-entry-"]');
      if (cards.length === 0) return null;
      let min = Number.POSITIVE_INFINITY;
      for (const card of cards) {
        const rect = card.getBoundingClientRect();
        // Skip zero-height placeholders during layout.
        if (rect.height <= 0) continue;
        const absoluteTop = rect.top + window.scrollY;
        if (absoluteTop < min) min = absoluteTop;
      }
      return Number.isFinite(min) ? min : null;
    };

    const applyScrollFor = (indicator: HTMLElement) => {
      const indicatorAbsoluteY = indicator.getBoundingClientRect().top + window.scrollY;
      const nowCenteredTarget = indicatorAbsoluteY - window.innerHeight / 2;
      const earliestEventTop = computeEarliestEventAbsoluteTop();
      const target =
        earliestEventTop !== null
          ? Math.min(nowCenteredTarget, earliestEventTop - EARLIEST_EVENT_HEADER_MARGIN_PX)
          : nowCenteredTarget;
      if (target > 0) {
        window.scrollTo({ top: target, behavior: "instant" });
        wrapper.dataset.scrollToNow = "done";
      } else {
        wrapper.dataset.scrollToNow = "skipped";
      }
    };

    const existing = wrapper.querySelector<HTMLElement>(".rbc-current-time-indicator");
    if (existing) {
      applyScrollFor(existing);
      return;
    }

    const observer = new MutationObserver(() => {
      const el = wrapper.querySelector<HTMLElement>(".rbc-current-time-indicator");
      if (el) {
        applyScrollFor(el);
        observer.disconnect();
      }
    });
    observer.observe(wrapper, { childList: true, subtree: true });
    return () => observer.disconnect();
  }, []);
```

**File:** apps/website/src/features/tracking/CalendarView.tsx (L303-321)
```typescript
        onEventDrop={({ event, start, end }: EventInteractionArgs<CalendarEvent>) => {
          const nextStart = new Date(start);
          const nextEnd = new Date(end);
          const minutesDelta = Math.round((nextStart.getTime() - event.start.getTime()) / 60_000);
          // A drop is a MOVE: onMoveEntry already shifts both start and
          // stop in a single PUT. Do NOT additionally fire onResizeEntry
          // here — that would issue a second concurrent PUT computed from
          // the stale pre-move snapshot, and last-write-wins would reset
          // `start` back to the original.
          if (minutesDelta !== 0) {
            void onMoveEntry?.(event.id, minutesDelta);
          }
          (window as Window & { __calendarDragResult?: unknown }).__calendarDragResult = {
            eventId: event.id,
            minutesDelta,
            start: nextStart.toISOString(),
            end: nextEnd.toISOString(),
          };
        }}
```
