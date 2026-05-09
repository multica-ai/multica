import {
  Fragment,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  ArrowDown,
  Copy as CopyIcon,
  Search,
  Server,
  Trash2,
  X,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { toast } from "sonner";
import type { DaemonStatus } from "../../../shared/daemon-types";
import {
  DAEMON_STATE_COLORS,
  DAEMON_STATE_LABELS,
  formatUptime,
} from "../../../shared/daemon-types";
import { parseLogLine, type LogLevel, type ParsedLogLine } from "./parse-daemon-log";

interface DaemonPanelProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  status: DaemonStatus;
  /** Number of runtimes this local daemon has registered (for the context badge). */
  runtimeCount: number;
}

const MAX_LOG_LINES = 500;
const LEVELS: readonly LogLevel[] = ["DEBUG", "INFO", "WARN", "ERROR"];

const LEVEL_BADGE_CLASS: Record<LogLevel, string> = {
  DEBUG: "border-muted-foreground/25 text-muted-foreground/70",
  INFO: "border-foreground/15 text-foreground/80",
  WARN: "border-warning/40 text-warning",
  ERROR: "border-destructive/40 text-destructive",
};

// What gets rendered in the viewport — a single line or a folded group of
// consecutive lines that share the same `message`. The group form is what
// turns a wall of `DBG poll: no tasks` into a single placeholder.
type DisplayItem =
  | { kind: "line"; line: ParsedLogLine }
  | { kind: "group"; first: ParsedLogLine; rest: ParsedLogLine[] };

export function DaemonPanel({
  open,
  onOpenChange,
  status,
  runtimeCount,
}: DaemonPanelProps) {
  const [logs, setLogs] = useState<ParsedLogLine[]>([]);
  const [search, setSearch] = useState("");
  // Each level chip is an independent toggle. DEBUG is off by default so
  // poll-loop noise doesn't drown out real events when the panel opens —
  // users opt in if they want to see it.
  const [enabledLevels, setEnabledLevels] = useState<Set<LogLevel>>(
    () => new Set<LogLevel>(["INFO", "WARN", "ERROR"]),
  );
  const [autoScroll, setAutoScroll] = useState(true);
  const [expandedFields, setExpandedFields] = useState<Set<number>>(new Set());
  const [expandedGroups, setExpandedGroups] = useState<Set<number>>(new Set());

  const idCounterRef = useRef(0);
  const logContainerRef = useRef<HTMLDivElement>(null);

  // --- Log stream subscription ---
  // Active only while the modal is open. On open we replay the file's tail
  // (~200 lines) so users have context for "what just happened"; on close
  // we tear down the watcher so the main process isn't doing work for a
  // hidden UI.
  useEffect(() => {
    if (!open) return;
    setLogs([]);
    setExpandedFields(new Set());
    setExpandedGroups(new Set());
    idCounterRef.current = 0;

    window.daemonAPI.startLogStream();
    const unsub = window.daemonAPI.onLogLine((line) => {
      setLogs((prev) => {
        const id = ++idCounterRef.current;
        const parsed = parseLogLine(line, id);
        const next =
          prev.length >= MAX_LOG_LINES
            ? [...prev.slice(prev.length - MAX_LOG_LINES + 1), parsed]
            : [...prev, parsed];
        return next;
      });
    });
    return () => {
      unsub();
      window.daemonAPI.stopLogStream();
    };
  }, [open]);

  // --- Derived: counts per level (for filter chip badges) ---
  const levelCounts = useMemo(() => {
    const counts: Record<LogLevel, number> = {
      DEBUG: 0,
      INFO: 0,
      WARN: 0,
      ERROR: 0,
    };
    for (const l of logs) {
      if (l.level) counts[l.level] += 1;
    }
    return counts;
  }, [logs]);

  // --- Derived: filtered list (level toggle + search) ---
  // Lines that didn't parse (level = null) always pass — they're typically
  // panic stack traces / partial writes; never silently drop them.
  const filtered = useMemo(() => {
    let result = logs;
    result = result.filter((l) => {
      if (!l.level) return true;
      return enabledLevels.has(l.level);
    });
    if (search) {
      const q = search.toLowerCase();
      result = result.filter((l) => l.raw.toLowerCase().includes(q));
    }
    return result;
  }, [logs, enabledLevels, search]);

  // --- Derived: collapse runs of consecutive lines that share the same
  // message into a single group placeholder. The most common case is the
  // 1-min `DBG poll: no tasks` heartbeat that otherwise pushes real events
  // off-screen. Grouping happens AFTER filtering so toggling DEBUG off
  // doesn't strand groups.
  const displayed = useMemo<DisplayItem[]>(() => {
    const out: DisplayItem[] = [];
    for (const line of filtered) {
      const last = out[out.length - 1];
      if (!last) {
        out.push({ kind: "line", line });
        continue;
      }
      const lastMessage =
        last.kind === "line" ? last.line.message : last.first.message;
      if (lastMessage && lastMessage === line.message) {
        if (last.kind === "line") {
          out[out.length - 1] = {
            kind: "group",
            first: last.line,
            rest: [line],
          };
        } else {
          last.rest.push(line);
        }
      } else {
        out.push({ kind: "line", line });
      }
    }
    return out;
  }, [filtered]);

  // --- Auto-scroll: pin to bottom while live; release on user scroll ---
  useEffect(() => {
    if (!autoScroll) return;
    const el = logContainerRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [displayed, autoScroll]);

  const handleScroll = useCallback(() => {
    const el = logContainerRef.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
    // Only flip auto-scroll OFF on user-initiated scroll-up; never flip ON
    // here. Re-enabling lives in the "Jump to latest" footer button so a
    // burst of lines doesn't yank a reading user back to the bottom.
    if (!atBottom && autoScroll) setAutoScroll(false);
  }, [autoScroll]);

  const handleResume = useCallback(() => {
    setAutoScroll(true);
    const el = logContainerRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, []);

  const handleCopy = useCallback(async () => {
    const text = filtered.map((l) => l.raw).join("\n");
    try {
      await navigator.clipboard.writeText(text);
      toast.success(
        `Copied ${filtered.length} line${filtered.length === 1 ? "" : "s"}`,
      );
    } catch (err) {
      toast.error("Failed to copy", {
        description: err instanceof Error ? err.message : String(err),
      });
    }
  }, [filtered]);

  const handleClear = useCallback(() => {
    setLogs([]);
    setExpandedFields(new Set());
    setExpandedGroups(new Set());
  }, []);

  const toggleLevel = useCallback((lv: LogLevel) => {
    setEnabledLevels((prev) => {
      const next = new Set(prev);
      if (next.has(lv)) next.delete(lv);
      else next.add(lv);
      return next;
    });
  }, []);

  const toggleFields = useCallback((id: number) => {
    setExpandedFields((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const toggleGroup = useCallback((id: number) => {
    setExpandedGroups((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const hasActiveFilter = !!search || enabledLevels.size < LEVELS.length;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="flex h-[85vh] flex-col gap-0 overflow-hidden p-0 sm:max-w-5xl"
        showCloseButton={false}
      >
        {/* Header */}
        <div className="flex shrink-0 items-center justify-between gap-3 border-b px-4 py-3">
          <div className="flex min-w-0 items-center gap-2">
            <Server className="size-4 shrink-0 text-muted-foreground" />
            <DialogTitle className="text-sm font-medium">
              Local daemon logs
            </DialogTitle>
            <ContextBadge status={status} runtimeCount={runtimeCount} />
          </div>
          <button
            type="button"
            onClick={() => onOpenChange(false)}
            aria-label="Close"
            className="flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <X className="size-4" />
          </button>
        </div>

        {/* Toolbar */}
        <div className="flex shrink-0 flex-wrap items-center gap-2 border-b px-4 py-2">
          {/* Search */}
          <div className="relative w-56">
            <Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search…"
              className="h-7 w-full rounded-md border bg-background pl-7 pr-2 text-xs placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
            />
          </div>

          {/* Level toggle chips. Each chip is independent — click to
              show/hide that level. DEBUG starts hidden because the
              poll-loop heartbeat dominates otherwise. */}
          <div className="flex items-center gap-1">
            {LEVELS.map((lv) => (
              <FilterChip
                key={lv}
                active={enabledLevels.has(lv)}
                onClick={() => toggleLevel(lv)}
                label={lv}
                count={levelCounts[lv]}
                variant={lv}
              />
            ))}
          </div>

          {/* Right-aligned actions */}
          <div className="ml-auto flex items-center gap-1">
            <Button
              variant="ghost"
              size="sm"
              className="h-7"
              onClick={handleCopy}
              disabled={filtered.length === 0}
            >
              <CopyIcon className="size-3.5 mr-1.5" />
              Copy
            </Button>
            <Button
              variant="ghost"
              size="sm"
              className="h-7"
              onClick={handleClear}
              disabled={logs.length === 0}
            >
              <Trash2 className="size-3.5 mr-1.5" />
              Clear
            </Button>
          </div>
        </div>

        {/* Logs viewport */}
        <div
          ref={logContainerRef}
          onScroll={handleScroll}
          className="min-h-0 flex-1 overflow-y-auto bg-muted/20 px-2 py-1 font-mono text-xs"
        >
          {displayed.length === 0 ? (
            <EmptyState
              hasLogs={logs.length > 0}
              hasFilter={hasActiveFilter}
              isRunning={status.state === "running"}
            />
          ) : (
            <div className="flex flex-col">
              {displayed.map((item) =>
                item.kind === "line" ? (
                  <LogLineRow
                    key={item.line.id}
                    line={item.line}
                    expanded={expandedFields.has(item.line.id)}
                    onToggle={() => toggleFields(item.line.id)}
                    search={search}
                  />
                ) : (
                  <GroupRows
                    key={item.first.id}
                    first={item.first}
                    rest={item.rest}
                    expanded={expandedGroups.has(item.first.id)}
                    onToggle={() => toggleGroup(item.first.id)}
                    expandedFields={expandedFields}
                    onToggleFields={toggleFields}
                    search={search}
                  />
                ),
              )}
            </div>
          )}
        </div>

        {/* Status bar — count only. The "is the user following" state is
            communicated implicitly by the presence of the Jump-to-latest
            button below; an explicit "Paused" word read as "log stream is
            paused" (it isn't — data keeps flowing into the buffer). */}
        <div className="flex shrink-0 items-center justify-between border-t bg-muted/30 px-4 py-1.5 text-xs text-muted-foreground">
          <span className="tabular-nums">
            Showing {filtered.length} of {logs.length}
            {logs.length === MAX_LOG_LINES && (
              <span className="ml-1 text-muted-foreground/60">
                (buffer full)
              </span>
            )}
          </span>
          {!autoScroll && (
            <button
              type="button"
              onClick={handleResume}
              className="inline-flex items-center gap-1 rounded-md px-2 py-0.5 hover:bg-muted hover:text-foreground"
            >
              <ArrowDown className="size-3" />
              Jump to latest
            </button>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

// ---------- Sub-components ----------

function ContextBadge({
  status,
  runtimeCount,
}: {
  status: DaemonStatus;
  runtimeCount: number;
}) {
  const isRunning = status.state === "running";
  return (
    <span className="inline-flex items-center gap-1.5 rounded-md border bg-background px-1.5 py-0.5 text-xs font-normal">
      <span
        className={cn(
          "size-1.5 rounded-full",
          DAEMON_STATE_COLORS[status.state],
        )}
      />
      <span
        className={cn(
          "tabular-nums",
          isRunning ? "text-foreground" : "text-muted-foreground",
        )}
      >
        {DAEMON_STATE_LABELS[status.state]}
      </span>
      {isRunning && status.uptime && (
        <span className="text-muted-foreground">
          · {formatUptime(status.uptime)}
        </span>
      )}
      {isRunning && runtimeCount > 0 && (
        <span className="text-muted-foreground">
          · {runtimeCount} runtime{runtimeCount === 1 ? "" : "s"}
        </span>
      )}
    </span>
  );
}

function FilterChip({
  active,
  onClick,
  label,
  count,
  variant,
}: {
  active: boolean;
  onClick: () => void;
  label: string;
  count: number;
  variant?: LogLevel;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "inline-flex h-7 items-center gap-1 rounded-md border bg-background px-2 text-xs transition-colors hover:bg-accent",
        active
          ? variant
            ? LEVEL_BADGE_CLASS[variant]
            : "bg-accent text-accent-foreground"
          : "border-dashed text-muted-foreground/50",
      )}
    >
      {label}
      <span
        className={cn(
          "tabular-nums",
          active ? "text-current/80" : "text-muted-foreground/40",
        )}
      >
        {count}
      </span>
    </button>
  );
}

function LevelBadge({ level }: { level: LogLevel }) {
  return (
    <span
      className={cn(
        "inline-flex h-4 shrink-0 items-center rounded border px-1 text-[10px] font-medium uppercase tracking-wide",
        LEVEL_BADGE_CLASS[level],
      )}
    >
      {level}
    </span>
  );
}

function LogLineRow({
  line,
  expanded,
  onToggle,
  search,
}: {
  line: ParsedLogLine;
  expanded: boolean;
  onToggle: () => void;
  search: string;
}) {
  const fieldEntries = Object.entries(line.fields);
  const hasFields = fieldEntries.length > 0;

  // Unparseable line — render the raw text so nothing is hidden. Common
  // for panic stack traces and partial writes during log rotation.
  if (!line.timestamp || !line.level) {
    return (
      <div className="break-all whitespace-pre-wrap px-2 py-0.5 text-muted-foreground/70">
        {highlight(line.raw, search)}
      </div>
    );
  }

  return (
    <div
      className={cn(
        "grid grid-cols-[auto_auto_minmax(0,1fr)] items-baseline gap-2 rounded px-2 py-0.5 hover:bg-accent/30",
        hasFields && "cursor-pointer",
      )}
      onClick={hasFields ? onToggle : undefined}
    >
      <span className="shrink-0 tabular-nums text-muted-foreground/60">
        {line.timestamp}
      </span>
      <LevelBadge level={line.level} />
      <div className="min-w-0">
        <div className="flex min-w-0 items-baseline gap-2">
          <span className="break-words">{highlight(line.message, search)}</span>
          {hasFields && !expanded && (
            <span className="min-w-0 truncate text-muted-foreground/60">
              {fieldEntries
                .map(([k, v]) => `${k}=${truncateValue(v)}`)
                .join("  ")}
            </span>
          )}
        </div>
        {expanded && hasFields && (
          <div className="ml-1 mt-1 grid grid-cols-[max-content_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-muted-foreground">
            {fieldEntries.map(([k, v]) => (
              <Fragment key={k}>
                <span className="text-muted-foreground/70">{k}</span>
                <span className="break-all text-foreground/85">{v}</span>
              </Fragment>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function GroupRows({
  first,
  rest,
  expanded,
  onToggle,
  expandedFields,
  onToggleFields,
  search,
}: {
  first: ParsedLogLine;
  rest: ParsedLogLine[];
  expanded: boolean;
  onToggle: () => void;
  expandedFields: Set<number>;
  onToggleFields: (id: number) => void;
  search: string;
}) {
  // Folded: show the first occurrence so the user still sees a sample
  // (timestamp, level, message), then a click-to-expand placeholder for
  // the suppressed run. The placeholder uses a dashed border + italics
  // so the eye reads it as "not a real line".
  if (!expanded) {
    return (
      <>
        <LogLineRow
          line={first}
          expanded={expandedFields.has(first.id)}
          onToggle={() => onToggleFields(first.id)}
          search={search}
        />
        <button
          type="button"
          onClick={onToggle}
          className="my-0.5 ml-2 inline-flex w-fit items-center gap-2 rounded border border-dashed border-muted-foreground/25 bg-muted/30 px-2 py-0.5 text-[11px] italic text-muted-foreground/70 hover:bg-muted/60 hover:text-foreground"
        >
          <span>···</span>
          <span>
            {rest.length} more &ldquo;{truncateValue(first.message, 48)}
            &rdquo; — click to expand
          </span>
        </button>
      </>
    );
  }

  // Unfolded: render every line, then a small "collapse" affordance at
  // the end so the user can put the toothpaste back in the tube.
  return (
    <>
      <LogLineRow
        line={first}
        expanded={expandedFields.has(first.id)}
        onToggle={() => onToggleFields(first.id)}
        search={search}
      />
      {rest.map((l) => (
        <LogLineRow
          key={l.id}
          line={l}
          expanded={expandedFields.has(l.id)}
          onToggle={() => onToggleFields(l.id)}
          search={search}
        />
      ))}
      <button
        type="button"
        onClick={onToggle}
        className="my-0.5 ml-2 inline-flex w-fit items-center gap-2 rounded border border-dashed border-muted-foreground/25 px-2 py-0.5 text-[11px] italic text-muted-foreground/60 hover:text-foreground"
      >
        <span>···</span>
        <span>collapse {rest.length + 1} repeated</span>
      </button>
    </>
  );
}

function EmptyState({
  hasLogs,
  hasFilter,
  isRunning,
}: {
  hasLogs: boolean;
  hasFilter: boolean;
  isRunning: boolean;
}) {
  let title: string;
  let subtitle: string;
  if (hasFilter) {
    title = "No matching log lines";
    subtitle = "Try a different search or level toggle.";
  } else if (!isRunning) {
    title = "Daemon isn't running";
    subtitle = "Start the daemon to see logs here.";
  } else if (!hasLogs) {
    title = "Waiting for logs…";
    subtitle = "New entries will appear in real time.";
  } else {
    title = "";
    subtitle = "";
  }
  return (
    <div className="flex h-full flex-col items-center justify-center gap-1 text-center text-muted-foreground/70">
      <p className="text-sm">{title}</p>
      <p className="text-xs text-muted-foreground/50">{subtitle}</p>
    </div>
  );
}

// ---------- Helpers ----------

function truncateValue(value: string, max = 32): string {
  return value.length > max ? `${value.slice(0, max)}…` : value;
}

function highlight(text: string, query: string): ReactNode {
  if (!query) return text;
  const q = query.toLowerCase();
  const lower = text.toLowerCase();
  const idx = lower.indexOf(q);
  if (idx === -1) return text;
  return (
    <>
      {text.slice(0, idx)}
      <mark className="rounded bg-warning/30 px-0.5 text-foreground">
        {text.slice(idx, idx + query.length)}
      </mark>
      {text.slice(idx + query.length)}
    </>
  );
}
