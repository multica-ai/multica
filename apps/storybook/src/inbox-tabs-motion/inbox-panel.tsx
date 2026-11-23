import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { AnimatePresence, cubicBezier, motion, useMotionValue, type Transition } from "motion/react";
import {
  AlarmClock,
  Archive,
  CheckCircle2,
  ChevronDown,
  Circle,
  CircleDot,
  CircleEqual,
  CircleSlash,
  Filter,
  FolderKanban,
  Inbox,
  MoreHorizontal,
} from "@multica/ui/icons";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { UI_EASE_IN, UI_EASE_OUT, UI_MOTION_DURATION } from "@multica/ui/lib/motion";
import { cn } from "@multica/ui/lib/utils";
import type { Lang } from "../voice-input/copy";
import { NEXT_SECTIONS, ROWS, SECTION_LIMIT, TAB_LABELS, TABS, type Row, type RowStatus, type TabId } from "./data";
import type { MotionOption } from "./options";

export interface MotionEnv {
  /** How long a tab's first load takes; Inbox and Snoozed are always warm. */
  latency: number;
  /** 1 for real time, 0.25 to watch the motion slowed down. */
  speed: number;
  reduce: boolean;
}

/** Sent to every panel at once, so the compare page plays them in step. */
export interface PanelCommand {
  seq: number;
  action: TabId | "reset";
}

function useSeconds(env: MotionEnv) {
  return (seconds: number) => (env.reduce ? 0 : seconds / env.speed);
}

const TAB_ICONS: Record<TabId, typeof Inbox> = { inbox: Inbox, snoozed: AlarmClock, archived: Archive };
const EASE_OUT = cubicBezier(...UI_EASE_OUT);

/**
 * The Work panel's Inbox tabs as shipped in #11268 (the open tab named, the
 * others icons), with the option's motion on top.
 */
function MotionTabs({
  option,
  env,
  lang,
  tab,
  onSelect,
  unread,
  snoozed,
}: {
  option: MotionOption;
  env: MotionEnv;
  lang: Lang;
  tab: TabId;
  onSelect: (tab: TabId) => void;
  unread: number;
  snoozed: number;
}) {
  const seconds = useSeconds(env);
  const listRef = useRef<HTMLDivElement>(null);
  const x = useMotionValue(0);
  const width = useMotionValue(0);
  const placed = useRef(false);
  const travel = option.pill === "travel";
  const travelMs = seconds(UI_MOTION_DURATION.standard) * 1000;

  // Both tabs change width while the highlight travels, so a target measured
  // at the start would be stale: each frame eases from where the highlight
  // started toward the open tab's live box, and lands with the labels.
  useLayoutEffect(() => {
    const list = listRef.current;
    if (!travel || !list) return;
    const box = () => {
      const button = list.querySelector<HTMLElement>(`[data-tab="${tab}"]`);
      if (!button) return null;
      const rect = button.getBoundingClientRect();
      return { x: rect.left - list.getBoundingClientRect().left, w: rect.width };
    };
    const snap = () => {
      const target = box();
      if (!target) return;
      x.set(target.x);
      width.set(target.w);
    };
    let frame = 0;
    if (!placed.current || travelMs === 0) {
      snap();
      placed.current = true;
    } else {
      const from = { x: x.get(), w: width.get() };
      const start = performance.now();
      const step = () => {
        const progress = Math.min(1, (performance.now() - start) / travelMs);
        const eased = EASE_OUT(progress);
        const target = box();
        if (target) {
          x.set(from.x + (target.x - from.x) * eased);
          width.set(from.w + (target.w - from.w) * eased);
        }
        frame = progress < 1 ? requestAnimationFrame(step) : 0;
      };
      step();
    }
    // A count that changes later resizes the tab without a switch.
    const observer = new ResizeObserver(() => frame === 0 && snap());
    list.querySelectorAll("[data-tab]").forEach((el) => observer.observe(el));
    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
    };
  }, [tab, travel, travelMs, x, width]);

  const unfold = {
    duration: option.pill === "instant" ? 0 : seconds(option.pill === "fade" ? UI_MOTION_DURATION.fast : UI_MOTION_DURATION.standard),
    ease: UI_EASE_OUT,
  };
  const labels = TAB_LABELS[lang];

  return (
    <div ref={listRef} role="tablist" aria-label={labels.inbox} className="relative -ml-1.5 flex min-w-0 items-center gap-0.5">
      {travel && (
        <motion.span aria-hidden className="absolute top-0 left-0 h-5 rounded-md bg-surface-selected" style={{ x, width }} />
      )}
      {TABS.map((id) => {
        const active = id === tab;
        const Icon = TAB_ICONS[id];
        const count = id === "inbox" ? unread : id === "snoozed" ? snoozed : 0;
        const button = (
          <button
            type="button"
            role="tab"
            data-tab={id}
            aria-selected={active}
            onClick={() => onSelect(id)}
            style={option.pill === "fade" ? { transitionDuration: `${seconds(UI_MOTION_DURATION.fast) * 1000}ms` } : undefined}
            className={cn(
              "relative flex h-5 items-center rounded-md px-1 outline-none focus-visible:outline-2 focus-visible:outline-ring",
              option.pill === "fade" && "transition-[background-color,color] ease-out",
              active ? "min-w-0 text-foreground" : "shrink-0 text-muted-foreground hover:text-foreground",
              active && !travel && "bg-surface-selected",
            )}
          >
            <motion.span
              aria-hidden
              initial={false}
              animate={{ width: active ? 0 : "auto", opacity: active ? 0 : 1 }}
              transition={unfold}
              className="flex shrink-0 items-center overflow-hidden"
            >
              <Icon className="size-3.5 shrink-0" />
            </motion.span>
            <TabLabel
              active={active}
              transition={unfold}
              label={labels[id]}
              trailing={
                id === "snoozed" && count > 0 ? <span className="shrink-0 pr-0.5 text-faint-foreground tabular-nums">{count}</span> : null
              }
            />
            {id === "inbox" && count > 0 && (
              <span className={cn("shrink-0 text-brand tabular-nums", active ? "pr-0.5 pl-0.5" : "ml-1")}>{count}</span>
            )}
          </button>
        );
        return (
          <Tooltip key={id} disabled={active}>
            <TooltipTrigger render={button} />
            <TooltipContent side="bottom">{count > 0 ? `${labels[id]} · ${count}` : labels[id]}</TooltipContent>
          </Tooltip>
        );
      })}
    </div>
  );
}

/**
 * A tab's name. While it unfolds or folds it is clipped, not truncated, so no
 * ellipsis flickers through the motion; once open it truncates as shipped.
 */
function TabLabel({
  active,
  transition,
  label,
  trailing,
}: {
  active: boolean;
  transition: Transition;
  label: string;
  trailing: ReactNode;
}) {
  const [settled, setSettled] = useState(active);
  const [was, setWas] = useState(active);
  if (was !== active) {
    setWas(active);
    setSettled(active && transition.duration === 0);
  }
  return (
    <motion.span
      initial={false}
      animate={{ width: active ? "auto" : 0, opacity: active ? 1 : 0 }}
      transition={transition}
      onAnimationComplete={() => setSettled(active)}
      className="flex min-w-0 items-center overflow-hidden"
    >
      <span className={cn("px-0.5", settled ? "truncate" : "whitespace-nowrap")}>{label}</span>
      {trailing}
    </motion.span>
  );
}

const STATUS_ICON: Record<RowStatus, { Icon: typeof Circle; className: string }> = {
  todo: { Icon: Circle, className: "text-muted-foreground" },
  in_progress: { Icon: CircleDot, className: "text-muted-foreground" },
  in_review: { Icon: CircleEqual, className: "text-brand" },
  done: { Icon: CheckCircle2, className: "text-brand" },
  blocked: { Icon: CircleSlash, className: "text-destructive" },
};

const ROW_CLASS =
  "flex min-h-8 w-full items-center gap-2.5 rounded-nav-item px-3 py-1.5 text-left text-body text-nav-item hover:bg-nav-item-hover hover:text-nav-item-hover-foreground";

function InboxRow({ row, lang }: { row: Row; lang: Lang }) {
  const { Icon, className } = STATUS_ICON[row.status];
  return (
    <li>
      <div className={ROW_CLASS}>
        <Icon aria-hidden className={cn("size-4 shrink-0", className)} />
        <span className={cn("min-w-0 flex-1 truncate", row.unread && "font-medium text-foreground")}>{row.title[lang]}</span>
        <span className="shrink-0 text-caption text-faint-foreground tabular-nums">{row.time[lang]}</span>
        {row.unread && <span aria-hidden className="size-1.5 shrink-0 rounded-full bg-brand" />}
      </div>
    </li>
  );
}

function TabRows({ tab, lang }: { tab: TabId; lang: Lang }) {
  const rows = ROWS[tab];
  const hidden = rows.length - SECTION_LIMIT;
  return (
    <ul className="px-1.5">
      {rows.slice(0, SECTION_LIMIT).map((row) => (
        <InboxRow key={row.key} row={row} lang={lang} />
      ))}
      {hidden > 0 && (
        <li>
          <div className={cn(ROW_CLASS, "pl-[2.375rem] text-caption")}>
            {lang === "zh" ? `显示另外 ${hidden} 条` : `Show ${hidden} more`}
          </div>
        </li>
      )}
    </ul>
  );
}

/**
 * The section's rows. `instant` is today: the list swaps, and while a tab
 * loads it renders nothing. The others keep the height through the load and
 * ease to the next one.
 */
function SectionBody({
  option,
  env,
  tab,
  dir,
  collapsed,
  loading,
  children,
}: {
  option: MotionOption;
  env: MotionEnv;
  tab: TabId;
  dir: number;
  collapsed: boolean;
  loading: boolean;
  children: ReactNode;
}) {
  const seconds = useSeconds(env);
  const innerRef = useRef<HTMLDivElement>(null);
  const [height, setHeight] = useState<number | null>(null);

  // Between the old rows leaving and the new ones arriving (or loading) the
  // list is empty for a moment; a tab always renders rows or a note, so 0 is
  // never a height to ease to.
  useLayoutEffect(() => {
    const inner = innerRef.current;
    if (!inner) return;
    const sync = () => {
      if (inner.offsetHeight > 0) setHeight(inner.offsetHeight);
    };
    sync();
    const observer = new ResizeObserver(sync);
    observer.observe(inner);
    return () => observer.disconnect();
  }, [option.list]);

  if (option.list === "instant") return collapsed || loading ? null : <div>{children}</div>;

  const shift = option.list === "direction" ? 16 : 0;
  const variants = {
    enter: (d: number) => ({ opacity: 0, x: d * shift }),
    show: { opacity: 1, x: 0, transition: { duration: seconds(UI_MOTION_DURATION.fast), ease: UI_EASE_OUT } },
    exit: (d: number) => ({ opacity: 0, x: -d * shift, transition: { duration: seconds(UI_MOTION_DURATION.micro), ease: UI_EASE_IN } }),
  };

  return (
    <motion.div
      initial={false}
      animate={{ height: collapsed ? 0 : (height ?? "auto") }}
      transition={{ duration: seconds(UI_MOTION_DURATION.standard), ease: UI_EASE_OUT }}
      className="overflow-hidden"
    >
      <div ref={innerRef} className="relative">
        <AnimatePresence mode="wait" initial={false} custom={dir}>
          {!loading && (
            <motion.div key={tab} custom={dir} variants={variants} initial="enter" animate="show" exit="exit">
              {children}
            </motion.div>
          )}
        </AnimatePresence>
      </div>
    </motion.div>
  );
}

export function InboxPanel({
  option,
  env,
  lang,
  command,
  width = 280,
}: {
  option: MotionOption;
  env: MotionEnv;
  lang: Lang;
  command?: PanelCommand;
  width?: number;
}) {
  const [tab, setTab] = useState<TabId>("inbox");
  const [collapsed, setCollapsed] = useState(false);
  const [warm, setWarm] = useState<ReadonlySet<TabId>>(() => new Set<TabId>(["inbox", "snoozed"]));
  const [loading, setLoading] = useState<TabId | null>(null);
  const [dir, setDir] = useState(1);
  const timer = useRef<number | undefined>(undefined);

  const select = (next: TabId) => {
    if (next === tab) {
      setCollapsed((value) => !value);
      return;
    }
    setDir(TABS.indexOf(next) > TABS.indexOf(tab) ? 1 : -1);
    setTab(next);
    setCollapsed(false);
    window.clearTimeout(timer.current);
    if (!warm.has(next) && env.latency > 0) {
      setLoading(next);
      timer.current = window.setTimeout(() => {
        setWarm((set) => new Set(set).add(next));
        setLoading(null);
      }, env.latency);
    } else {
      setLoading(null);
      setWarm((set) => new Set(set).add(next));
    }
  };

  const seq = command?.seq;
  useEffect(() => {
    if (!command) return;
    if (command.action === "reset") {
      window.clearTimeout(timer.current);
      setTab("inbox");
      setCollapsed(false);
      setLoading(null);
      setWarm(new Set<TabId>(["inbox", "snoozed"]));
      return;
    }
    select(command.action);
    // Only a new command acts; `select` changes every render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [seq]);

  useEffect(() => () => window.clearTimeout(timer.current), []);

  const unread = ROWS.inbox.filter((row) => row.unread).length;

  return (
    <div className="rounded-[10px] bg-sidebar pt-1 pb-3" style={{ width }}>
      <section data-panel-section="inbox" className="group/section">
        <div className="flex h-7 items-center gap-1 pr-1 pl-3">
          <div className="flex min-w-0 flex-1 items-center gap-0.5 text-caption font-medium text-muted-foreground">
            <MotionTabs
              key={`${env.speed}-${env.reduce}`}
              option={option}
              env={env}
              lang={lang}
              tab={tab}
              onSelect={select}
              unread={unread}
              snoozed={ROWS.snoozed.length}
            />
            {collapsed && <ChevronDown aria-hidden className="size-3.5 shrink-0 -rotate-90" />}
          </div>
          <div className="flex items-center gap-1 text-muted-foreground opacity-0 group-hover/section:opacity-100">
            <span className="flex size-5 items-center justify-center rounded-md">
              <Filter className="size-3.5" />
            </span>
            <span className="flex size-5 items-center justify-center rounded-md">
              <MoreHorizontal className="size-3.5" />
            </span>
          </div>
        </div>
        <SectionBody option={option} env={env} tab={tab} dir={dir} collapsed={collapsed} loading={loading === tab}>
          <TabRows tab={tab} lang={lang} />
        </SectionBody>
      </section>
      {NEXT_SECTIONS.map((section, index) => (
        <section key={section.label.en} className="mt-5">
          <h3 className="flex h-7 items-center pl-3 text-caption font-medium text-muted-foreground">{section.label[lang]}</h3>
          <ul className="px-1.5">
            {section.rows.map((row) => (
              <li key={row.en}>
                <div className={ROW_CLASS}>
                  {index === 0 ? (
                    <CircleDot aria-hidden className="size-4 shrink-0 text-muted-foreground" />
                  ) : (
                    <FolderKanban aria-hidden className="size-4 shrink-0 text-muted-foreground" />
                  )}
                  <span className="min-w-0 flex-1 truncate">{row[lang]}</span>
                </div>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}
