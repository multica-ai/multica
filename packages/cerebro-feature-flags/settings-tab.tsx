"use client";

import { useMemo, useState, type ComponentType } from "react";
import { Lock, ChevronRight, Search } from "lucide-react";
import { Switch } from "@multica/ui/components/ui/switch";
import { Input } from "@multica/ui/components/ui/input";
import {
  Collapsible,
  CollapsibleTrigger,
  CollapsibleContent,
} from "@multica/ui/components/ui/collapsible";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";
import { Label } from "@multica/ui/components/ui/label";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { WakeupLimitsSettings } from "@multica/cerebro-wakeup";
import { AgentStartSettings } from "@multica/cerebro-issue-datetime/views";
import {
  CEREBRO_FLAGS,
  CEREBRO_FLAG_GROUPS,
  flagsForGroup,
  subgroupsForGroup,
  flagsForSubgroup,
  ungroupedFlags,
  type CerebroFlagDefinition,
  type CerebroFlagGroupKey,
  type CerebroFlagKey,
} from "./registry";
import {
  flagEffectiveState,
  flagStatusCopy,
  matchesFilter,
  matchesQuery,
  type FlagEffectiveState,
  type FlagFilter,
} from "./flag-status";
import { SecretaryCriteriaSettings } from "./secretary-criteria-settings";
import { RemindersSettings } from "./reminders-settings";
import { DictationSettings } from "./dictation-settings-tab";
import {
  useFeatureFlag,
  useFeatureFlagsQuery,
  useSetFeatureFlagMutation,
  useSetWorkspaceFeatureFlagMutation,
} from "./api";
import {
  useCerebroFeatureFlagsStore,
  useFlagLocked,
  useWorkspaceFlagValue,
  resolveFlag,
} from "./store";

// Per-feature inline settings. A cerebro feature can attach extra controls that
// render under its toggle, so everything for that feature lives in one place in
// the Cerebro features tab. Add a feature's settings component here keyed by its
// flag; the row renders it whenever the feature is enabled. TECH-3298.
const FLAG_SETTINGS: Partial<Record<CerebroFlagKey, ComponentType>> = {
  cerebro_wakeup_time: WakeupLimitsSettings,
  cerebro_issue_date_times: AgentStartSettings,
  cerebro_inbox_secretary: SecretaryCriteriaSettings,
  cerebro_reminders: RemindersSettings,
  cerebro_voice_dictation_enabled: DictationSettings,
};

const DOT_TONE: Record<"on" | "off" | "forced", string> = {
  on: "bg-success",
  off: "bg-muted-foreground/40",
  forced: "bg-info",
};

/** The colored dot + plain-language line: "is it on, and for whom". */
function StatusLine({ state }: { state: FlagEffectiveState }) {
  const { tone, text } = flagStatusCopy(state);
  return (
    <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
      <span className={cn("size-2 shrink-0 rounded-full", DOT_TONE[tone])} />
      {text}
    </p>
  );
}

// Exported so a feature can render its own toggles in a dedicated settings tab
// (e.g. the Notes tab, TECH-3637) with the same personal/workspace 3-way
// control instead of duplicating the logic.
export function FlagRow({
  flagKey,
  label,
  description,
  isAdmin,
}: {
  flagKey: CerebroFlagKey;
  label: string;
  description: string;
  isAdmin: boolean;
}) {
  const enabled = useFeatureFlag(flagKey);
  const locked = useFlagLocked(flagKey);
  const workspaceValue = useWorkspaceFlagValue(flagKey);
  const personalMutation = useSetFeatureFlagMutation();
  const workspaceMutation = useSetWorkspaceFeatureFlagMutation();

  // The owner's workspace-level setting, as a 3-way mode:
  //   "default" — no workspace override; members fall back to personal/default.
  //   "on"      — forced on for everyone (locked).
  //   "off"     — forced off for everyone (locked); this is the reliable
  //               org-wide kill switch that personal toggles can't restore.
  const teamMode: "default" | "on" | "off" = !locked
    ? "default"
    : workspaceValue === false
      ? "off"
      : "on";

  const state = flagEffectiveState({ enabled, locked, workspaceValue });

  // Optional inline settings for this feature, shown only while it's enabled.
  const SettingsComponent = FLAG_SETTINGS[flagKey];

  const handlePersonalChange = (next: boolean) => {
    personalMutation.mutate(
      { key: flagKey, enabled: next },
      {
        onError: (err) => {
          toast.error(err instanceof Error ? err.message : `Failed to update ${label}`);
        },
      },
    );
  };

  const handleTeamModeChange = (mode: "default" | "on" | "off") => {
    // default => clear the workspace override (members choose).
    // on/off  => force that value for everyone and lock it so a personal
    //            toggle can't override it.
    workspaceMutation.mutate(
      {
        key: flagKey,
        enabled: mode === "default" ? null : mode === "on",
        locked: mode !== "default",
      },
      {
        onError: (err) => {
          toast.error(err instanceof Error ? err.message : `Failed to update ${label}`);
        },
      },
    );
  };

  return (
    <Card>
      <CardContent>
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0 space-y-1.5">
            <Label className="text-sm font-medium">{label}</Label>
            <StatusLine state={state} />
            <p className="text-xs text-muted-foreground">{description}</p>
            {locked && !isAdmin && (
              <p className="flex items-center gap-1 text-xs text-muted-foreground">
                <Lock className="h-3 w-3" />
                {teamMode === "off"
                  ? "Turned off for the whole workspace by an owner — you can't change this."
                  : "Turned on for the whole workspace by an owner — you can't change this."}
              </p>
            )}
          </div>
          <Switch
            checked={enabled}
            disabled={personalMutation.isPending || locked}
            onCheckedChange={handlePersonalChange}
            aria-label={label}
          />
        </div>

        {isAdmin && (
          <div className="mt-3 flex items-center justify-between gap-4 border-t pt-3">
            <Label className="text-xs font-medium text-muted-foreground">
              For whom
            </Label>
            <NativeSelect
              size="sm"
              value={teamMode}
              disabled={workspaceMutation.isPending}
              onChange={(e) =>
                handleTeamModeChange(e.target.value as "default" | "on" | "off")
              }
              aria-label={`${label} — who this applies to`}
            >
              <NativeSelectOption value="default">Members choose</NativeSelectOption>
              <NativeSelectOption value="on">Forced on for everyone</NativeSelectOption>
              <NativeSelectOption value="off">Forced off for everyone</NativeSelectOption>
            </NativeSelect>
          </div>
        )}

        {SettingsComponent && enabled && (
          <div className="mt-3 border-t pt-3">
            <SettingsComponent />
          </div>
        )}
      </CardContent>
    </Card>
  );
}

/** Resolve the effective state of every flag from the store, once per render. */
function useFlagStates(): Record<CerebroFlagKey, FlagEffectiveState> {
  const overrides = useCerebroFeatureFlagsStore((s) => s.overrides);
  const workspaceOverrides = useCerebroFeatureFlagsStore((s) => s.workspaceOverrides);
  const locked = useCerebroFeatureFlagsStore((s) => s.locked);

  return useMemo(() => {
    const out = {} as Record<CerebroFlagKey, FlagEffectiveState>;
    for (const flag of CEREBRO_FLAGS) {
      out[flag.key] = flagEffectiveState({
        enabled: resolveFlag(flag.key, overrides, workspaceOverrides, locked),
        locked: !!locked[flag.key],
        workspaceValue: workspaceOverrides[flag.key],
      });
    }
    return out;
  }, [overrides, workspaceOverrides, locked]);
}

const FILTER_CHIPS: { key: FlagFilter; label: string }[] = [
  { key: "all", label: "All" },
  { key: "on", label: "On" },
  { key: "off", label: "Off" },
  { key: "forced", label: "Forced" },
];

const isEnabledState = (s: FlagEffectiveState) => s === "on" || s === "forced_on";

export function CerebroFeatureFlagsTab() {
  const query = useFeatureFlagsQuery();
  const wsId = useWorkspaceId();
  const { role } = useCurrentMember(wsId);
  const isAdmin = role === "owner" || role === "admin";

  const states = useFlagStates();
  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState<FlagFilter>("all");
  const [openOverrides, setOpenOverrides] = useState<
    Partial<Record<CerebroFlagGroupKey, boolean>>
  >({});

  const isFiltering = search.trim().length > 0 || filter !== "all";

  // A flag is visible when it matches both the search box and the chip.
  const isVisible = (flag: CerebroFlagDefinition) =>
    matchesQuery(flag, search) && matchesFilter(states[flag.key], filter);

  const counts = useMemo(() => {
    let on = 0;
    let off = 0;
    let forced = 0;
    for (const flag of CEREBRO_FLAGS) {
      const s = states[flag.key];
      if (isEnabledState(s)) on += 1;
      if (s === "off" || s === "forced_off") off += 1;
      if (s === "forced_on" || s === "forced_off") forced += 1;
    }
    return { all: CEREBRO_FLAGS.length, on, off, forced };
  }, [states]);

  const renderFlag = (flag: CerebroFlagDefinition) => (
    <FlagRow
      key={flag.key}
      flagKey={flag.key}
      label={flag.label}
      description={flag.description}
      isAdmin={isAdmin}
    />
  );

  // Pre-compute, per group, which flags survive the filter so we can hide
  // empty groups and show an accurate result count.
  const groups = CEREBRO_FLAG_GROUPS.map((group) => {
    const all = flagsForGroup(group.key);
    if (all.length === 0) return null;

    const visibleTop = ungroupedFlags(group.key).filter(isVisible);
    const visibleSubgroups = subgroupsForGroup(group.key)
      .map((sg) => ({
        meta: sg,
        flags: flagsForSubgroup(group.key, sg.key).filter(isVisible),
      }))
      .filter((sg) => sg.flags.length > 0);

    const visibleCount =
      visibleTop.length + visibleSubgroups.reduce((n, sg) => n + sg.flags.length, 0);
    if (visibleCount === 0) return null;

    const onCount = all.filter((f) => isEnabledState(states[f.key])).length;
    const open = isFiltering ? true : (openOverrides[group.key] ?? false);

    return { group, all, visibleTop, visibleSubgroups, visibleCount, onCount, open };
  }).filter((g): g is NonNullable<typeof g> => g !== null);

  const totalVisible = groups.reduce((n, g) => n + g.visibleCount, 0);

  return (
    <section className="space-y-4">
      <div className="space-y-1">
        <h2 className="text-sm font-semibold">Cerebro features</h2>
        <p className="text-xs text-muted-foreground">
          Turn features on or off and decide who they apply to. Defaults are safe:
          turning a feature off falls back to upstream multica behaviour where
          applicable, and never changes behaviour for other installs. Workspace
          owners can force a feature on or off for everyone.
        </p>
      </div>

      {/* Search + filter chips */}
      <div className="space-y-2">
        <div className="relative">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            type="search"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search features by name or what they do…"
            className="h-9 pl-8"
            aria-label="Search Cerebro features"
          />
        </div>
        <div className="flex flex-wrap items-center gap-1.5">
          {FILTER_CHIPS.map((chip) => (
            <button
              key={chip.key}
              type="button"
              onClick={() => setFilter(chip.key)}
              aria-pressed={filter === chip.key}
              className={cn(
                "flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs transition-colors",
                filter === chip.key
                  ? "border-foreground bg-foreground text-background"
                  : "border-border text-muted-foreground hover:bg-muted/50",
              )}
            >
              {chip.label}
              <span className="tabular-nums opacity-70">{counts[chip.key]}</span>
            </button>
          ))}
          {isFiltering && (
            <span className="ml-auto text-xs text-muted-foreground">
              {totalVisible} {totalVisible === 1 ? "feature" : "features"} shown
            </span>
          )}
        </div>
      </div>

      {query.isError && (
        <p className="text-xs text-destructive">
          Failed to load feature flags:{" "}
          {query.error instanceof Error ? query.error.message : "unknown error"}
        </p>
      )}

      <div className="space-y-2">
        {groups.map(({ group, all, visibleTop, visibleSubgroups, onCount, open }) => (
          <Collapsible
            key={group.key}
            open={open}
            onOpenChange={(o) =>
              setOpenOverrides((prev) => ({ ...prev, [group.key]: o }))
            }
            className="rounded-lg border"
          >
            <CollapsibleTrigger className="group flex w-full cursor-pointer items-center gap-2 rounded-lg px-3 py-2.5 text-left transition-colors hover:bg-muted/50">
              <div className="flex-1 space-y-0.5">
                <h3 className="text-sm font-semibold">{group.label}</h3>
                <p className="text-xs text-muted-foreground">{group.description}</p>
              </div>
              <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
                <span className="text-success">{onCount}</span> / {all.length} on
              </span>
              <ChevronRight className="size-4 shrink-0 text-muted-foreground transition-transform duration-200 group-data-[panel-open]:rotate-90" />
            </CollapsibleTrigger>
            <CollapsibleContent>
              <div className="space-y-3 border-t px-3 py-3">
                {visibleTop.map(renderFlag)}
                {visibleSubgroups.map(({ meta, flags }) => (
                  <div
                    key={meta.key}
                    className="rounded-lg border border-dashed bg-muted/30"
                  >
                    <div className="flex items-center justify-between gap-2 px-3 py-2.5">
                      <div className="space-y-0.5">
                        <h4 className="text-sm font-semibold">{meta.label}</h4>
                        <p className="text-xs text-muted-foreground">
                          {meta.description}
                        </p>
                      </div>
                      <span className="shrink-0 rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">
                        {flags.length} settings
                      </span>
                    </div>
                    <div className="space-y-3 border-t border-dashed px-3 py-3">
                      {flags.map(renderFlag)}
                    </div>
                  </div>
                ))}
              </div>
            </CollapsibleContent>
          </Collapsible>
        ))}

        {totalVisible === 0 && (
          <p className="rounded-lg border border-dashed px-3 py-10 text-center text-xs text-muted-foreground">
            No features match your search.
          </p>
        )}
      </div>
    </section>
  );
}
