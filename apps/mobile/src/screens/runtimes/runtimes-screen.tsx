import { useCallback, useEffect, useRef, useState } from "react";
import {
  ActivityIndicator,
  FlatList,
  Pressable,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { useNavigation } from "@react-navigation/native";
import type { NativeStackNavigationProp } from "@react-navigation/native-stack";
import { api } from "@multica/core/api";
import { useRuntimeList } from "@multica/core/runtimes/hooks";
import { useMemberList } from "@multica/core/workspace/hooks";
import type { AgentRuntime, RuntimePingStatus } from "@multica/core/types";
import {
  Bot,
  CheckCircle2,
  Clock,
  Cloud,
  Monitor,
  Play,
  Server,
  Smartphone,
  UserRound,
  Wifi,
  WifiOff,
  XCircle,
} from "lucide-react-native";
import { EmptyState, LoadingState, Screen } from "../../components/ui/primitives";
import { ScreenTitleBar } from "../../components/ui/screen-title-bar";
import type { RootStackParamList } from "../../navigation/root-navigator";
import { useMobileWorkspace } from "../../navigation/workspace-context";
import { colors, radii, spacing } from "../../theme/tokens";

type RuntimesNavigation = NativeStackNavigationProp<RootStackParamList>;

function formatLastSeen(lastSeenAt: string | null): string {
  if (!lastSeenAt) return "Never";
  const diff = Date.now() - new Date(lastSeenAt).getTime();
  if (diff < 60_000) return "Just now";
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`;
  return `${Math.floor(diff / 86_400_000)}d ago`;
}

function getCliVersion(metadata: Record<string, unknown>): string | null {
  return typeof metadata.cli_version === "string" && metadata.cli_version
    ? metadata.cli_version
    : null;
}

export function RuntimesScreen() {
  const navigation = useNavigation<RuntimesNavigation>();
  const { workspace } = useMobileWorkspace();
  const {
    data: runtimes = [],
    isError,
    isLoading,
    isRefetching,
    refetch,
  } = useRuntimeList(workspace.id, "me");
  const { data: members = [] } = useMemberList(workspace.id);

  const getOwnerName = useCallback(
    (ownerId: string | null) => {
      if (!ownerId) return "Unknown";
      return members.find((member) => member.user_id === ownerId)?.name ?? "Unknown";
    },
    [members],
  );

  if (isLoading) return <LoadingState />;
  if (isError) {
    return <EmptyState detail="Pull to retry once the connection is available." title="Unable to load runtimes" />;
  }

  const onlineCount = runtimes.filter((runtime) => runtime.status === "online").length;

  return (
    <Screen padded={false} safeArea={false}>
      <ScreenTitleBar
        onBack={() => navigation.goBack()}
        right={
          <Text style={styles.subtitle}>
            {onlineCount}/{runtimes.length} online
          </Text>
        }
        title="Runtimes"
      />
      <FlatList
        contentContainerStyle={styles.listContent}
        data={runtimes}
        keyExtractor={(runtime) => runtime.id}
        ListEmptyComponent={
          <EmptyState
            detail="Run multica daemon start to register a local runtime."
            title="No runtimes owned by you"
          />
        }
        onRefresh={refetch}
        refreshing={isRefetching}
        renderItem={({ item }) => (
          <RuntimeCard ownerName={getOwnerName(item.owner_id)} runtime={item} />
        )}
        showsVerticalScrollIndicator={false}
      />
    </Screen>
  );
}

function RuntimeCard({
  runtime,
  ownerName,
}: {
  runtime: AgentRuntime;
  ownerName: string;
}) {
  const cliVersion =
    runtime.runtime_mode === "local" ? getCliVersion(runtime.metadata) ?? "unknown" : "N/A";
  const ModeIcon = runtime.runtime_mode === "cloud" ? Cloud : Monitor;

  return (
    <View style={styles.card}>
      <View style={styles.cardHeader}>
        <View style={styles.runtimeIcon}>
          <Server color={colors.foreground} size={20} />
        </View>
        <View style={styles.cardTitleBlock}>
          <Text numberOfLines={1} style={styles.runtimeName}>
            {runtime.name}
          </Text>
          <Text numberOfLines={1} style={styles.runtimeMeta}>
            {runtime.provider}
          </Text>
        </View>
        <StatusPill status={runtime.status} />
      </View>

      <View style={styles.infoGrid}>
        <InfoCell
          icon={ModeIcon}
          label="Runtime Mode"
          value={runtime.runtime_mode}
        />
        <InfoCell icon={Bot} label="Provider" value={runtime.provider} />
        <InfoCell
          icon={Clock}
          label="Last Seen"
          value={formatLastSeen(runtime.last_seen_at)}
        />
        <InfoCell icon={UserRound} label="Owner" value={ownerName} />
        <InfoCell
          icon={Smartphone}
          label="Device"
          value={runtime.device_info || "Unknown"}
        />
        <InfoCell icon={Server} label="CLI Version" value={cliVersion} />
      </View>

      <ConnectionTest runtimeId={runtime.id} />
    </View>
  );
}

function StatusPill({ status }: { status: AgentRuntime["status"] }) {
  const online = status === "online";
  const Icon = online ? Wifi : WifiOff;
  return (
    <View style={[styles.statusPill, online ? styles.statusOnline : styles.statusOffline]}>
      <Icon color={online ? colors.success : colors.mutedForeground} size={13} />
      <Text style={[styles.statusText, online ? styles.statusTextOnline : styles.statusTextOffline]}>
        {online ? "Online" : "Offline"}
      </Text>
    </View>
  );
}

function InfoCell({
  icon: Icon,
  label,
  value,
}: {
  icon: typeof Server;
  label: string;
  value: string;
}) {
  return (
    <View style={styles.infoCell}>
      <Icon color={colors.mutedForeground} size={16} />
      <View style={styles.infoText}>
        <Text style={styles.infoLabel}>{label}</Text>
        <Text numberOfLines={1} style={styles.infoValue}>
          {value}
        </Text>
      </View>
    </View>
  );
}

function ConnectionTest({ runtimeId }: { runtimeId: string }) {
  const [status, setStatus] = useState<RuntimePingStatus | null>(null);
  const [output, setOutput] = useState("");
  const [error, setError] = useState("");
  const [durationMs, setDurationMs] = useState<number | null>(null);
  const [testing, setTesting] = useState(false);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const cleanup = useCallback(() => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
  }, []);

  useEffect(() => cleanup, [cleanup]);

  const handleTest = async () => {
    cleanup();
    setTesting(true);
    setStatus("pending");
    setOutput("");
    setError("");
    setDurationMs(null);

    try {
      const ping = await api.pingRuntime(runtimeId);
      pollRef.current = setInterval(async () => {
        try {
          const result = await api.getPingResult(runtimeId, ping.id);
          setStatus(result.status);
          if (result.status === "completed") {
            setOutput(result.output ?? "");
            setDurationMs(result.duration_ms ?? null);
            setTesting(false);
            cleanup();
          } else if (result.status === "failed" || result.status === "timeout") {
            setError(result.error ?? "Unknown error");
            setDurationMs(result.duration_ms ?? null);
            setTesting(false);
            cleanup();
          }
        } catch {
          // Keep polling; transient request failures are common during reconnects.
        }
      }, 2000);
    } catch {
      setStatus("failed");
      setError("Failed to initiate test");
      setTesting(false);
    }
  };

  const statusLabel = getPingStatusLabel(status);
  const StatusIcon =
    status === "completed" ? CheckCircle2 : status === "failed" || status === "timeout" ? XCircle : null;

  return (
    <View style={styles.connectionSection}>
      <View style={styles.connectionHeader}>
        <Text style={styles.connectionTitle}>Connection Test</Text>
        <Pressable
          accessibilityRole="button"
          disabled={testing}
          onPress={handleTest}
          style={({ pressed }) => [
            styles.testButton,
            testing && styles.testButtonDisabled,
            pressed && !testing && styles.pressed,
          ]}
        >
          {testing ? (
            <ActivityIndicator color={colors.foreground} size="small" />
          ) : (
            <Play color={colors.foreground} size={14} />
          )}
          <Text style={styles.testButtonText}>{testing ? "Testing" : "Test"}</Text>
        </Pressable>
      </View>

      {statusLabel ? (
        <View style={styles.pingStatusRow}>
          {StatusIcon ? (
            <StatusIcon
              color={status === "completed" ? colors.success : colors.destructive}
              size={15}
            />
          ) : null}
          <Text style={styles.pingStatusText}>
            {statusLabel}
            {durationMs != null ? ` (${(durationMs / 1000).toFixed(1)}s)` : ""}
          </Text>
        </View>
      ) : null}

      {status === "completed" && output ? (
        <Text style={styles.pingOutput}>{output}</Text>
      ) : null}
      {(status === "failed" || status === "timeout") && error ? (
        <Text style={styles.pingError}>{error}</Text>
      ) : null}
    </View>
  );
}

function getPingStatusLabel(status: RuntimePingStatus | null): string {
  switch (status) {
    case "pending":
      return "Waiting for daemon...";
    case "running":
      return "Running test...";
    case "completed":
      return "Connected";
    case "failed":
      return "Failed";
    case "timeout":
      return "Timeout";
    default:
      return "";
  }
}

const styles = StyleSheet.create({
  subtitle: {
    color: colors.mutedForeground,
    fontSize: 13,
  },
  listContent: {
    gap: spacing.md,
    padding: spacing.lg,
    paddingBottom: spacing.xl,
  },
  card: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    padding: spacing.md,
  },
  cardHeader: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.md,
    marginBottom: spacing.md,
  },
  runtimeIcon: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    height: 40,
    justifyContent: "center",
    width: 40,
  },
  cardTitleBlock: {
    flex: 1,
    minWidth: 0,
  },
  runtimeName: {
    color: colors.foreground,
    fontSize: 16,
    fontWeight: "600",
  },
  runtimeMeta: {
    color: colors.mutedForeground,
    fontSize: 13,
    marginTop: 2,
  },
  statusPill: {
    alignItems: "center",
    borderRadius: 999,
    flexDirection: "row",
    gap: spacing.xs,
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.xs,
  },
  statusOnline: {
    backgroundColor: "#eaf7ef",
  },
  statusOffline: {
    backgroundColor: colors.muted,
  },
  statusText: {
    fontSize: 12,
    fontWeight: "600",
  },
  statusTextOnline: {
    color: colors.success,
  },
  statusTextOffline: {
    color: colors.mutedForeground,
  },
  infoGrid: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: spacing.sm,
  },
  infoCell: {
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    flexBasis: "48%",
    flexGrow: 1,
    flexDirection: "row",
    gap: spacing.sm,
    minWidth: 0,
    padding: spacing.sm,
  },
  infoText: {
    flex: 1,
    minWidth: 0,
  },
  infoLabel: {
    color: colors.mutedForeground,
    fontSize: 11,
    marginBottom: 2,
  },
  infoValue: {
    color: colors.foreground,
    fontSize: 13,
    fontWeight: "500",
  },
  connectionSection: {
    borderTopColor: colors.border,
    borderTopWidth: StyleSheet.hairlineWidth,
    gap: spacing.sm,
    marginTop: spacing.md,
    paddingTop: spacing.md,
  },
  connectionHeader: {
    alignItems: "center",
    flexDirection: "row",
    justifyContent: "space-between",
  },
  connectionTitle: {
    color: colors.foreground,
    fontSize: 13,
    fontWeight: "600",
  },
  testButton: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    flexDirection: "row",
    gap: spacing.xs,
    minHeight: 32,
    paddingHorizontal: spacing.md,
  },
  testButtonDisabled: {
    opacity: 0.6,
  },
  testButtonText: {
    color: colors.foreground,
    fontSize: 12,
    fontWeight: "600",
  },
  pressed: {
    opacity: 0.75,
  },
  pingStatusRow: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.xs,
  },
  pingStatusText: {
    color: colors.mutedForeground,
    fontSize: 12,
  },
  pingOutput: {
    backgroundColor: "#eaf7ef",
    borderRadius: radii.md,
    color: colors.foreground,
    fontSize: 12,
    padding: spacing.sm,
  },
  pingError: {
    backgroundColor: "#fff0ed",
    borderRadius: radii.md,
    color: colors.destructive,
    fontSize: 12,
    padding: spacing.sm,
  },
});
