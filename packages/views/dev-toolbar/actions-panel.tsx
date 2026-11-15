"use client";

import { useEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { useDevActionsStore } from "@multica/core/dev-actions";
import { createShortcutChord, formatShortcut, shortcutMatchesEvent } from "@multica/core/shortcuts";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { History, Pin, Play, Search, Zap } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import {
  blockedReason,
  commandTitle,
  searchCommands,
  type DevAction,
  type DevActionContext,
  type DevCommand,
  type DevFeature,
  type DevFeatureState,
} from "./actions";
import { TONE_CLASS } from "./panels";
import { DEV_COMMANDS, DEV_FEATURES, DEV_GROUPS } from "./registry";

const OPEN_SHORTCUT = createShortcutChord("K", { primary: true, shift: true });
const ACTION_COUNT = DEV_FEATURES.reduce((n, f) => n + f.actions.length, 0);
const PINNED = "Pinned";
const RECENT = "Recent";

const COMMAND_BY_KEY = new Map(DEV_COMMANDS.map((c) => [c.key, c]));
const commandsFor = (keys: readonly string[]) => keys.flatMap((key) => COMMAND_BY_KEY.get(key) ?? []);

function useDevActionContext(wsId: string | null): DevActionContext {
  const user = useAuthStore((s) => s.user);
  const queryClient = useQueryClient();
  return { wsId, user, queryClient };
}

function runCommand(command: DevCommand, ctx: DevActionContext) {
  if (blockedReason(command.action, ctx)) return;
  try {
    command.action.run(ctx, command.choice?.value);
  } catch (error) {
    console.error(error);
    toast.error(`${commandTitle(command)} failed`, {
      description: error instanceof Error ? error.message : String(error),
    });
    return;
  }
  useDevActionsStore.getState().recordRun(command.key);
}

function RunButton({ title, disabled, onClick }: { title: string; disabled: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      aria-label={`Run ${title}`}
      onClick={onClick}
      disabled={disabled}
      className="flex h-6 shrink-0 items-center rounded-md border border-border px-2 text-micro text-foreground hover:bg-accent disabled:pointer-events-none disabled:opacity-50"
    >
      Run
    </button>
  );
}

function PinButton({ command }: { command: DevCommand }) {
  const pinned = useDevActionsStore((s) => s.pinned.includes(command.key));
  const title = commandTitle(command);
  return (
    <button
      type="button"
      aria-label={pinned ? `Unpin ${title}` : `Pin ${title}`}
      aria-pressed={pinned}
      title={pinned ? "Unpin from the toolbar" : "Pin to the toolbar"}
      onClick={() => useDevActionsStore.getState().togglePin(command.key)}
      className={cn(
        "flex size-6 shrink-0 items-center justify-center rounded-md hover:bg-accent",
        pinned ? "text-brand" : "text-faint-foreground hover:text-foreground",
      )}
    >
      <Pin className="size-3" />
    </button>
  );
}

function ActionText({ action, title, crumb, blocked }: { action: DevAction; title: string; crumb?: string; blocked: string | null }) {
  return (
    <div className="flex min-w-0 flex-1 items-baseline gap-2">
      <span className="shrink-0 text-foreground">{title}</span>
      {crumb && <span className="shrink-0 text-micro text-muted-foreground">{crumb}</span>}
      <span
        className={cn("min-w-0 truncate text-micro", blocked || action.caution ? "text-warning" : "text-muted-foreground")}
        title={blocked ?? action.caution ?? action.description}
      >
        {blocked ?? action.caution ?? action.description}
      </span>
    </div>
  );
}

function CommandRow({
  command,
  ctx,
  crumb,
  highlighted,
  onHover,
  onRun,
}: {
  command: DevCommand;
  ctx: DevActionContext;
  crumb?: boolean;
  highlighted?: boolean;
  onHover?: () => void;
  onRun: () => void;
}) {
  const blocked = blockedReason(command.action, ctx);
  const title = commandTitle(command);
  return (
    <div
      data-highlighted={highlighted || undefined}
      onMouseMove={onHover}
      className="group flex items-center gap-2 px-3 py-1 hover:bg-accent/50 data-[highlighted]:bg-accent"
    >
      <Play className="size-3 shrink-0 text-muted-foreground" />
      <ActionText action={command.action} title={title} crumb={crumb ? command.feature.title : undefined} blocked={blocked} />
      <PinButton command={command} />
      <RunButton title={title} disabled={blocked !== null} onClick={onRun} />
    </div>
  );
}

/** An action that runs with a value: picking one runs it. */
function ChoiceRow({ action, commands, ctx }: { action: DevAction; commands: DevCommand[]; ctx: DevActionContext }) {
  const blocked = blockedReason(action, ctx);
  return (
    <div className="flex items-center gap-2 px-3 py-1 hover:bg-accent/50">
      <Play className="size-3 shrink-0 text-muted-foreground" />
      <ActionText action={action} title={action.title} blocked={blocked} />
      <select
        aria-label={action.title}
        value=""
        disabled={blocked !== null}
        onChange={(e) => {
          const command = commands.find((c) => c.choice?.value === e.target.value);
          if (command) runCommand(command, ctx);
        }}
        className="h-6 shrink-0 rounded-md border border-border bg-background px-1 font-mono text-micro disabled:opacity-50"
      >
        <option value="" disabled>
          Run with…
        </option>
        {commands.map((c) => (
          <option key={c.key} value={c.choice!.value}>
            {c.choice!.label}
          </option>
        ))}
      </select>
    </div>
  );
}

function FeatureBlock({ feature, ctx }: { feature: DevFeature; ctx: DevActionContext }) {
  const state = (feature.useDevState ?? noState)(ctx);
  return (
    <section aria-label={feature.title} className="border-b border-border py-2 last:border-b-0">
      <div className="flex items-center gap-2 px-3 pb-1">
        <span className="font-medium text-foreground">{feature.title}</span>
        {state.status && (
          <span className={cn("ml-auto min-w-0 truncate font-mono text-micro", TONE_CLASS[state.status.tone ?? "normal"])}>
            {state.status.text}
          </span>
        )}
      </div>
      {state.rows && state.rows.length > 0 && (
        <div className="mx-3 mb-1 rounded-md bg-muted/50 px-2 py-1">
          {state.rows.map((row) => (
            <div key={row.label} className="flex min-h-5 items-center gap-3">
              <span className="w-28 shrink-0 text-muted-foreground">{row.label}</span>
              <span className={cn("min-w-0 flex-1 truncate font-mono tabular-nums", TONE_CLASS[row.tone ?? "normal"])}>
                {row.value}
              </span>
            </div>
          ))}
        </div>
      )}
      {state.note && <div className="px-3 pb-1 text-micro text-muted-foreground">{state.note}</div>}
      {feature.actions.map((action) => {
        const commands = DEV_COMMANDS.filter((c) => c.feature === feature && c.action === action);
        return action.choices ? (
          <ChoiceRow key={action.id} action={action} commands={commands} ctx={ctx} />
        ) : (
          <CommandRow key={action.id} command={commands[0]!} ctx={ctx} onRun={() => runCommand(commands[0]!, ctx)} />
        );
      })}
    </section>
  );
}

const noState = (): DevFeatureState => ({});

function NavItem({ label, count, icon, active, onClick }: { label: string; count: number; icon?: ReactNode; active: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      aria-current={active || undefined}
      onClick={onClick}
      className={cn(
        "flex h-7 w-full items-center gap-2 rounded-md px-2 text-left hover:bg-accent hover:text-foreground",
        active ? "bg-accent text-foreground" : "text-muted-foreground",
      )}
    >
      {icon}
      <span className="min-w-0 flex-1 truncate">{label}</span>
      <span className="font-mono text-micro tabular-nums text-muted-foreground">{count}</span>
    </button>
  );
}

function Empty({ children }: { children: ReactNode }) {
  return <div className="px-3 py-8 text-center text-muted-foreground">{children}</div>;
}

function SearchResults({
  query,
  results,
  active,
  ctx,
  onHighlight,
  onRun,
}: {
  query: string;
  results: DevCommand[];
  active: number;
  ctx: DevActionContext;
  onHighlight: (index: number) => void;
  onRun: (command: DevCommand) => void;
}) {
  const listRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    listRef.current?.querySelector("[data-highlighted]")?.scrollIntoView?.({ block: "nearest" });
  }, [active]);

  if (results.length === 0) return <Empty>{`No action matches “${query}”.`}</Empty>;
  return (
    <div ref={listRef} aria-label="Matching actions" className="min-h-0 flex-1 overflow-y-auto py-1">
      {results.map((command, i) => (
        <CommandRow
          key={command.key}
          command={command}
          ctx={ctx}
          crumb
          highlighted={i === active}
          onHover={() => onHighlight(i)}
          onRun={() => onRun(command)}
        />
      ))}
    </div>
  );
}

/**
 * Every feature's dev actions, grouped, with the state that decides what they
 * do. Typing searches them all; Enter runs the highlighted one and closes.
 */
export function ActionsPanel({ wsId, onClose }: { wsId: string | null; onClose: () => void }) {
  const ctx = useDevActionContext(wsId);
  const [query, setQuery] = useState("");
  const [highlight, setHighlight] = useState(0);
  const results = searchCommands(DEV_COMMANDS, query);
  const active = Math.min(highlight, Math.max(0, results.length - 1));
  const runAndClose = (command: DevCommand) => {
    runCommand(command, ctx);
    onClose();
  };
  const onSearchKey = (e: ReactKeyboardEvent) => {
    if (results.length === 0) return;
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      setHighlight(Math.min(Math.max(0, active + (e.key === "ArrowDown" ? 1 : -1)), results.length - 1));
    } else if (e.key === "Enter" && !blockedReason(results[active]!.action, ctx)) {
      e.preventDefault();
      runAndClose(results[active]!);
    }
  };
  const pinned = commandsFor(useDevActionsStore((s) => s.pinned));
  const recent = commandsFor(useDevActionsStore((s) => s.recent));
  const stored = useDevActionsStore((s) => s.section);
  const section = stored === PINNED || stored === RECENT || DEV_GROUPS.includes(stored ?? "") ? stored! : DEV_GROUPS[0]!;
  const setSection = useDevActionsStore((s) => s.setSection);

  const list = (commands: DevCommand[], empty: string) =>
    commands.length === 0 ? (
      <Empty>{empty}</Empty>
    ) : (
      <div className="py-1">
        {commands.map((command) => (
          <CommandRow key={command.key} command={command} ctx={ctx} crumb onRun={() => runCommand(command, ctx)} />
        ))}
      </div>
    );

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex h-10 shrink-0 items-center gap-2 border-b border-border px-3">
        <Search className="size-3.5 shrink-0 text-muted-foreground" />
        <input
          autoFocus
          aria-label="Search actions"
          placeholder={`Search ${ACTION_COUNT} actions…`}
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            setHighlight(0);
          }}
          onKeyDown={onSearchKey}
          className="min-w-0 flex-1 bg-transparent text-body text-foreground outline-none placeholder:text-muted-foreground"
        />
        <kbd className="shrink-0 font-mono text-micro text-muted-foreground">{formatShortcut(OPEN_SHORTCUT)}</kbd>
      </div>
      {query.trim() ? (
        <SearchResults
          query={query}
          results={results}
          active={active}
          ctx={ctx}
          onHighlight={setHighlight}
          onRun={runAndClose}
        />
      ) : (
        <div className="flex min-h-0 flex-1">
          <nav aria-label="Action groups" className="flex w-44 shrink-0 flex-col gap-0.5 overflow-y-auto border-r border-border p-1.5">
            <NavItem
              label={PINNED}
              count={pinned.length}
              icon={<Pin className="size-3" />}
              active={section === PINNED}
              onClick={() => setSection(PINNED)}
            />
            <NavItem
              label={RECENT}
              count={recent.length}
              icon={<History className="size-3" />}
              active={section === RECENT}
              onClick={() => setSection(RECENT)}
            />
            <div className="my-1 border-t border-border" />
            {DEV_GROUPS.map((group) => (
              <NavItem
                key={group}
                label={group}
                count={DEV_FEATURES.filter((f) => f.group === group).reduce((n, f) => n + f.actions.length, 0)}
                active={section === group}
                onClick={() => setSection(group)}
              />
            ))}
          </nav>
          <div className="min-w-0 flex-1 overflow-y-auto">
            {section === PINNED
              ? list(pinned, "Pin an action to keep it here and on the toolbar.")
              : section === RECENT
                ? list(recent, "Actions you run show up here.")
                : DEV_FEATURES.filter((f) => f.group === section).map((feature) => (
                    <FeatureBlock key={feature.id} feature={feature} ctx={ctx} />
                  ))}
          </div>
        </div>
      )}
    </div>
  );
}

/**
 * The toolbar's Actions entry: pinned actions as one-click chips, then the
 * button that opens the panel, which ⌘⇧K (Ctrl+Shift+K) also toggles from
 * anywhere in the app.
 */
export function DevActions({ wsId }: { wsId: string | null }) {
  const [open, setOpen] = useState(false);
  const ctx = useDevActionContext(wsId);
  const pinned = commandsFor(useDevActionsStore((s) => s.pinned));

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!shortcutMatchesEvent(OPEN_SHORTCUT, e)) return;
      e.preventDefault();
      setOpen((o) => !o);
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, []);

  return (
    <>
      {pinned.map((command) => {
        const blocked = blockedReason(command.action, ctx);
        return (
          <button
            key={command.key}
            type="button"
            disabled={blocked !== null}
            title={blocked ?? `${command.feature.title} · ${command.action.description ?? commandTitle(command)}`}
            onClick={() => runCommand(command, ctx)}
            className="flex h-5 items-center gap-1 whitespace-nowrap rounded-md px-1 hover:bg-accent hover:text-foreground disabled:opacity-50"
          >
            <Play className="size-2.5 text-brand" />
            {commandTitle(command)}
          </button>
        );
      })}
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          data-testid="dev-toolbar-actions"
          className="flex h-5 items-center gap-1 whitespace-nowrap rounded-md px-1 hover:bg-accent hover:text-foreground data-[popup-open]:bg-accent data-[popup-open]:text-foreground"
        >
          <Zap className="size-3" />
          <span>Actions</span>
          <span className="tabular-nums text-foreground">{ACTION_COUNT}</span>
        </PopoverTrigger>
        <PopoverContent side="top" align="end" sideOffset={6} className="h-[520px] w-[720px] gap-0 overflow-hidden p-0 text-caption">
          <ActionsPanel wsId={wsId} onClose={() => setOpen(false)} />
        </PopoverContent>
      </Popover>
    </>
  );
}
