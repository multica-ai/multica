/**
 * HarmonyOS port of apps/mobile/app/(app)/[workspace]/more/settings/
 * notifications.tsx — notification preferences subscreen. 6 inbox groups +
 * system_notifications toggle, each backed by an optimistic PATCH
 * /api/notification-preferences (data/mutations/notification-preferences).
 *
 * Copy mirrors packages/views/settings/components/notifications-tab.tsx but
 * hardcoded English (mobile has no i18n infra yet). The group labels MUST
 * stay in sync with web — they describe the same server-side semantics,
 * and divergent labels would violate behavioral parity.
 */
import React from "react";
import { ActivityIndicator, ScrollView, StyleSheet, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import type {
  NotificationGroupKey,
  NotificationPreferences,
} from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Switch } from "@/components/ui/switch";
import { Separator } from "@/components/ui/separator";
import { IconButton } from "@/components/ui/icon-button";
import { SafeAreaView } from "@/lib/safe-area";
import { useWorkspaceStore } from "@/data/workspace-store";
import { notificationPreferenceOptions } from "@/data/queries/notification-preferences";
import { useUpdateNotificationPreferences } from "@/data/mutations/notification-preferences";
import type { ThemeColors } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

const INBOX_GROUPS: {
  key: Exclude<NotificationGroupKey, "system_notifications">;
  label: string;
  description?: string;
}[] = [
  {
    key: "assignments",
    label: "Assignments",
    description: "Assigned or unassigned.",
  },
  {
    key: "status_changes",
    label: "Status changes",
  },
  {
    key: "comments",
    label: "Comments",
    description: "New comments on issues you're subscribed to.",
  },
  {
    key: "mentions",
    label: "Mentions",
    description: "When someone @mentions you, including @all and @squad.",
  },
  {
    key: "updates",
    label: "Issue updates",
    description: "Edits to title, description, labels, priority, or due date.",
  },
  {
    key: "agent_activity",
    label: "Agent activity",
    description: "When an agent run fails.",
  },
];

export function SettingsNotificationsScreen({ onBack }: { onBack?: () => void }) {
  const c = useThemeColors();
  const s = styles(c);
  const wsId = useWorkspaceStore((state) => state.currentWorkspaceId);
  const { data, isLoading, error } = useQuery(
    notificationPreferenceOptions(wsId),
  );
  const mutation = useUpdateNotificationPreferences();

  const preferences: NotificationPreferences = data?.preferences ?? {};

  const onToggle = (key: NotificationGroupKey, enabled: boolean) => {
    const next: NotificationPreferences = { ...preferences };
    if (enabled) {
      // Default is "all" — omitting the key keeps the object clean.
      delete next[key];
    } else {
      next[key] = "muted";
    }
    mutation.mutate(next);
  };

  const systemEnabled = preferences.system_notifications !== "muted";

  if (isLoading) {
    return (
      <SafeAreaView edges={["top"]} style={s.screen}>
        {onBack ? <TitleRow onBack={onBack} s={s} c={c} /> : null}
        <View style={s.centerWrap}>
          <ActivityIndicator color={c.mutedForeground} />
        </View>
      </SafeAreaView>
    );
  }

  if (error) {
    return (
      <SafeAreaView edges={["top"]} style={s.screen}>
        {onBack ? <TitleRow onBack={onBack} s={s} c={c} /> : null}
        <View style={s.centerWrap}>
          <Text style={[s.errorText, { color: c.destructive }]}>
            Failed to load notification preferences.
          </Text>
        </View>
      </SafeAreaView>
    );
  }

  return (
    <SafeAreaView edges={["top"]} style={s.screen}>
      {onBack ? <TitleRow onBack={onBack} s={s} c={c} /> : null}
      <ScrollView
        style={s.flex}
        contentContainerStyle={s.content}
        showsVerticalScrollIndicator={false}
      >
        <Section title="Inbox notifications" c={c} s={s}>
          {INBOX_GROUPS.map((group, idx) => {
            const enabled = preferences[group.key] !== "muted";
            const isLast = idx === INBOX_GROUPS.length - 1;
            return (
              <View key={group.key}>
                <View style={s.toggleRow}>
                  <View style={s.toggleTextWrap}>
                    <Text style={s.toggleLabel}>{group.label}</Text>
                    {group.description ? (
                      <Text style={[s.toggleDescription, { color: c.mutedForeground }]}>
                        {group.description}
                      </Text>
                    ) : null}
                  </View>
                  <Switch
                    checked={enabled}
                    onCheckedChange={(checked) => onToggle(group.key, checked)}
                  />
                </View>
                {!isLast ? <Separator /> : null}
              </View>
            );
          })}
        </Section>

        <Section title="System" c={c} s={s}>
          <View style={s.toggleRow}>
            <View style={s.toggleTextWrap}>
              <Text style={s.toggleLabel}>System notifications</Text>
              <Text style={[s.toggleDescription, { color: c.mutedForeground }]}>
                Account changes, security alerts, product updates.
              </Text>
            </View>
            <Switch
              checked={systemEnabled}
              onCheckedChange={(checked) =>
                onToggle("system_notifications", checked)
              }
            />
          </View>
        </Section>
      </ScrollView>
    </SafeAreaView>
  );
}

function TitleRow({
  onBack,
  s,
  c,
}: {
  onBack: () => void;
  s: ReturnType<typeof styles>;
  c: ThemeColors;
}) {
  return (
    <View style={s.titleRow}>
      <IconButton name="chevron-back" onPress={onBack} accessibilityLabel="Back" />
      <Text style={s.title}>Notifications</Text>
      <View style={s.titleSpacer} />
    </View>
  );
}

function Section({
  title,
  children,
  c,
  s,
}: {
  title: string;
  children: React.ReactNode;
  c: ThemeColors;
  s: ReturnType<typeof styles>;
}) {
  return (
    <View style={s.section}>
      <View style={s.sectionHeaderWrap}>
        <Text style={[s.sectionTitle, { color: c.mutedForeground }]}>
          {title}
        </Text>
      </View>
      <View style={[s.card, { borderColor: c.border, backgroundColor: c.card }]}>
        {children}
      </View>
    </View>
  );
}

const styles = (c: ThemeColors) =>
  StyleSheet.create({
    flex: { flex: 1 },
    screen: { flex: 1, backgroundColor: c.background },
    content: { paddingHorizontal: 16, paddingVertical: 16, gap: 24 },
    centerWrap: {
      flex: 1,
      alignItems: "center",
      justifyContent: "center",
      paddingHorizontal: 24,
    },
    errorText: { fontSize: 14, textAlign: "center" },
    titleRow: {
      flexDirection: "row",
      alignItems: "center",
      height: 48,
      paddingLeft: 8,
    },
    title: { fontSize: 18, fontWeight: "600", color: c.foreground },
    // Symmetric spacer so the title sits where the icon row centers it.
    titleSpacer: { width: 40 },
    section: { gap: 8 },
    sectionHeaderWrap: { paddingHorizontal: 4 },
    sectionTitle: {
      fontSize: 12,
      textTransform: "uppercase",
      letterSpacing: 0.8,
    },
    card: {
      borderRadius: 6,
      borderWidth: 1,
      overflow: "hidden",
    },
    toggleRow: {
      flexDirection: "row",
      alignItems: "center",
      paddingHorizontal: 16,
      paddingVertical: 12,
      gap: 12,
    },
    toggleTextWrap: { flex: 1 },
    toggleLabel: { fontSize: 16, fontWeight: "500", color: c.foreground },
    toggleDescription: { fontSize: 12, marginTop: 2 },
  });
