/**
 * Single row inside the agent-runs formSheet route
 * (`app/(app)/[workspace]/issue/[id]/runs.tsx`). Same component for active
 * and past tasks —
 * the trailing Cancel button is conditional on `status in {queued,
 * dispatched, running}`, and the status badge / colour swaps based on the
 * AgentTask.status enum.
 *
 * Tapping a past row is a no-op in v1 — the transcript-detail screen is
 * explicitly out of scope per /Users/qingnaiyuan/.claude/plans/
 * ok-plan-linked-taco.md.
 */
import { Alert, Pressable, View } from "react-native";
import type { AgentTask } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { useCancelTask } from "@/data/mutations/issues";
import { useActorLookup } from "@/data/use-actor-name";
import { timeAgo } from "@/lib/time-ago";
import { translate } from "@/i18n";

interface Props {
  task: AgentTask;
  issueId: string;
}

const ACTIVE_STATUSES: readonly AgentTask["status"][] = [
  "queued",
  "dispatched",
  "running",
];

export function RunRow({ task, issueId }: Props) {
  const { getName } = useActorLookup();
  const isActive = ACTIVE_STATUSES.includes(task.status);
  const summary = task.trigger_summary?.trim() || fallbackSummary(task);
  // Past tasks use completed_at when present (server fills it for terminal
  // statuses); active tasks fall back to created_at so the user sees how
  // long it's been waiting.
  const timestamp = task.completed_at || task.created_at;

  return (
    <View className="flex-row items-start gap-3 py-2">
      <ActorAvatar type="agent" id={task.agent_id} size={28} showPresence />
      <View className="flex-1 gap-1">
        <Text className="text-sm text-foreground" numberOfLines={2}>
          <Text className="font-medium">{getName("agent", task.agent_id)}</Text>
          <Text className="text-muted-foreground"> · {summary}</Text>
        </Text>
        <View className="flex-row items-center gap-2">
          <StatusBadge task={task} />
          <Text className="text-xs text-muted-foreground">
            {timestamp ? timeAgo(timestamp) : ""}
          </Text>
        </View>
      </View>
      {isActive ? <CancelButton taskId={task.id} issueId={issueId} /> : null}
    </View>
  );
}

function StatusBadge({ task }: { task: AgentTask }) {
  const label = STATUS_LABEL[task.status] ?? task.status;
  const cls = STATUS_CLASS[task.status] ?? "text-muted-foreground";
  // For failed tasks, surface the failure_reason inline so users don't have
  // to drill in. Missing / empty / unrecognised stays as just "Failed".
  if (task.status === "failed" && task.failure_reason) {
    const reasonLabel = FAILURE_REASON_LABEL[task.failure_reason];
    if (reasonLabel) {
      return (
        <Text className={`text-xs ${cls}`}>
          {label} · {reasonLabel}
        </Text>
      );
    }
  }
  return <Text className={`text-xs ${cls}`}>{label}</Text>;
}

function CancelButton({
  taskId,
  issueId,
}: {
  taskId: string;
  issueId: string;
}) {
  const mutation = useCancelTask(issueId);

  const onPress = () => {
    Alert.alert(
      translate("Cancel task?"),
      translate("The agent will stop after the current step."),
      [
        { text: translate("Keep running"), style: "cancel" },
        {
          text: translate("Cancel task"),
          style: "destructive",
          onPress: () => mutation.mutate(taskId),
        },
      ],
    );
  };

  return (
    <Pressable
      onPress={onPress}
      disabled={mutation.isPending}
      className="px-3 py-1.5 rounded-md bg-secondary active:opacity-70"
    >
      <Text className="text-xs font-medium text-foreground">
        {translate("Cancel")}
      </Text>
    </Pressable>
  );
}

function fallbackSummary(task: AgentTask): string {
  switch (task.kind) {
    case "comment":
      return translate("Comment task");
    case "autopilot":
      return translate("Autopilot run");
    case "chat":
      return translate("Chat task");
    case "quick_create":
      return translate("Quick create");
    case "direct":
    default:
      return translate("Task");
  }
}

const STATUS_LABEL: Record<AgentTask["status"], string> = {
  queued: translate("Queued"),
  dispatched: translate("Starting"),
  waiting_local_directory: translate("Waiting for directory"),
  running: translate("Running"),
  completed: translate("Done"),
  failed: translate("Failed"),
  cancelled: translate("Cancelled"),
};

const STATUS_CLASS: Record<AgentTask["status"], string> = {
  queued: "text-muted-foreground",
  dispatched: "text-brand",
  waiting_local_directory: "text-muted-foreground",
  running: "text-brand",
  completed: "text-muted-foreground",
  failed: "text-destructive",
  cancelled: "text-muted-foreground",
};

// Short badge copy — deliberately terser than lib/failure-reason-label.ts,
// which backs a full-width chat bubble; this one shares a single line with the
// status word and a timestamp.
//
// Keyed by the raw wire value, not a closed enum: `failure_reason` is an open
// string that grows as classifier rules land. It held only the six
// pre-MUL-1949 coarse values until MUL-5370, so every refined `agent_error.*`
// the backend has written since fell through and the badge read just "Failed".
// An unrecognised reason still does — a compact badge is the one place where
// web's raw-wire-value fallback would overflow the row.
const FAILURE_REASON_LABEL: Record<string, string> = {
  queued_expired: translate("Queue expired"),
  runtime_offline: translate("Runtime offline"),
  runtime_recovery: translate("Runtime recovery"),
  timeout: translate("Timeout"),
  iteration_limit: translate("Iteration limit"),
  agent_blocked: translate("Needs input"),
  api_invalid_request: translate("Request rejected"),
  skill_bundle_unavailable: translate("Skill download failed"),
  runtime_cli_timeout: translate("Runtime CLI timeout"),

  "agent_error.provider_auth_or_access": translate("Auth failed"),
  "agent_error.provider_quota_limit": translate("Quota exhausted"),
  "agent_error.provider_capacity_or_rate_limit": translate("Rate limited"),
  "agent_error.provider_server_error": translate("Provider error"),
  "agent_error.provider_network": translate("Network error"),
  "agent_error.process_failure": translate("Process crashed"),
  "agent_error.empty_or_unparseable_output": translate("No usable output"),
  "agent_error.agent_timeout": translate("Agent timeout"),
  "agent_error.context_overflow": translate("Context overflow"),
  "agent_error.missing_config": translate("Config missing"),
  "agent_error.model_not_found_or_unavailable": translate("Model unavailable"),
  "agent_error.runtime_version_unsupported": translate("CLI unsupported"),
  "agent_error.runtime_missing_executable": translate("CLI not installed"),
  "agent_error.unknown": translate("Agent error"),

  agent_error: translate("Agent error"),
  codex_semantic_inactivity: translate("Codex inactivity"),
  manual: translate("Manual"),
};
