"use client";

import { useState, type ReactNode } from "react";
import {
  ChevronDown,
  ChevronRight,
  Cloud,
  Download,
  Laptop,
  Loader2,
  Lock,
  Monitor,
  Plus,
  SquareTerminal,
  TriangleAlert,
} from "lucide-react";
import type { QoderConnection } from "@multica/core/runtimes/qoder";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import { HealthDot, useHealthLabel } from "../../runtimes/components/shared";
import {
  filterDevice,
  harnessMatches,
  HARNESS_PREVIEW_COUNT,
  LOGO_STACK_LIMIT,
  previewOptions,
  type DeviceOption,
  type HarnessOption,
} from "./runtime-choice";

export type QoderChoiceState =
  | "not_configured"
  | "starting"
  | "ready"
  | "error"
  | "stopped";

export function qoderChoiceState(
  connection: QoderConnection,
  runtimeOnline: boolean,
): QoderChoiceState {
  if (!connection.available || !connection.configured) return "not_configured";
  if (!connection.enabled) return "stopped";
  if (connection.status === "error") return "error";
  return runtimeOnline && connection.status === "online" ? "ready" : "starting";
}

const QODER_NAME = "Qoder Cloud Agents";

const COMING_SOON = [
  { provider: "cursor", name: "Cursor Cloud Agents", vendor: "Cursor" },
  { provider: "claude", name: "Claude Managed Agents", vendor: "Anthropic" },
];

const ROW = "flex w-full min-w-0 items-center gap-3 px-4 text-left";
const INTERACTIVE =
  "transition-colors hover:bg-accent/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring";
const SELECTED = "bg-brand/5 shadow-[inset_2px_0_0_var(--brand)] hover:bg-brand/5";
/** Lines child rows up under their machine's tile, past the chevron. */
const CHILD_INDENT = "pl-11";

function Highlight({ text, query }: { text: string; query: string }) {
  const q = query.trim().toLowerCase();
  const index = q ? text.toLowerCase().indexOf(q) : -1;
  if (index < 0) return <>{text}</>;
  return (
    <>
      {text.slice(0, index)}
      <mark className="rounded-xs bg-warning/25 text-inherit">
        {text.slice(index, index + q.length)}
      </mark>
      {text.slice(index + q.length)}
    </>
  );
}

function Radio({ checked, disabled }: { checked: boolean; disabled?: boolean }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "flex size-4 shrink-0 items-center justify-center rounded-full border-[1.5px] border-input",
        checked && "border-brand",
        disabled && "opacity-50",
      )}
    >
      {checked ? <span className="size-2 rounded-full bg-brand" /> : null}
    </span>
  );
}

function Tile({ children, dot }: { children: ReactNode; dot?: ReactNode }) {
  return (
    <span className="relative flex size-7 shrink-0 items-center justify-center rounded-md border bg-background">
      {children}
      {dot}
    </span>
  );
}

const TILE_DOT = "absolute -bottom-0.5 -right-0.5 ring-2 ring-card";

function GroupHeader({
  icon: Icon,
  title,
  count,
}: {
  icon: typeof Monitor;
  title: string;
  count: number;
}) {
  return (
    <div className="flex h-9 items-center gap-2 bg-muted/40 px-4 text-caption">
      <Icon aria-hidden="true" className="size-3.5 text-muted-foreground" />
      <h3 className="font-semibold">{title}</h3>
      <span className="font-mono tabular-nums text-muted-foreground">{count}</span>
    </div>
  );
}

function Badge({
  tone = "muted",
  children,
}: {
  tone?: "muted" | "success" | "warning" | "destructive";
  children: ReactNode;
}) {
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center gap-1 rounded-xs px-1.5 py-0.5 text-micro font-medium",
        tone === "muted" && "bg-muted text-muted-foreground",
        tone === "success" && "bg-success/10 text-success",
        tone === "warning" && "bg-warning/10 text-warning",
        tone === "destructive" && "bg-destructive/10 text-destructive",
      )}
    >
      {children}
    </span>
  );
}

function HarnessRow({
  option,
  query,
  selected,
  indent,
  onSelect,
}: {
  option: HarnessOption;
  query: string;
  selected: boolean;
  indent: boolean;
  onSelect: () => void;
}) {
  const { t } = useT("agents");
  const online = option.runtime.status === "online";
  return (
    <button
      type="button"
      aria-pressed={selected}
      disabled={!online}
      onClick={onSelect}
      className={cn(
        ROW,
        INTERACTIVE,
        "min-h-11",
        indent && CHILD_INDENT,
        "disabled:cursor-not-allowed disabled:text-muted-foreground disabled:hover:bg-transparent",
        selected && SELECTED,
      )}
    >
      <Radio checked={selected} disabled={!online} />
      <Tile>
        <ProviderLogo
          provider={option.runtime.provider}
          className={cn("size-4", !online && "opacity-50")}
        />
      </Tile>
      <span className={cn("min-w-0 truncate text-body", selected && "font-medium")}>
        <Highlight text={option.label} query={query} />
      </span>
      {option.custom ? (
        <Badge>{t(($) => $.creation_studio.runtime_step.custom_badge)}</Badge>
      ) : null}
      {option.version ? (
        <span className="shrink-0 font-mono text-caption text-faint-foreground">
          {option.version}
        </span>
      ) : null}
    </button>
  );
}

function LogoStack({ options }: { options: HarnessOption[] }) {
  const shown = options.slice(0, LOGO_STACK_LIMIT);
  const extra = options.length - shown.length;
  return (
    <span aria-hidden="true" className="hidden items-center pr-1 sm:flex">
      {shown.map((option) => (
        <span
          key={option.runtime.id}
          className="-mr-1 flex size-5 items-center justify-center rounded-sm bg-background ring-1 ring-border"
        >
          <ProviderLogo provider={option.runtime.provider} className="size-3" />
        </span>
      ))}
      {extra > 0 ? (
        <span className="ml-2 font-mono text-micro text-muted-foreground">+{extra}</span>
      ) : null}
    </span>
  );
}

function MachineBlock({
  device,
  open,
  onToggle,
  showAll,
  onShowAll,
  starting,
  query,
  owner,
  selectedRuntimeId,
  selectedNotice,
  onSelect,
}: {
  device: DeviceOption;
  open: boolean;
  onToggle: () => void;
  showAll: boolean;
  onShowAll: () => void;
  starting: boolean;
  query: string;
  owner: string | null;
  selectedRuntimeId: string | null;
  selectedNotice: string | null;
  onSelect: (runtimeId: string) => void;
}) {
  const { t } = useT("agents");
  const healthLabel = useHealthLabel();
  const { machine, harnesses } = device;
  const searching = query.trim().length > 0;
  const offline = machine.health !== "online";
  const Icon = machine.isCurrent ? Laptop : Monitor;
  const subtitle = [owner, machine.deviceSummary].filter(Boolean).join(" · ");
  const { shown, hiddenCount } =
    searching || showAll
      ? { shown: harnesses, hiddenCount: 0 }
      : previewOptions(harnesses, selectedRuntimeId, HARNESS_PREVIEW_COUNT);
  const hiddenNames = harnesses
    .filter((option) => !shown.includes(option))
    .map((option) => option.label)
    .join(", ");

  return (
    <>
      <button
        type="button"
        aria-expanded={open}
        onClick={onToggle}
        className={cn(ROW, INTERACTIVE, "min-h-11", open && "bg-muted/20")}
      >
        <ChevronRight
          aria-hidden="true"
          className={cn(
            "size-3.5 shrink-0 text-muted-foreground transition-transform",
            open && "rotate-90",
          )}
        />
        <Tile dot={<HealthDot health={machine.health} className={TILE_DOT} />}>
          <Icon aria-hidden="true" className="size-3.5 text-muted-foreground" />
        </Tile>
        <span
          className={cn(
            "max-w-64 shrink-0 truncate text-body font-medium",
            offline && "text-muted-foreground",
          )}
        >
          <Highlight text={machine.title} query={query} />
        </span>
        {machine.isCurrent ? (
          <Badge>{t(($) => $.creation_studio.runtime_step.this_machine)}</Badge>
        ) : subtitle ? (
          <span className="min-w-0 truncate text-caption text-muted-foreground">
            {subtitle}
          </span>
        ) : null}
        <span className="ml-auto flex shrink-0 items-center gap-4 text-caption text-muted-foreground">
          {!open && harnesses.length > 0 ? <LogoStack options={harnesses} /> : null}
          <span className="hidden tabular-nums sm:inline">
            {t(($) => $.creation_studio.runtime_step.harness_count, {
              count: harnesses.length,
            })}
          </span>
          <span className="flex min-w-12 items-center justify-end gap-1.5 whitespace-nowrap">
            <HealthDot health={machine.health} />
            {healthLabel(machine.health)}
          </span>
        </span>
      </button>
      {open && harnesses.length === 0 ? (
        <div className={cn(ROW, CHILD_INDENT, "gap-2.5 py-3")}>
          {starting ? (
            <Loader2
              aria-hidden="true"
              className="size-4 shrink-0 animate-spin text-muted-foreground"
            />
          ) : (
            <SquareTerminal
              aria-hidden="true"
              className="size-4 shrink-0 text-muted-foreground"
            />
          )}
          <span className="min-w-0">
            <span className="block text-label font-medium">
              {starting
                ? t(($) => $.creation_studio.runtime_step.local_starting)
                : t(($) => $.creation_studio.runtime_step.local_empty_title)}
            </span>
            <span className="block text-caption text-muted-foreground">
              {t(($) => $.creation_studio.runtime_step.local_empty_hint)}
            </span>
          </span>
        </div>
      ) : null}
      {open
        ? shown.map((option) => {
            const selected = option.runtime.id === selectedRuntimeId;
            return (
              <div key={option.runtime.id}>
                <HarnessRow
                  option={option}
                  query={query}
                  selected={selected}
                  indent
                  onSelect={() => onSelect(option.runtime.id)}
                />
                {selected && selectedNotice ? (
                  <p
                    className={cn(
                      ROW,
                      CHILD_INDENT,
                      SELECTED,
                      "items-start gap-2 pb-2.5 text-caption text-muted-foreground",
                    )}
                  >
                    <TriangleAlert
                      aria-hidden="true"
                      className="mt-0.5 size-3.5 shrink-0 text-warning"
                    />
                    {selectedNotice}
                  </p>
                ) : null}
              </div>
            );
          })
        : null}
      {open && hiddenCount > 0 ? (
        <button
          type="button"
          onClick={onShowAll}
          className={cn(
            ROW,
            INTERACTIVE,
            CHILD_INDENT,
            "h-9 gap-1.5 text-caption text-muted-foreground hover:text-foreground",
          )}
        >
          <ChevronDown aria-hidden="true" className="size-3.5 shrink-0" />
          <span className="truncate">
            {t(($) => $.creation_studio.runtime_step.more_harnesses, {
              count: hiddenCount,
              names: hiddenNames,
            })}
          </span>
        </button>
      ) : null}
    </>
  );
}

function QoderRow({
  connection,
  state,
  selected,
  canManage,
  query,
  onSelect,
  onConnect,
  onManage,
}: {
  connection: QoderConnection;
  state: QoderChoiceState;
  selected: boolean;
  canManage: boolean;
  query: string;
  onSelect: () => void;
  onConnect: () => void;
  onManage: () => void;
}) {
  const { t } = useT("agents");
  const ready = state === "ready";
  const dot = {
    ready: "bg-success",
    starting: "bg-warning",
    not_configured: "bg-warning",
    error: "bg-destructive",
    stopped: "bg-muted-foreground/40",
  }[state];
  const errorText = state === "error" ? connection.lastError : null;
  const status = (
    <Badge
      tone={
        ready
          ? "success"
          : state === "error"
            ? "destructive"
            : state === "stopped"
              ? "muted"
              : "warning"
      }
    >
      {state === "starting" ? (
        <Loader2 aria-hidden="true" className="size-3 animate-spin" />
      ) : null}
      {t(($) => $.creation_studio.runtime_step.qoder_status[state])}
    </Badge>
  );

  let trailing: ReactNode = status;
  if (state === "not_configured" || state === "error" || state === "stopped") {
    const unconfigured = state === "not_configured";
    trailing = canManage ? (
      <>
        {unconfigured ? null : status}
        <Button size="sm" variant="outline" onClick={unconfigured ? onConnect : onManage}>
          {unconfigured
            ? t(($) => $.creation_studio.runtime_step.connect)
            : t(($) => $.creation_studio.runtime_step.settings)}
        </Button>
      </>
    ) : (
      <>
        {status}
        <span className="text-caption text-muted-foreground">
          {unconfigured
            ? t(($) => $.creation_studio.runtime_step.member_connect)
            : t(($) => $.creation_studio.runtime_step.member_fix)}
        </span>
      </>
    );
  }

  const body = (
    <>
      <Radio checked={selected} disabled={!ready} />
      <Tile dot={<span className={cn("size-2 rounded-full", TILE_DOT, dot)} />}>
        <ProviderLogo provider="qoder_cloud" className="size-4" />
      </Tile>
      <span className="min-w-0 flex-1">
        <span className={cn("block truncate text-body", selected && "font-medium")}>
          <Highlight text={QODER_NAME} query={query} />
        </span>
        <span
          className={cn(
            "block truncate text-caption",
            errorText ? "text-destructive" : "text-muted-foreground",
          )}
        >
          {errorText ||
            t(($) => $.creation_studio.runtime_step.hosted_by, { vendor: "Qoder" })}
        </span>
      </span>
    </>
  );

  if (ready) {
    return (
      <button
        type="button"
        aria-pressed={selected}
        onClick={onSelect}
        className={cn(ROW, INTERACTIVE, "min-h-13 py-2", selected && SELECTED)}
      >
        {body}
        {trailing}
      </button>
    );
  }
  return (
    <div className={cn(ROW, "min-h-13 py-2")}>
      {body}
      <span className="flex shrink-0 items-center gap-2">{trailing}</span>
    </div>
  );
}

export interface RuntimeChoiceListProps {
  local: DeviceOption | null;
  /** False on web, where no harness can run on the device in use. */
  localAvailable: boolean;
  localStarting: boolean;
  remotes: DeviceOption[];
  hiddenRemoteCount: number;
  cloud: HarnessOption[];
  qoder: QoderConnection | null;
  qoderState: QoderChoiceState | null;
  qoderSelected: boolean;
  selectedRuntimeId: string | null;
  query: string;
  currentUserId: string | null;
  canManage: boolean;
  ownerName: (ownerId: string | null) => string | null;
  onSelect: (runtimeId: string) => void;
  onSelectQoder: () => void;
  onConnectQoder: () => void;
  onManageQoder: () => void;
  onAddRemote: () => void;
}

/**
 * Every place an agent can run, as one list: machines (this one first, open)
 * and cloud services. A machine folds to a single row until opened.
 */
export function RuntimeChoiceList({
  local,
  localAvailable,
  localStarting,
  remotes,
  hiddenRemoteCount,
  cloud,
  qoder,
  qoderState,
  qoderSelected,
  selectedRuntimeId,
  query,
  currentUserId,
  canManage,
  ownerName,
  onSelect,
  onSelectQoder,
  onConnectQoder,
  onManageQoder,
  onAddRemote,
}: RuntimeChoiceListProps) {
  const { t } = useT("agents");
  const [openOverrides, setOpenOverrides] = useState<ReadonlyMap<string, boolean>>(
    () => new Map(),
  );
  const [showAllIds, setShowAllIds] = useState<ReadonlySet<string>>(() => new Set());
  const searching = query.trim().length > 0;
  const q = query.trim().toLowerCase();

  const devices = [...(local ? [local] : []), ...remotes];
  const matchedDevices = searching
    ? devices
        .map((device) => filterDevice(device, query))
        .filter((device): device is DeviceOption => device !== null)
    : devices;
  const showWebRow = !localAvailable && !searching;

  const showQoder = !!qoder && !!qoderState && (!q || QODER_NAME.toLowerCase().includes(q));
  const matchedCloud = cloud.filter((option) => harnessMatches(option, "", query));
  const soon = COMING_SOON.filter((item) => !q || item.name.toLowerCase().includes(q));

  const showMachines = matchedDevices.length > 0 || showWebRow || !searching;
  const showCloud = showQoder || matchedCloud.length > 0 || soon.length > 0;

  if (!showMachines && !showCloud) {
    return (
      <div className="rounded-lg border border-dashed px-4 py-6 text-center text-label text-muted-foreground">
        {t(($) => $.creation_studio.runtime_step.no_match, { query })}
      </div>
    );
  }

  const isOpen = (device: DeviceOption) =>
    searching ||
    (openOverrides.get(device.machine.id) ??
      (device.machine.isCurrent ||
        device.harnesses.some((option) => option.runtime.id === selectedRuntimeId)));

  const noticeFor = (device: DeviceOption) => {
    if (device.machine.isCurrent) return null;
    const selected = device.harnesses.find(
      (option) => option.runtime.id === selectedRuntimeId,
    );
    const ownerId = selected?.runtime.owner_id ?? null;
    if (!selected || !ownerId || ownerId === currentUserId) return null;
    const owner = ownerName(ownerId);
    return owner
      ? t(($) => $.creation_studio.runtime_step.remote_warning, { owner })
      : null;
  };

  return (
    <div className="divide-y overflow-hidden rounded-lg border bg-card">
      {showMachines ? (
        <>
          <GroupHeader
            icon={Monitor}
            title={t(($) => $.creation_studio.runtime_step.groups.machines)}
            count={matchedDevices.length}
          />
          {showWebRow ? (
            <div className={cn(ROW, "min-h-12 py-2")}>
              <Tile>
                <Laptop aria-hidden="true" className="size-3.5 text-muted-foreground" />
              </Tile>
              <span className="min-w-0 flex-1 text-label text-muted-foreground">
                {t(($) => $.creation_studio.runtime_step.web_local_title)}
              </span>
              <a
                href="/download"
                target="_blank"
                rel="noreferrer"
                className={buttonVariants({ size: "sm", variant: "outline" })}
              >
                <Download aria-hidden="true" className="size-3.5" />
                {t(($) => $.creation_studio.runtime_step.download_desktop)}
              </a>
            </div>
          ) : null}
          {matchedDevices.map((device) => {
            const id = device.machine.id;
            const ownerId = device.harnesses[0]?.runtime.owner_id ?? null;
            const owner = device.machine.isCurrent
              ? null
              : ownerId && ownerId === currentUserId
                ? t(($) => $.creation_studio.runtime_step.owner_you)
                : ownerName(ownerId);
            return (
              <MachineBlock
                key={id}
                device={device}
                open={isOpen(device)}
                onToggle={() =>
                  setOpenOverrides((current) =>
                    new Map(current).set(id, !isOpen(device)),
                  )
                }
                showAll={showAllIds.has(id)}
                onShowAll={() => setShowAllIds((current) => new Set(current).add(id))}
                starting={device.machine.isCurrent && localStarting}
                query={query}
                owner={owner}
                selectedRuntimeId={selectedRuntimeId}
                selectedNotice={noticeFor(device)}
                onSelect={onSelect}
              />
            );
          })}
          {!searching ? (
            <div className={cn(ROW, "h-10 text-caption text-muted-foreground")}>
              {hiddenRemoteCount > 0 ? (
                <span className="flex min-w-0 items-center gap-1.5">
                  <Lock aria-hidden="true" className="size-3 shrink-0" />
                  <span className="truncate">
                    {t(($) => $.creation_studio.runtime_step.hidden_remote, {
                      count: hiddenRemoteCount,
                    })}
                  </span>
                </span>
              ) : null}
              <button
                type="button"
                onClick={onAddRemote}
                className="ml-auto inline-flex shrink-0 items-center gap-1 rounded-sm font-medium text-brand hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <Plus aria-hidden="true" className="size-3.5" />
                {t(($) => $.creation_studio.runtime_step.add_remote)}
              </button>
            </div>
          ) : null}
        </>
      ) : null}
      {showCloud ? (
        <>
          <GroupHeader
            icon={Cloud}
            title={t(($) => $.creation_studio.runtime_step.groups.cloud)}
            count={(showQoder ? 1 : 0) + matchedCloud.length}
          />
          {showQoder && qoder && qoderState ? (
            <QoderRow
              connection={qoder}
              state={qoderState}
              selected={qoderSelected}
              canManage={canManage}
              query={query}
              onSelect={onSelectQoder}
              onConnect={onConnectQoder}
              onManage={onManageQoder}
            />
          ) : null}
          {matchedCloud.map((option) => (
            <HarnessRow
              key={option.runtime.id}
              option={option}
              query={query}
              selected={option.runtime.id === selectedRuntimeId}
              indent={false}
              onSelect={() => onSelect(option.runtime.id)}
            />
          ))}
          {soon.map((item) => (
            <div
              key={item.provider}
              aria-disabled="true"
              className={cn(ROW, "min-h-13 py-2 text-muted-foreground")}
            >
              <Radio checked={false} disabled />
              <Tile>
                <ProviderLogo provider={item.provider} className="size-4 opacity-50 grayscale" />
              </Tile>
              <span className="min-w-0 flex-1">
                <span className="block truncate text-body">
                  <Highlight text={item.name} query={query} />
                </span>
                <span className="block truncate text-caption text-faint-foreground">
                  {t(($) => $.creation_studio.runtime_step.hosted_by, {
                    vendor: item.vendor,
                  })}
                </span>
              </span>
              <Badge>{t(($) => $.creation_studio.runtime_step.coming_soon)}</Badge>
            </div>
          ))}
        </>
      ) : null}
    </div>
  );
}
