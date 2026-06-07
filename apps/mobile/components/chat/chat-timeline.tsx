/**
 * Per-task execution trace — what the agent is/was thinking and which tools
 * it called. Rendered:
 *
 *   - Live (under the StatusPill while a task is in flight), AND
 *   - Persisted (under the assistant bubble once the message has landed)
 *
 * Process steps (thinking / tool_use / tool_result / error) collapse
 * behind a single "N steps" toggle. Final text is NOT rendered here —
 * the parent renders the assistant message's `content` (or the latest
 * streaming text) as its own markdown block.
 *
 * Folds use RNR `Collapsible` (built on `@rn-primitives/collapsible`).
 * The earlier version of this file hand-rolled four separate
 * `useState + Pressable + chevron` triggers (~60 lines of state +
 * handlers); Collapsible owns open/close + a11y semantics in one place.
 *
 * `defaultOpen` is true on the outer fold while streaming so the user
 * sees activity; the persisted instance below an assistant bubble
 * starts closed (matches web's `OuterProcessFold` behaviour in
 * `packages/views/chat/components/chat-message-list.tsx`).
 */
import { View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TaskMessagePayload } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";

interface Props {
  items: TaskMessagePayload[];
  /** Whether the owning task is still running. Drives the default-open
   *  state and the dot-pulse next to the trigger. */
  isStreaming?: boolean;
}

export function ChatTimeline({ items, isStreaming = false }: Props) {
  const processSteps = items.filter((i) => i.type !== "text");
  if (processSteps.length === 0) return null;

  return (
    <Collapsible defaultOpen={isStreaming}>
      <CollapsibleTrigger asChild>
        <View
          accessibilityRole="button"
          accessibilityLabel={`${processSteps.length} step${processSteps.length === 1 ? "" : "s"}`}
          className="flex-row items-center gap-1 active:opacity-70"
        >
          <Ionicons name="chevron-forward" size={12} color="#71717a" />
          {isStreaming ? <StreamingDot /> : null}
          <Text className="text-xs text-muted-foreground">
            {processSteps.length === 1
              ? "1 step"
              : `${processSteps.length} steps`}
          </Text>
        </View>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <View className="mt-1 rounded-lg border border-border bg-muted/20 px-2 py-1.5 gap-0.5">
          {processSteps.map((item) => (
            <StepRow key={`${item.task_id}-${item.seq}`} item={item} />
          ))}
        </View>
      </CollapsibleContent>
    </Collapsible>
  );
}

function StreamingDot() {
  // Single accent dot beside the trigger so the user knows the rows
  // below may still be growing. Real "agent is alive" cue is StatusPill
  // (breathing dots) above; this is a quiet co-signal.
  return <View className="h-1.5 w-1.5 rounded-full bg-primary" />;
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
  const text = item.content ?? "";
  if (!text) return null;
  const preview = text.length > 80 ? `${text.slice(0, 80)}…` : text;
  return (
    <Collapsible>
      <CollapsibleTrigger asChild>
        <View className="py-0.5 flex-row items-start gap-1.5 active:opacity-70">
          <Ionicons
            name="bulb-outline"
            size={12}
            color="#a1a1aa"
            style={{ marginTop: 2 }}
          />
          <Text
            className="flex-1 text-xs italic text-muted-foreground"
            numberOfLines={1}
          >
            {preview}
          </Text>
        </View>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <Text className="ml-4 mt-0.5 text-xs italic text-muted-foreground">
          {text}
        </Text>
      </CollapsibleContent>
    </Collapsible>
  );
}

function ToolCallRow({ item }: { item: TaskMessagePayload }) {
  const summary = getToolSummary(item);
  const hasInput = !!item.input && Object.keys(item.input).length > 0;
  // If the call has no expandable input, render a non-interactive row —
  // wrapping a static row in Collapsible adds a wasted tap target.
  if (!hasInput) {
    return (
      <View className="py-0.5 flex-row items-center gap-1.5">
        <View style={{ width: 12 }} />
        <Text className="text-xs font-medium text-foreground">
          {item.tool ?? "tool"}
        </Text>
        {summary ? (
          <Text
            className="flex-1 text-xs text-muted-foreground"
            numberOfLines={1}
          >
            {summary}
          </Text>
        ) : null}
      </View>
    );
  }
  return (
    <Collapsible>
      <CollapsibleTrigger asChild>
        <View className="py-0.5 flex-row items-center gap-1.5 active:opacity-70">
          <Ionicons name="chevron-forward" size={12} color="#71717a" />
          <Text className="text-xs font-medium text-foreground">
            {item.tool ?? "tool"}
          </Text>
          {summary ? (
            <Text
              className="flex-1 text-xs text-muted-foreground"
              numberOfLines={1}
            >
              {summary}
            </Text>
          ) : null}
        </View>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <View className="ml-4 mt-1 rounded bg-muted/40 px-2 py-1.5">
          <Text className="text-xs text-muted-foreground">
            {JSON.stringify(item.input, null, 2)}
          </Text>
        </View>
      </CollapsibleContent>
    </Collapsible>
  );
}

function ToolResultRow({ item }: { item: TaskMessagePayload }) {
  const output = item.output ?? "";
  if (!output) return null;
  const preview = output.length > 80 ? `${output.slice(0, 80)}…` : output;
  const prefix = item.tool ? `${item.tool} result: ` : "result: ";
  return (
    <Collapsible>
      <CollapsibleTrigger asChild>
        <View className="py-0.5 flex-row items-start gap-1.5 active:opacity-70">
          <Ionicons
            name="chevron-forward"
            size={12}
            color="#71717a"
            style={{ marginTop: 2 }}
          />
          <Text
            className="flex-1 text-xs text-muted-foreground/80"
            numberOfLines={1}
          >
            <Text className="text-xs text-muted-foreground">{prefix}</Text>
            {preview}
          </Text>
        </View>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <View className="ml-4 mt-1 rounded bg-muted/40 px-2 py-1.5">
          <Text className="text-xs text-muted-foreground">
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
  return (
    <View className="py-0.5 flex-row items-start gap-1.5">
      <Ionicons
        name="alert-circle"
        size={12}
        color="#dc2626"
        style={{ marginTop: 2 }}
      />
      <Text className="flex-1 text-xs text-destructive" numberOfLines={3}>
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
