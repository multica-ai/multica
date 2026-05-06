import type { ReactNode } from "react";
import { Pressable, ScrollView, StyleSheet, Text, View } from "react-native";
import type { NativeStackScreenProps } from "@react-navigation/native-stack";
import { Archive, ChevronLeft } from "lucide-react-native";
import {
  formatInboxDetailText,
  formatInboxTimeAgo,
  inboxTypeLabels,
  useInboxList,
  useArchiveInbox,
  useMarkInboxRead,
} from "@multica/core/inbox";
import { useActorName } from "@multica/core/workspace/hooks";
import {
  Button,
  EmptyState,
  LoadingState,
  Screen,
} from "../../components/ui/primitives";
import type { RootStackParamList } from "../../navigation/root-navigator";
import { useMobileWorkspace } from "../../navigation/workspace-context";
import { colors, radii, spacing } from "../../theme/tokens";

type Props = NativeStackScreenProps<RootStackParamList, "InboxDetail">;

export function InboxDetailScreen({ navigation, route }: Props) {
  const { inboxItemId } = route.params;
  const { workspace } = useMobileWorkspace();
  const { getActorName } = useActorName();
  const {
    data: rawItems = [],
    isLoading,
    isError,
  } = useInboxList(workspace.id);
  const item = rawItems.find((candidate) => candidate.id === inboxItemId) ?? null;
  const markRead = useMarkInboxRead();
  const archive = useArchiveInbox();

  if (isLoading) {
    return (
      <InboxDetailFrame navigation={navigation}>
        <LoadingState />
      </InboxDetailFrame>
    );
  }
  if (isError) {
    return (
      <InboxDetailFrame navigation={navigation}>
        <EmptyState title="Unable to load notification" />
      </InboxDetailFrame>
    );
  }
  if (!item) {
    return (
      <InboxDetailFrame navigation={navigation}>
        <EmptyState
          detail="It may have been archived or removed."
          title="Notification not found"
        />
      </InboxDetailFrame>
    );
  }

  const detail = formatInboxDetailText(item, getActorName);

  const archiveAndReturn = async () => {
    await archive.mutateAsync(item.id);
    navigation.goBack();
  };

  const openIssue = () => {
    if (!item.read) markRead.mutate(item.id);
    if (item.issue_id) {
      navigation.navigate("IssueDetail", { issueId: item.issue_id });
    }
  };

  return (
    <InboxDetailFrame navigation={navigation}>
      <ScrollView contentContainerStyle={styles.content}>
        <View style={styles.card}>
          <Text style={styles.type}>{inboxTypeLabels[item.type]}</Text>
          <Text style={styles.title}>{item.title}</Text>
          <Text style={styles.meta}>{formatInboxTimeAgo(item.created_at)}</Text>
          <Text style={styles.detail}>{detail}</Text>
          {item.body && item.body !== detail ? (
            <Text style={styles.body}>{item.body}</Text>
          ) : null}
        </View>
        <View style={styles.actions}>
          {item.issue_id ? (
            <Button onPress={openIssue}>Open issue</Button>
          ) : null}
          <Pressable
            accessibilityRole="button"
            disabled={archive.isPending}
            onPress={() => {
              void archiveAndReturn();
            }}
            style={({ pressed }) => [
              styles.archiveButton,
              archive.isPending && styles.disabled,
              pressed && !archive.isPending && styles.pressed,
            ]}
          >
            <Archive color={colors.foreground} size={16} />
            <Text style={styles.archiveText}>Archive</Text>
          </Pressable>
        </View>
      </ScrollView>
    </InboxDetailFrame>
  );
}

function InboxDetailFrame({
  children,
  navigation,
}: {
  children: ReactNode;
  navigation: Props["navigation"];
}) {
  return (
    <Screen padded={false}>
      <View style={styles.header}>
        <Pressable
          accessibilityLabel="Back"
          accessibilityRole="button"
          onPress={() => navigation.goBack()}
          style={({ pressed }) => [styles.iconButton, pressed && styles.pressed]}
        >
          <ChevronLeft color={colors.foreground} size={22} />
        </Pressable>
        <Text style={styles.headerTitle}>Notification</Text>
        <View style={styles.iconButton} />
      </View>
      {children}
    </Screen>
  );
}

const styles = StyleSheet.create({
  header: {
    alignItems: "center",
    borderBottomColor: colors.border,
    borderBottomWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    minHeight: 52,
    paddingHorizontal: spacing.md,
  },
  iconButton: {
    alignItems: "center",
    borderRadius: radii.md,
    height: 36,
    justifyContent: "center",
    width: 36,
  },
  headerTitle: {
    color: colors.foreground,
    flex: 1,
    fontSize: 17,
    fontWeight: "600",
    textAlign: "center",
  },
  content: {
    gap: spacing.lg,
    padding: spacing.lg,
  },
  card: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    padding: spacing.lg,
  },
  type: {
    color: colors.mutedForeground,
    fontSize: 13,
    fontWeight: "500",
    marginBottom: spacing.sm,
  },
  title: {
    color: colors.foreground,
    fontSize: 20,
    fontWeight: "600",
    lineHeight: 26,
  },
  meta: {
    color: colors.mutedForeground,
    fontSize: 13,
    marginTop: spacing.xs,
  },
  detail: {
    color: colors.foreground,
    fontSize: 15,
    lineHeight: 22,
    marginTop: spacing.lg,
  },
  body: {
    color: colors.mutedForeground,
    fontSize: 14,
    lineHeight: 21,
    marginTop: spacing.md,
  },
  actions: {
    gap: spacing.sm,
  },
  archiveButton: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    flexDirection: "row",
    gap: spacing.xs,
    justifyContent: "center",
    minHeight: 44,
  },
  archiveText: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "500",
  },
  disabled: {
    opacity: 0.45,
  },
  pressed: {
    opacity: 0.7,
  },
});
