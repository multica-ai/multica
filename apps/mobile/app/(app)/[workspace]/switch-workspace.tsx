/**
 * Workspace switcher — presented as a formSheet by the parent Stack.
 *
 * Reached from the More popover's WorkspaceCard (collapsed single-row entry).
 * Lists every workspace the user belongs to, current one disabled with a
 * checkmark. Tapping a non-current row triggers an iOS-native `Alert.alert`
 * confirm — only after the user confirms do we dismiss the sheet and
 * `router.replace` to the target slug.
 *
 * Why a confirm step:
 *   The previous flow ("popover → tap row → instant switch") had no friction
 *   against fat-finger taps in the cramped popover, and the user lost their
 *   entire navigation context (tabs, scroll position) with one accidental
 *   tap. iOS Alert is the platform-correct gate (mobile/CLAUDE.md Principle
 *   3 — iOS native > RNR > discuss).
 *
 * Switching itself stays minimal: `router.dismiss()` to close this sheet,
 * then `router.replace(/${slug}/inbox)`. The downstream WorkspaceRouteLayout
 * handles `setCurrentWorkspace(slug, uuid)` on mount.
 */
import {
  ActivityIndicator,
  Alert,
  Pressable,
  ScrollView,
  View,
} from "react-native";
import { Image as ExpoImage } from "expo-image";
import { router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { Workspace } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { WorkspaceAvatar } from "@/components/workspace/workspace-avatar";
import { workspaceListOptions } from "@/data/queries/workspaces";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import { cn } from "@/lib/utils";

export default function SwitchWorkspaceRoute() {
  const activeSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  const { data, isLoading } = useQuery(workspaceListOptions());
  const { t } = useTranslation("auth");
  const { t: tCommon } = useTranslation("common");

  const onSelect = (ws: Workspace) => {
    if (ws.slug === activeSlug) return;
    Alert.alert(
      t("switch_workspace.confirm_title"),
      t("switch_workspace.confirm_message", { name: ws.name }),
      [
        { text: tCommon("cancel"), style: "cancel" },
        {
          text: t("switch_workspace.switch_button"),
          onPress: () => {
            router.dismiss();
            router.replace(`/${ws.slug}/inbox`);
          },
        },
      ],
    );
  };

  return (
    <View className="flex-1">
      <View className="px-4 pt-4 pb-3">
        <Text className="text-base font-semibold text-foreground">
          {t("switch_workspace.title")}
        </Text>
      </View>
      {isLoading ? (
        <View className="py-6 items-center">
          <ActivityIndicator />
        </View>
      ) : (
        <ScrollView className="flex-1" showsVerticalScrollIndicator={false}>
          {(data ?? []).map((ws) => (
            <WorkspaceRow
              key={ws.id}
              workspace={ws}
              active={ws.slug === activeSlug}
              onPress={() => onSelect(ws)}
              iconTint={theme.foreground}
              t={t}
            />
          ))}
        </ScrollView>
      )}
    </View>
  );
}

function WorkspaceRow({
  workspace,
  active,
  onPress,
  iconTint,
  t,
}: {
  workspace: Workspace;
  active: boolean;
  onPress: () => void;
  iconTint: string;
  t: (key: string, options?: Record<string, unknown>) => string;
}) {
  return (
    <Pressable
      onPress={onPress}
      disabled={active}
      accessibilityLabel={
        active
          ? t("switch_workspace.current_workspace_a11y", { name: workspace.name })
          : t("switch_workspace.switch_to_a11y", { name: workspace.name })
      }
      className={cn(
        "flex-row items-center gap-3 px-4 py-3 active:bg-secondary",
        active && "opacity-100",
      )}
    >
      <WorkspaceAvatar
        name={workspace.name}
        avatarUrl={workspace.avatar_url}
        size={24}
      />
      <Text
        className={cn(
          "flex-1 text-sm text-foreground",
          active && "font-semibold",
        )}
        numberOfLines={1}
      >
        {workspace.name}
      </Text>
      {active ? (
        <ExpoImage
          source="sf:checkmark"
          tintColor={iconTint}
          style={{ width: 16, height: 16 }}
        />
      ) : null}
    </Pressable>
  );
}
