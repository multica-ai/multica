/**
 * Per-task execution trace — what the agent is/was thinking and which tools
 * it called. HarmonyOS port of apps/mobile/components/chat/chat-timeline.tsx.
 * Rendered:
 *
 *   - Live (under the StatusPill while a task is in flight), AND
 *   - Persisted (under the assistant bubble once the message has landed)
 *
 * Process steps (thinking / tool_use / tool_result / error) collapse
 * behind a single "N steps" toggle. Final text is NOT rendered here —
 * the parent renders the assistant message's `content` as its own
 * markdown block.
 *
 * Folds use the ported `Collapsible` (Root + Trigger + Content over a pure
 * RN state machine — same semantics as the RNR primitive, no `asChild`).
 * `defaultOpen` is emulated with local state seeded from `isStreaming` so
 * the user still sees activity while streaming, and the persisted instance
 * below an assistant bubble starts closed (matches web's
 * `OuterProcessFold` behaviour).
 *
 * Platform deltas: @expo/vector-icons → <Icon>; the iOS hardcoded grays
 * (#71717a chevron, #a1a1aa bulb, #dc2626 error) map to theme tokens
 * (mutedForeground, withAlpha(mutedForeground, 0.65), destructive) so dark
 * mode stays correct; NativeWind classes → StyleSheet styles.
 */
import { useState } from "react";
import { StyleSheet, Text, View } from "react-native";
import type { TaskMessagePayload } from "@multica/core/types";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Icon } from "@/components/ui/icon";

interface Props {
  items: TaskMessagePayload[];
  /** Whether the owning task is still running. Drives the default-open
   *  state and the dot-pulse next to the trigger. */
  isStreaming?: boolean;
}

export function ChatTimeline({ items, isStreaming = false }: Props) {
  const c = useThemeColors();
  const s = styles(c);
  // Emulates the iOS Collapsible `defaultOpen` — the primitive's controlled
  // prop would freeze the fold, so seed local state once instead.
  const [open, setOpen] = useState(isStreaming);
  const processSteps = items.filter((i) => i.type !== "text");
  if (processSteps.length === 0) return null;

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger
        style={({ pressed }) => [s.triggerRow, pressed && shared.pressed]}
        accessibilityRole="button"
        accessibilityLabel={`${processSteps.length} step${processSteps.length === 1 ? "" : "s"}`}
      >
        <Icon name="chevron-forward" size={12} color={c.mutedForeground} />
        {isStreaming ? <StreamingDot tint={c.primary} /> : null}
        <Text style={s.triggerLabel}>
          {processSteps.length === 1
            ? "1 step"
            : `${processSteps.length} steps`}
        </Text>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <View style={s.stepsBox}>
          {processSteps.map((item) => (
            <StepRow key={`${item.task_id}-${item.seq}`} item={item} />
          ))}
        </View>
      </CollapsibleContent>
    </Collapsible>
  );
}

// Theme-independent styles (the pressed cue, the streaming dot shell).
const shared = StyleSheet.create({
  // h-1.5 w-1.5 rounded-full (tint from the caller)
  dotBase: { width: 6, height: 6, borderRadius: 3 },
  // active:opacity-70 — shared pressed cue for every fold trigger
  pressed: { opacity: 0.7 },
});

function StreamingDot({ tint }: { tint: string }) {
  // Single accent dot beside the trigger so the user knows the rows
  // below may still be growing. Real "agent is alive" cue is StatusPill
  // (breathing dots) above; this is a quiet co-signal.
  return <View style={[shared.dotBase, { backgroundColor: tint }]} />;
}

function StepRow({ item }: { item: TaskMessagePayload }) {
  switch (item.type) {
    case "thinking":
      return <ThinkingRow item={item} />;
    case "tool_use":
      return <ToolCallRow item={item} />;
    case "tool_result":
      return <ToolResultRow item={item} />;
    case "error":
      return <ErrorRow item={item} />;
    default:
      return null;
  }
}

function ThinkingRow({ item }: { item: TaskMessagePayload }) {
  const c = useThemeColors();
  const s = styles(c);
  const text = item.content ?? "";
  if (!text) return null;
  const preview = text.length > 80 ? `${text.slice(0, 80)}…` : text;
  return (
    <Collapsible>
      <CollapsibleTrigger
        style={({ pressed }) => [s.stepTrigger, pressed && shared.pressed]}
      >
        <Icon
          name="bulb-outline"
          size={12}
          color={withAlpha(c.mutedForeground, 0.65)}
        />
        <Text style={s.stepPreview} numberOfLines={1}>
          {preview}
        </Text>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <Text style={s.stepFull}>{text}</Text>
      </CollapsibleContent>
    </Collapsible>
  );
}

function ToolCallRow({ item }: { item: TaskMessagePayload }) {
  const c = useThemeColors();
  const s = styles(c);
  const summary = getToolSummary(item);
  const hasInput = !!item.input && Object.keys(item.input).length > 0;
  // If the call has no expandable input, render a non-interactive row —
  // wrapping a static row in Collapsible adds a wasted tap target.
  if (!hasInput) {
    return (
      <View style={s.staticRow}>
        <View style={s.indent} />
        <Text style={s.toolName}>{item.tool ?? "tool"}</Text>
        {summary ? (
          <Text style={s.stepPreview} numberOfLines={1}>
            {summary}
          </Text>
        ) : null}
      </View>
    );
  }
  return (
    <Collapsible>
      <CollapsibleTrigger
        style={({ pressed }) => [s.stepTriggerCenter, pressed && shared.pressed]}
      >
        <Icon name="chevron-forward" size={12} color={c.mutedForeground} />
        <Text style={s.toolName}>{item.tool ?? "tool"}</Text>
        {summary ? (
          <Text style={s.stepPreview} numberOfLines={1}>
            {summary}
          </Text>
        ) : null}
      </CollapsibleTrigger>
      <CollapsibleContent>
        <View style={s.detailBox}>
          <Text style={s.detailText}>
            {JSON.stringify(item.input, null, 2)}
          </Text>
        </View>
      </CollapsibleContent>
    </Collapsible>
  );
}

function ToolResultRow({ item }: { item: TaskMessagePayload }) {
  const c = useThemeColors();
  const s = styles(c);
  const output = item.output ?? "";
  if (!output) return null;
  const preview = output.length > 80 ? `${output.slice(0, 80)}…` : output;
  const prefix = item.tool ? `${item.tool} result: ` : "result: ";
  return (
    <Collapsible>
      <CollapsibleTrigger
        style={({ pressed }) => [s.stepTrigger, pressed && shared.pressed]}
      >
        <Icon name="chevron-forward" size={12} color={c.mutedForeground} />
        <Text style={s.stepPreview} numberOfLines={1}>
          <Text style={s.stepPrefix}>{prefix}</Text>
          {preview}
        </Text>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <View style={s.detailBox}>
          <Text style={s.detailText}>
            {output.length > 4000
              ? `${output.slice(0, 4000)}\n…(truncated)`
              : output}
          </Text>
        </View>
      </CollapsibleContent>
    </Collapsible>
  );
}

function ErrorRow({ item }: { item: TaskMessagePayload }) {
  const c = useThemeColors();
  return (
    <View style={styles(c).errorRow}>
      <Icon name="alert-circle" size={12} color={c.destructive} />
      <Text style={styles(c).errorText} numberOfLines={3}>
        {item.content}
      </Text>
    </View>
  );
}

/**
 * Mirror of web's `getToolSummary` (chat-message-list.tsx) — picks the most
 * informative single-line summary from a tool_use payload. Order matters:
 * `query` / `file_path` / `pattern` are the headline params, `command` /
 * `prompt` get truncated, and a final loop catches whichever short string
 * a future tool might emit.
 */
function getToolSummary(item: TaskMessagePayload): string {
  if (!item.input) return "";
  const inp = item.input as Record<string, unknown>;
  const pick = (k: string): string | undefined => {
    const v = inp[k];
    return typeof v === "string" && v.length > 0 ? v : undefined;
  };
  const q = pick("query");
  if (q) return q;
  const fp = pick("file_path") ?? pick("path");
  if (fp) return shortenPath(fp);
  const p = pick("pattern");
  if (p) return p;
  const d = pick("description");
  if (d) return d;
  const cmd = pick("command");
  if (cmd) return cmd.length > 100 ? `${cmd.slice(0, 100)}…` : cmd;
  const prompt = pick("prompt");
  if (prompt) return prompt.length > 100 ? `${prompt.slice(0, 100)}…` : prompt;
  const skill = pick("skill");
  if (skill) return skill;
  for (const v of Object.values(inp)) {
    if (typeof v === "string" && v.length > 0 && v.length < 120) return v;
  }
  return "";
}

function shortenPath(p: string): string {
  const parts = p.split("/");
  if (parts.length <= 3) return p;
  return `…/${parts.slice(-2).join("/")}`;
}

const styles = (c: ReturnType<typeof useThemeColors>) =>
  StyleSheet.create({
    // flex-row items-center gap-1 active:opacity-70
    triggerRow: {
      flexDirection: "row",
      alignItems: "center",
      gap: 4,
      opacity: 1,
    },
    // text-xs text-muted-foreground
    triggerLabel: { fontSize: 12, color: c.mutedForeground },
    // mt-1 rounded-lg border border-border bg-muted/20 px-2 py-1.5 gap-0.5
    stepsBox: {
      marginTop: 4,
      borderRadius: 8,
      borderWidth: 1,
      borderColor: c.border,
      backgroundColor: withAlpha(c.muted, 0.2),
      paddingHorizontal: 8,
      paddingVertical: 6,
      gap: 2,
    },
    // py-0.5 flex-row items-start gap-1.5
    stepTrigger: {
      flexDirection: "row",
      alignItems: "flex-start",
      gap: 6,
      paddingVertical: 2,
    },
    // py-0.5 flex-row items-center gap-1.5
    stepTriggerCenter: {
      flexDirection: "row",
      alignItems: "center",
      gap: 6,
      paddingVertical: 2,
    },
    // text-xs italic text-muted-foreground flex-1
    stepPreview: {
      flex: 1,
      fontSize: 12,
      fontStyle: "italic",
      color: c.mutedForeground,
    },
    // ml-4 mt-0.5 text-xs italic text-muted-foreground
    stepFull: {
      marginLeft: 16,
      marginTop: 2,
      fontSize: 12,
      fontStyle: "italic",
      color: c.mutedForeground,
    },
    // text-xs font-medium text-foreground
    toolName: { fontSize: 12, fontWeight: "500", color: c.foreground },
    // py-0.5 flex-row items-center gap-1.5
    staticRow: {
      flexDirection: "row",
      alignItems: "center",
      gap: 6,
      paddingVertical: 2,
    },
    indent: { width: 12 },
    // text-xs text-muted-foreground
    stepPrefix: { fontSize: 12, color: c.mutedForeground },
    // ml-4 mt-1 rounded-xs bg-muted/40 px-2 py-1.5
    detailBox: {
      marginLeft: 16,
      marginTop: 4,
      borderRadius: 2,
      backgroundColor: withAlpha(c.muted, 0.4),
      paddingHorizontal: 8,
      paddingVertical: 6,
    },
    detailText: { fontSize: 12, color: c.mutedForeground },
    // py-0.5 flex-row items-start gap-1.5
    errorRow: {
      flexDirection: "row",
      alignItems: "flex-start",
      gap: 6,
      paddingVertical: 2,
    },
    // flex-1 text-xs text-destructive
    errorText: { flex: 1, fontSize: 12, color: c.destructive },
  });
