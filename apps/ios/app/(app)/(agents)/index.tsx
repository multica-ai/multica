import { useCallback, useMemo, useState } from "react";
import {
  FlatList,
  RefreshControl,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import type { Agent, AgentStatus } from "@multica/core/types";
import { Avatar } from "@/components/Avatar";
import { EmptyState } from "@/components/EmptyState";
import { MetricTile } from "@/components/MetricTile";
import { ScreenHeader } from "@/components/ScreenHeader";
import { TabBar } from "@/components/TabBar";
import { colors } from "@/lib/theme";

type AgentTab = "active" | "archived";

const TABS: { key: AgentTab; label: string }[] = [
  { key: "active", label: "Active" },
  { key: "archived", label: "Archived" },
];

const STATUS_LABELS: Record<AgentStatus, string> = {
  idle: "Idle",
  working: "Working",
  blocked: "Blocked",
  error: "Error",
  offline: "Offline",
};

export default function AgentsScreen() {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const [activeTab, setActiveTab] = useState<AgentTab>("active");
  const { data: agents = [], isLoading } = useQuery(agentListOptions(wsId));

  const visibleAgents = useMemo(
    () =>
      agents.filter((agent) =>
        activeTab === "archived" ? agent.archived_at : !agent.archived_at,
      ),
    [activeTab, agents],
  );
  const workingCount = agents.filter((agent) => agent.status === "working").length;
  const blockedCount = agents.filter((agent) => agent.status === "blocked" || agent.status === "error").length;
  const onlineCount = agents.filter((agent) => agent.status !== "offline" && !agent.archived_at).length;

  const handleRefresh = useCallback(() => {
    qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
  }, [qc, wsId]);

  return (
    <View style={styles.container}>
      <ScreenHeader
        title="Agents"
        subtitle="People, runtimes, and autonomous work"
        backLabel="More"
        fallback="/(app)/(more)"
      />
      <View style={styles.metrics}>
        <MetricTile label="Online" value={onlineCount} tone="blue" />
        <MetricTile label="Working" value={workingCount} tone="green" />
        <MetricTile label="Needs attention" value={blockedCount} tone={blockedCount > 0 ? "red" : "default"} />
      </View>
      <TabBar tabs={TABS} activeTab={activeTab} onTabChange={setActiveTab} />
      <FlatList
        data={visibleAgents}
        keyExtractor={(agent) => agent.id}
        renderItem={({ item }) => <AgentRow agent={item} />}
        ItemSeparatorComponent={Separator}
        refreshControl={
          <RefreshControl refreshing={isLoading} onRefresh={handleRefresh} />
        }
        contentContainerStyle={
          visibleAgents.length === 0 ? styles.empty : undefined
        }
        ListEmptyComponent={
          <EmptyState
            title={isLoading ? "Loading..." : "No agents"}
            body={
              isLoading
                ? undefined
                : activeTab === "active"
                  ? "Create or restore an agent to start automated work."
                  : "Archived agents will appear here."
            }
          />
        }
      />
    </View>
  );
}

function AgentRow({ agent }: { agent: Agent }) {
  return (
    <TouchableOpacity style={styles.row} activeOpacity={0.7}>
      <Avatar
        name={agent.name}
        avatarUrl={agent.avatar_url}
        size={36}
        isAgent
      />
      <View style={styles.rowContent}>
        <View style={styles.titleRow}>
          <Text style={styles.agentName} numberOfLines={1}>
            {agent.name}
          </Text>
          <StatusPill status={agent.status} />
        </View>
        <Text style={styles.description} numberOfLines={2}>
          {agent.description || "No description"}
        </Text>
        <Text style={styles.meta} numberOfLines={1}>
          {agent.runtime_mode} runtime | {agent.visibility} visibility
        </Text>
      </View>
    </TouchableOpacity>
  );
}

function StatusPill({ status }: { status: AgentStatus }) {
  return (
    <View style={styles.statusPill}>
      <View style={[styles.statusDot, { backgroundColor: statusColor(status) }]} />
      <Text style={styles.statusText}>{STATUS_LABELS[status]}</Text>
    </View>
  );
}

function statusColor(status: AgentStatus) {
  switch (status) {
    case "working":
      return colors.success;
    case "blocked":
      return colors.warning;
    case "error":
      return colors.destructive;
    case "idle":
      return colors.info;
    case "offline":
      return colors.mutedForeground;
  }
}

function Separator() {
  return <View style={styles.separator} />;
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: colors.background,
  },
  metrics: {
    flexDirection: "row",
    gap: 8,
    paddingHorizontal: 16,
    paddingVertical: 12,
  },
  row: {
    flexDirection: "row",
    gap: 12,
    paddingHorizontal: 16,
    paddingVertical: 14,
    backgroundColor: colors.background,
  },
  rowContent: {
    flex: 1,
    gap: 4,
  },
  titleRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  agentName: {
    flex: 1,
    fontSize: 16,
    fontWeight: "600",
    color: colors.foreground,
  },
  description: {
    fontSize: 13,
    color: colors.foreground,
    lineHeight: 18,
  },
  meta: {
    fontSize: 12,
    color: colors.mutedForeground,
  },
  statusPill: {
    flexDirection: "row",
    alignItems: "center",
    gap: 5,
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 8,
    backgroundColor: colors.secondary,
  },
  statusDot: {
    width: 7,
    height: 7,
    borderRadius: 4,
  },
  statusText: {
    fontSize: 12,
    color: colors.foreground,
    fontWeight: "500",
  },
  separator: {
    height: StyleSheet.hairlineWidth,
    backgroundColor: colors.border,
    marginLeft: 64,
  },
  empty: {
    flexGrow: 1,
  },
});
