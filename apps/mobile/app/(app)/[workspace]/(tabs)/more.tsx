/**
 * More tab — a real page (not a popover). Mirrors what the dropdown it
 * replaced showed: the user's identity (→ Settings), the current
 * workspace (→ switch-workspace), and shortcuts to Pinned/Issues/Projects.
 *
 * SectionGroup/NavRow are the same shared components more/settings.tsx
 * uses — this is the pattern's second call site, not a new one.
 */
import { View } from "react-native";
import { router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Text } from "@/components/ui/text";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Header } from "@/components/ui/header";
import { NavRow, SectionGroup } from "@/components/ui/section-group";
import { WorkspaceAvatar } from "@/components/workspace/workspace-avatar";
import { workspaceListOptions } from "@/data/queries/workspaces";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

function initialsOf(name: string | undefined): string {
  if (!name) return "?";
  return name
    .split(" ")
    .map((w) => w[0])
    .filter(Boolean)
    .slice(0, 2)
    .join("")
    .toUpperCase();
}

export default function MorePage() {
  const { t } = useTranslation("workspace");
  const { t: tCommon } = useTranslation("common");
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;

  const slug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const user = useAuthStore((s) => s.user);
  const { data: workspaces } = useQuery(workspaceListOptions());
  const currentWorkspace = workspaces?.find((w) => w.slug === slug);
  const canSwitchWorkspace = (workspaces?.length ?? 0) > 1;
  const workspaceFallback = t("more_page.workspace_fallback");

  const goSettings = () => slug && router.push(`/${slug}/more/settings`);
  const goSwitchWorkspace = () =>
    slug && router.push(`/${slug}/switch-workspace`);

  return (
    <View className="flex-1 bg-background">
      <Header title={tCommon("tabs.more")} />
      <View className="flex-1 px-4 py-4 gap-6">
        <SectionGroup>
          <NavRow
            onPress={goSettings}
            chevronColor={mutedFg}
            leading={
              <Avatar
                alt={user?.name ?? t("more_page.account_settings_a11y")}
                className="size-10"
              >
                {user?.avatar_url ? (
                  <AvatarImage source={{ uri: user.avatar_url }} />
                ) : null}
                <AvatarFallback>
                  <Text className="text-sm font-semibold text-muted-foreground">
                    {initialsOf(user?.name)}
                  </Text>
                </AvatarFallback>
              </Avatar>
            }
            title={user?.name ?? "—"}
            subtitle={user?.email}
          />
          {canSwitchWorkspace ? (
            <NavRow
              onPress={goSwitchWorkspace}
              chevronColor={mutedFg}
              leading={
                <WorkspaceAvatar
                  name={currentWorkspace?.name ?? workspaceFallback}
                  avatarUrl={currentWorkspace?.avatar_url}
                  size={32}
                />
              }
              title={currentWorkspace?.name ?? workspaceFallback}
            />
          ) : (
            <View className="flex-row items-center px-4 py-3.5 gap-3">
              <WorkspaceAvatar
                name={currentWorkspace?.name ?? workspaceFallback}
                avatarUrl={currentWorkspace?.avatar_url}
                size={32}
              />
              <View className="flex-1">
                <Text className="text-base font-medium text-foreground">
                  {currentWorkspace?.name ?? workspaceFallback}
                </Text>
              </View>
            </View>
          )}
        </SectionGroup>

        <SectionGroup title={t("more_page.section_title")}>
          <NavRow
            onPress={() => slug && router.push(`/${slug}/more/pins`)}
            chevronColor={mutedFg}
            title={t("more_page.nav.pinned")}
          />
          <NavRow
            onPress={() => slug && router.push(`/${slug}/more/issues`)}
            chevronColor={mutedFg}
            title={t("more_page.nav.issues")}
          />
          <NavRow
            onPress={() => slug && router.push(`/${slug}/more/projects`)}
            chevronColor={mutedFg}
            title={t("more_page.nav.projects")}
          />
        </SectionGroup>
      </View>
    </View>
  );
}
