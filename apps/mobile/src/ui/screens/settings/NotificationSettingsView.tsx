import { ScrollView, StyleSheet, Switch } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import type { NotificationGroupKey } from "@multica/core/types";
import { Group, GroupNote, GroupRow } from "../../components/Group";
import { PushHeader } from "../../components/Header";
import { Screen } from "../../components/Screen";
import { Failure, Loading } from "../../components/States";
import { useStrings } from "../../strings";

const INBOX_GROUPS: readonly NotificationGroupKey[] = ["assignments", "mentions", "comments", "status_changes", "updates", "new_issues", "agent_activity"];
const SYSTEM_GROUPS: readonly NotificationGroupKey[] = ["system_notifications"];

interface NotificationSettingsViewProps {
  state: "loading" | "error" | "ready";
  /** Groups the viewer has muted. */
  muted: ReadonlySet<NotificationGroupKey>;
  onBack: () => void;
  onRetry: () => void;
  onToggle: (group: NotificationGroupKey, enabled: boolean) => void;
}

export function NotificationSettingsView({ state, muted, onBack, onRetry, onToggle }: NotificationSettingsViewProps) {
  const strings = useStrings();
  const insets = useSafeAreaInsets();
  const rows = (groups: readonly NotificationGroupKey[]) =>
    groups.map((group, index) => {
      const copy = strings.notifications.groups[group];
      return (
        <GroupRow
          key={group}
          label={copy?.label ?? group}
          detail={copy?.detail}
          last={index === groups.length - 1}
          right={<Switch value={!muted.has(group)} onValueChange={(enabled) => onToggle(group, enabled)} accessibilityLabel={copy?.label ?? group} />}
        />
      );
    });

  return (
    <Screen>
      <PushHeader title={strings.notifications.title} onBack={onBack} backLabel={strings.common.back} />
      {state === "loading" ? (
        <Loading />
      ) : state === "error" ? (
        <Failure title={strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={onRetry} />
      ) : (
        <ScrollView contentContainerStyle={[styles.content, { paddingBottom: insets.bottom + 24 }]}>
          <GroupNote above>{strings.notifications.inbox}</GroupNote>
          <Group>{rows(INBOX_GROUPS)}</Group>
          <GroupNote>{strings.notifications.detail}</GroupNote>
          <GroupNote above>{strings.notifications.system}</GroupNote>
          <Group>{rows(SYSTEM_GROUPS)}</Group>
        </ScrollView>
      )}
    </Screen>
  );
}

const styles = StyleSheet.create({
  content: { paddingHorizontal: 16, paddingTop: 0 },
});
