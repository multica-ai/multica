import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ActivityIndicator,
  Alert,
  ScrollView,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { api } from "@multica/core/api";
import { useWSEvent } from "@multica/core/realtime";
import type { AgentTask, TaskMessagePayload } from "@multica/core/types";
import { Avatar } from "@/components/Avatar";
import { colors } from "@/lib/theme";

type TimelineItem = {
  seq: number;
  type: TaskMessagePayload["type"];
  tool?: string;
  content?: string;
  input?: Record<string, unknown>;
  output?: string;
};

type TaskState = {
  task: AgentTask;
  items: TimelineItem[];
};

export function AgentLiveLog({
  issueId,
  getAgentName,
  getAgentAvatar,
}: {
  issueId: string;
  getAgentName: (agentId: string | null) => string;
  getAgentAvatar: (agentId: string | null) => string | null;
}) {
  const [taskStates, setTaskStates] = useState<Map<string, TaskState>>(new Map());
  const [history, setHistory] = useState<AgentTask[]>([]);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const seenSeqs = useRef(new Set<string>());

  const loadActiveTasks = useCallback(() => {
    setLoading(true);
    api
      .getActiveTasksForIssue(issueId)
      .then(({ tasks }) => {
        setTaskStates((prev) => {
          const next = new Map(prev);
          for (const task of tasks) {
            if (!next.has(task.id)) next.set(task.id, { task, items: [] });
          }
          for (const taskId of Array.from(next.keys())) {
            if (!tasks.some((task) => task.id === taskId)) next.delete(taskId);
          }
          return next;
        });

        for (const task of tasks) {
          api
            .listTaskMessages(task.id)
            .then((messages) => {
              for (const message of messages) {
                seenSeqs.current.add(`${message.task_id}:${message.seq}`);
              }
              const items = buildTimeline(messages);
              setTaskStates((prev) => {
                const next = new Map(prev);
                const existing = next.get(task.id);
                if (!existing) return next;
                const loadedSeqs = new Set(items.map((item) => item.seq));
                const wsOnly = existing.items.filter(
                  (item) => !loadedSeqs.has(item.seq),
                );
                next.set(task.id, {
                  task: existing.task,
                  items: [...items, ...wsOnly].sort((a, b) => a.seq - b.seq),
                });
                return next;
              });
            })
            .catch(() => undefined);
        }
      })
      .finally(() => setLoading(false));
  }, [issueId]);

  const loadHistory = useCallback(() => {
    api
      .listTasksByIssue(issueId)
      .then((tasks) => setHistory(tasks))
      .catch(() => undefined);
  }, [issueId]);

  useEffect(() => {
    loadActiveTasks();
    loadHistory();
  }, [loadActiveTasks, loadHistory]);

  useWSEvent(
    "task:dispatch",
    useCallback(
      (payload: unknown) => {
        const p = payload as { issue_id?: string };
        if (p.issue_id && p.issue_id !== issueId) return;
        loadActiveTasks();
      },
      [issueId, loadActiveTasks],
    ),
  );

  useWSEvent(
    "task:message",
    useCallback(
      (payload: unknown) => {
        const message = payload as TaskMessagePayload;
        if (message.issue_id !== issueId) return;
        const key = `${message.task_id}:${message.seq}`;
        if (seenSeqs.current.has(key)) return;
        seenSeqs.current.add(key);
        const item = messageToTimelineItem(message);

        setTaskStates((prev) => {
          const next = new Map(prev);
          const existing = next.get(message.task_id);
          if (!existing) return next;
          next.set(message.task_id, {
            ...existing,
            items: [...existing.items, item].sort((a, b) => a.seq - b.seq),
          });
          return next;
        });
      },
      [issueId],
    ),
  );

  const handleTaskEnd = useCallback(
    (payload: unknown) => {
      const p = payload as { task_id: string; issue_id: string };
      if (p.issue_id !== issueId) return;
      setTaskStates((prev) => {
        const next = new Map(prev);
        next.delete(p.task_id);
        return next;
      });
      loadHistory();
    },
    [issueId, loadHistory],
  );

  useWSEvent("task:completed", handleTaskEnd);
  useWSEvent("task:failed", handleTaskEnd);
  useWSEvent("task:cancelled", handleTaskEnd);

  const activeTasks = useMemo(
    () => Array.from(taskStates.values()),
    [taskStates],
  );
  const completedHistory = history.filter((task) =>
    ["completed", "failed", "cancelled"].includes(task.status),
  );

  if (loading && activeTasks.length === 0 && completedHistory.length === 0) {
    return (
      <View style={styles.loadingRow}>
        <ActivityIndicator size="small" />
        <Text style={styles.mutedText}>Checking agent runs...</Text>
      </View>
    );
  }

  if (activeTasks.length === 0 && completedHistory.length === 0) return null;

  return (
    <View style={styles.container}>
      {activeTasks.map(({ task, items }) => (
        <LiveTaskCard
          key={task.id}
          issueId={issueId}
          task={task}
          items={items}
          agentName={getAgentName(task.agent_id)}
          avatarUrl={getAgentAvatar(task.agent_id)}
        />
      ))}

      {completedHistory.length > 0 && (
        <View style={styles.historyBlock}>
          <TouchableOpacity
            style={styles.historyHeader}
            onPress={() => setHistoryOpen((open) => !open)}
            activeOpacity={0.7}
          >
            <Text style={styles.historyTitle}>
              Execution history ({completedHistory.length})
            </Text>
            <Text style={styles.historyChevron}>{historyOpen ? "−" : "+"}</Text>
          </TouchableOpacity>
          {historyOpen &&
            completedHistory.map((task) => (
              <TaskHistoryRow
                key={task.id}
                task={task}
                agentName={getAgentName(task.agent_id)}
              />
            ))}
        </View>
      )}
    </View>
  );
}

function LiveTaskCard({
  issueId,
  task,
  items,
  agentName,
  avatarUrl,
}: {
  issueId: string;
  task: AgentTask;
  items: TimelineItem[];
  agentName: string;
  avatarUrl: string | null;
}) {
  const [open, setOpen] = useState(true);
  const [cancelling, setCancelling] = useState(false);
  const toolCount = items.filter((item) => item.type === "tool_use").length;

  const handleCancel = useCallback(async () => {
    if (cancelling) return;
    setCancelling(true);
    try {
      await api.cancelTask(issueId, task.id);
    } catch (err) {
      setCancelling(false);
      Alert.alert(
        "Error",
        err instanceof Error ? err.message : "Failed to stop agent",
      );
    }
  }, [cancelling, issueId, task.id]);

  return (
    <View style={styles.card}>
      <TouchableOpacity
        style={styles.cardHeader}
        onPress={() => setOpen((value) => !value)}
        activeOpacity={0.7}
      >
        <Avatar name={agentName} avatarUrl={avatarUrl} size={28} isAgent />
        <View style={styles.cardHeaderText}>
          <Text style={styles.cardTitle} numberOfLines={1}>
            {agentName} is working
          </Text>
          <Text style={styles.cardMeta}>
            {task.status} {toolCount > 0 ? `| ${toolCount} tools` : ""}
          </Text>
        </View>
        <TouchableOpacity
          style={styles.stopButton}
          onPress={handleCancel}
          disabled={cancelling}
        >
          <Text style={styles.stopButtonText}>
            {cancelling ? "Stopping" : "Stop"}
          </Text>
        </TouchableOpacity>
      </TouchableOpacity>

      {open && (
        <View style={styles.timeline}>
          {items.length > 0 ? (
            <ScrollView style={styles.timelineScroll} nestedScrollEnabled>
              {items.map((item, index) => (
                <TimelineRow key={`${item.seq}-${index}`} item={item} />
              ))}
            </ScrollView>
          ) : (
            <Text style={styles.mutedText}>
              Waiting for live log messages...
            </Text>
          )}
        </View>
      )}
    </View>
  );
}

function TaskHistoryRow({
  task,
  agentName,
}: {
  task: AgentTask;
  agentName: string;
}) {
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<TimelineItem[] | null>(null);

  const loadMessages = useCallback(() => {
    if (items !== null) return;
    api
      .listTaskMessages(task.id)
      .then((messages) => setItems(buildTimeline(messages)))
      .catch(() => setItems([]));
  }, [items, task.id]);

  return (
    <View style={styles.historyRow}>
      <TouchableOpacity
        style={styles.historyRowHeader}
        onPress={() => {
          setOpen((value) => !value);
          loadMessages();
        }}
        activeOpacity={0.7}
      >
        <Text style={styles.historyStatus}>{task.status}</Text>
        <Text style={styles.historyAgent} numberOfLines={1}>
          {agentName}
        </Text>
        <Text style={styles.historyDate}>
          {formatShortDate(task.completed_at ?? task.created_at)}
        </Text>
      </TouchableOpacity>
      {open && (
        <View style={styles.historyTimeline}>
          {items === null ? (
            <ActivityIndicator size="small" />
          ) : items.length > 0 ? (
            items.map((item, index) => (
              <TimelineRow key={`${item.seq}-${index}`} item={item} compact />
            ))
          ) : (
            <Text style={styles.mutedText}>No execution data recorded.</Text>
          )}
        </View>
      )}
    </View>
  );
}

function TimelineRow({
  item,
  compact = false,
}: {
  item: TimelineItem;
  compact?: boolean;
}) {
  const summary = timelineSummary(item);
  if (!summary) return null;

  return (
    <View style={styles.timelineRow}>
      <Text style={styles.timelineKind}>{labelForType(item.type)}</Text>
      <Text
        style={[styles.timelineText, compact && styles.timelineTextCompact]}
        numberOfLines={compact ? 4 : undefined}
      >
        {summary}
      </Text>
    </View>
  );
}

function buildTimeline(messages: TaskMessagePayload[]) {
  return messages.map(messageToTimelineItem).sort((a, b) => a.seq - b.seq);
}

function messageToTimelineItem(message: TaskMessagePayload): TimelineItem {
  return {
    seq: message.seq,
    type: message.type,
    tool: message.tool,
    content: message.content ? redactSecrets(message.content) : message.content,
    input: message.input,
    output: message.output ? redactSecrets(message.output) : message.output,
  };
}

function timelineSummary(item: TimelineItem) {
  switch (item.type) {
    case "tool_use":
      return `${item.tool ?? "tool"} ${toolInputSummary(item.input)}`;
    case "tool_result":
      return item.output ?? "";
    case "thinking":
    case "text":
    case "error":
      return item.content ?? "";
  }
}

function labelForType(type: TimelineItem["type"]) {
  switch (type) {
    case "tool_use":
      return "tool";
    case "tool_result":
      return "result";
    case "thinking":
      return "think";
    case "error":
      return "error";
    case "text":
      return "log";
  }
}

function toolInputSummary(input?: Record<string, unknown>) {
  if (!input) return "";
  const keys = ["description", "command", "file_path", "path", "query", "pattern", "prompt"];
  for (const key of keys) {
    const value = input[key];
    if (typeof value === "string" && value.trim()) return truncate(value.trim(), 140);
  }
  return truncate(JSON.stringify(input), 140);
}

function truncate(value: string, max: number) {
  return value.length > max ? `${value.slice(0, max)}...` : value;
}

function redactSecrets(value: string) {
  return value
    .replace(/(sk-[A-Za-z0-9_-]{12,})/g, "[REDACTED]")
    .replace(/(ghp_[A-Za-z0-9_]{12,})/g, "[REDACTED]")
    .replace(/(Bearer\s+)[A-Za-z0-9._-]+/gi, "$1[REDACTED]");
}

function formatShortDate(value: string | null) {
  if (!value) return "";
  return new Date(value).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

const styles = StyleSheet.create({
  container: {
    gap: 10,
  },
  loadingRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  card: {
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: 8,
    backgroundColor: colors.secondary,
    overflow: "hidden",
  },
  cardHeader: {
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    padding: 10,
  },
  cardHeaderText: {
    flex: 1,
  },
  cardTitle: {
    fontSize: 14,
    color: colors.foreground,
    fontWeight: "600",
  },
  cardMeta: {
    marginTop: 2,
    fontSize: 12,
    color: colors.mutedForeground,
  },
  stopButton: {
    borderRadius: 8,
    paddingHorizontal: 10,
    paddingVertical: 6,
    backgroundColor: colors.background,
  },
  stopButtonText: {
    fontSize: 12,
    fontWeight: "600",
    color: colors.destructive,
  },
  timeline: {
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: colors.border,
    padding: 10,
  },
  timelineScroll: {
    maxHeight: 260,
  },
  timelineRow: {
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 8,
    paddingVertical: 3,
  },
  timelineKind: {
    width: 42,
    fontSize: 11,
    color: colors.mutedForeground,
    fontWeight: "600",
  },
  timelineText: {
    flex: 1,
    fontSize: 12,
    lineHeight: 17,
    color: colors.foreground,
  },
  timelineTextCompact: {
    color: colors.mutedForeground,
  },
  mutedText: {
    fontSize: 13,
    color: colors.mutedForeground,
  },
  historyBlock: {
    gap: 6,
  },
  historyHeader: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    paddingVertical: 4,
  },
  historyTitle: {
    fontSize: 13,
    color: colors.mutedForeground,
    fontWeight: "600",
  },
  historyChevron: {
    fontSize: 18,
    color: colors.mutedForeground,
  },
  historyRow: {
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: colors.border,
    paddingTop: 8,
  },
  historyRowHeader: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  historyStatus: {
    fontSize: 12,
    fontWeight: "600",
    color: colors.foreground,
    textTransform: "capitalize",
  },
  historyAgent: {
    flex: 1,
    fontSize: 12,
    color: colors.mutedForeground,
  },
  historyDate: {
    fontSize: 12,
    color: colors.mutedForeground,
  },
  historyTimeline: {
    paddingTop: 8,
  },
});
