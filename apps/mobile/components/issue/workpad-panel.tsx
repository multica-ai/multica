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
 * Pressable header — the required extra state per the UI rules. Web has no
 * collapse (it sits in a wide sidebar-less column).
 */
import { useState } from "react";
import { Pressable, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
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
  type WorkpadItem,
  type WorkpadPhase,
} from "@/lib/workpad";

const ALL = "__all__";

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

function PhaseBlock({ phase }: { phase: WorkpadPhase }) {
  const { done, total } = workpadProgress(phase.items);
  return (
    <View className="gap-1.5">
      {phase.title !== null && (
        <View className="flex-row items-center gap-2">
          <Text className="text-xs font-semibold text-muted-foreground">
            {phase.title}
          </Text>
          <Text className="text-xs text-muted-foreground">
            {done}/{total}
          </Text>
        </View>
      )}
      <View className="gap-1.5">
        {phase.items.map((item, i) => (
          <StepRow key={i} item={item} />
        ))}
      </View>
    </View>
  );
}

function PhaseChip({
  label,
  active,
  onPress,
}: {
  label: string;
  active: boolean;
  onPress: () => void;
}) {
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityState={{ selected: active }}
      className={cn(
        "rounded-full border px-2.5 py-1 active:opacity-70",
        active ? "border-transparent bg-primary" : "border-border",
      )}
    >
      <Text
        className={cn(
          "text-xs",
          active ? "text-primary-foreground" : "text-muted-foreground",
        )}
      >
        {label}
      </Text>
    </Pressable>
  );
}

export function WorkpadPanel({ issueId }: { issueId: string }) {
  const enabled = useWorkpadEnabled();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const [open, setOpen] = useState(true);
  const [selected, setSelected] = useState<string>(ALL);
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
  const named = namedPhases(phases);
  const showSelector = named.length >= 2;
  // Filter to the selected phase (by title); fall back to all phases when the
  // selection is stale (the plan changed and the title no longer exists).
  const filtered =
    showSelector && selected !== ALL
      ? phases.filter((p) => p.title === selected)
      : phases;
  const visible = filtered.length > 0 ? filtered : phases;

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

          {showSelector && (
            <View className="flex-row flex-wrap items-center gap-1.5">
              <PhaseChip
                label="All"
                active={selected === ALL}
                onPress={() => setSelected(ALL)}
              />
              {named.map((p) => (
                <PhaseChip
                  key={p.title as string}
                  label={p.title as string}
                  active={selected === p.title}
                  onPress={() => setSelected(p.title as string)}
                />
              ))}
            </View>
          )}

          {total === 0 ? (
            <Text className="text-xs text-muted-foreground">
              The plan has no steps yet.
            </Text>
          ) : (
            <View className="gap-3">
              {visible.map((phase, i) => (
                <PhaseBlock key={phase.title ?? `__lead__${i}`} phase={phase} />
              ))}
            </View>
          )}
        </View>
      )}
    </View>
  );
}
