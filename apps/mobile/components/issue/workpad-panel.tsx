/**
 * WorkpadPanel (mobile) — the issue's PLAN rendered above the composer, the
 * phone counterpart of web's packages/cerebro-artifacts/views/components/
 * workpad-panel.tsx (FIR-3659, FIR-3765).
 *
 * Behavioral parity with web (apps/mobile/CLAUDE.md §Behavioral parity):
 *   - plan selection mirrors `selectIssuePlan` (kind:"plan", most-recent wins)
 *     via `selectPlanArtifact` in the query;
 *   - phase grouping + progress mirror `parseWorkpadPhases` / `workpadProgress`
 *     (same regex, same counts — lib/workpad.ts is the mobile copy);
 *   - the filter appears only with ≥2 named phases, exactly like web;
 *   - gated by the same `cerebro_workpad` flag (default-off).
 *
 * Mobile-only divergence: the whole panel is collapsible (a plan can be long
 * and the issue screen is short), controlled by local `open` state with a
 * Pressable header — the required extra state per the UI rules.
 *
 * FIR-3765 (variant A): a phase renders as a collapsible section headed by the
 * shared StatusIcon reflecting the phase's own progress (todo / in-progress /
 * done). Completed phases start collapsed; tapping a header folds it. No phase
 * filter — mirrors web's packages/cerebro-artifacts/views/components/
 * workpad-panel.tsx.
 */
import { useState } from "react";
import { Pressable, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { StatusIcon } from "@/components/ui/status-icon";
import { issuePlanOptions } from "@/data/queries/artifacts";
import { useWorkpadEnabled } from "@/data/queries/feature-flags";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import { cn } from "@/lib/utils";
import {
  parseWorkpadPhases,
  namedPhases,
  workpadProgress,
  phaseStatus,
  phaseComplete,
  type WorkpadItem,
  type WorkpadPhase,
} from "@/lib/workpad";

function StepRow({ item }: { item: WorkpadItem }) {
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;
  return (
    <View className="flex-row items-start gap-2">
      <Ionicons
        name={item.done ? "checkmark-circle" : "ellipse-outline"}
        size={16}
        color={mutedFg}
        style={{ marginTop: 1 }}
      />
      <Text
        className={cn(
          "flex-1 text-sm leading-snug",
          item.done && "text-muted-foreground line-through",
        )}
      >
        {item.text}
      </Text>
    </View>
  );
}

// PhaseSection — a named phase as a collapsible section: a status icon for the
// phase's own progress, its title, count, and its steps when expanded.
function PhaseSection({
  phase,
  open,
  onToggle,
}: {
  phase: WorkpadPhase;
  open: boolean;
  onToggle: () => void;
}) {
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;
  const progress = workpadProgress(phase.items);
  return (
    <View className="gap-1.5">
      <Pressable
        onPress={onToggle}
        accessibilityRole="button"
        accessibilityLabel={phase.title ?? undefined}
        accessibilityState={{ expanded: open }}
        className="flex-row items-center gap-2 active:opacity-70"
      >
        <Ionicons
          name={open ? "chevron-down" : "chevron-forward"}
          size={14}
          color={mutedFg}
        />
        <StatusIcon status={phaseStatus(progress)} size={14} />
        <Text className="flex-1 text-sm font-medium" numberOfLines={1}>
          {phase.title}
        </Text>
        <Text className="text-xs text-muted-foreground">
          {progress.done}/{progress.total}
        </Text>
      </Pressable>
      {open && (
        <View className="gap-1.5 pl-6">
          {phase.items.map((item, i) => (
            <StepRow key={i} item={item} />
          ))}
        </View>
      )}
    </View>
  );
}

// StepGroup renders a run of steps with no header (the ungrouped preamble).
function StepGroup({ items }: { items: WorkpadItem[] }) {
  return (
    <View className="gap-1.5">
      {items.map((item, i) => (
        <StepRow key={i} item={item} />
      ))}
    </View>
  );
}

export function WorkpadPanel({ issueId }: { issueId: string }) {
  const enabled = useWorkpadEnabled();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const [open, setOpen] = useState(true);
  // Per-phase open overrides (keyed by title); default is collapsed-when-complete.
  const [openOverrides, setOpenOverrides] = useState<Record<string, boolean>>({});
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;

  const { data: plan } = useQuery({
    ...issuePlanOptions(wsId, issueId),
    enabled: enabled && !!wsId && !!issueId,
  });

  if (!enabled || !plan) return null;

  const phases = parseWorkpadPhases(plan.body);
  const items = phases.flatMap((p) => p.items);
  const { done, total } = workpadProgress(items);
  const showPhases = namedPhases(phases).length >= 2;

  return (
    <View className="mx-3 mb-2 rounded-lg border border-border bg-muted/40 px-3 py-2">
      <Pressable
        onPress={() => setOpen((o) => !o)}
        accessibilityRole="button"
        accessibilityLabel="Workpad plan"
        accessibilityState={{ expanded: open }}
        className="flex-row items-center gap-2 active:opacity-70"
      >
        <Ionicons name="list" size={16} color={mutedFg} />
        <Text className="text-sm font-semibold">Workpad</Text>
        {total > 0 && (
          <Text className="ml-auto text-xs text-muted-foreground">
            {done}/{total}
          </Text>
        )}
        <Ionicons
          name={open ? "chevron-down" : "chevron-forward"}
          size={16}
          color={mutedFg}
          style={total > 0 ? undefined : { marginLeft: "auto" }}
        />
      </Pressable>

      {open && (
        <View className="mt-2 gap-3">
          {plan.title ? (
            <Text className="text-xs text-muted-foreground" numberOfLines={1}>
              {plan.title}
            </Text>
          ) : null}

          {total === 0 ? (
            <Text className="text-xs text-muted-foreground">
              The plan has no steps yet.
            </Text>
          ) : showPhases ? (
            <View className="gap-2">
              {phases.map((phase, i) => {
                if (phase.title === null) {
                  return <StepGroup key={`__lead__${i}`} items={phase.items} />;
                }
                const complete = phaseComplete(workpadProgress(phase.items));
                const title = phase.title;
                const phaseOpen =
                  title in openOverrides ? openOverrides[title] : !complete;
                return (
                  <PhaseSection
                    key={title}
                    phase={phase}
                    open={phaseOpen}
                    onToggle={() =>
                      setOpenOverrides((o) => ({ ...o, [title]: !phaseOpen }))
                    }
                  />
                );
              })}
            </View>
          ) : (
            <StepGroup items={items} />
          )}
        </View>
      )}
    </View>
  );
}
