import { Pressable, View } from "react-native";
import { Image as ExpoImage } from "expo-image";
import { router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Text } from "@/components/ui/text";
import { WorkspaceAvatar } from "@/components/workspace/workspace-avatar";
import { workspaceListOptions } from "@/data/queries/workspaces";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import { cn } from "@/lib/utils";

type TabletTabRoute = {
  key: string;
  name: string;
};

export interface TabletTabBarProps {
  state: {
    index: number;
    routes: TabletTabRoute[];
  };
  navigation: {
    emit: (event: {
      type: "tabPress" | "tabLongPress";
      target: string;
      canPreventDefault?: boolean;
    }) => unknown;
    navigate: (name: string) => void;
  };
  inboxBadge?: string;
}

const NAV_ITEMS = [
  { route: "inbox", label: "Inbox", icon: "tray", activeIcon: "tray.fill" },
  { route: "issues", label: "Issues", icon: "checklist", activeIcon: "checklist" },
  { route: "projects", label: "Projects", icon: "folder", activeIcon: "folder.fill" },
  { route: "agents", label: "Agents", icon: "cpu", activeIcon: "cpu" },
  { route: "settings", label: "Settings", icon: "gearshape", activeIcon: "gearshape.fill" },
] as const;

/**
 * iPad navigation rail. The phone keeps React Navigation's standard bottom
 * bar; only the iPad swaps in this persistent rail so the selected issue can
 * remain visible beside the workspace list, matching the chosen design.
 */
export function TabletTabBar({
  state,
  navigation,
  inboxBadge,
}: TabletTabBarProps) {
  const insets = useSafeAreaInsets();
  const slug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const user = useAuthStore((s) => s.user);
  const { data: workspaces } = useQuery(workspaceListOptions());
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];

  const workspace = workspaces?.find((candidate) => candidate.slug === slug);
  const activeRoute = state.routes[state.index]?.name;

  const openWorkspaceSwitcher = () => {
    if (slug) router.push(`/${slug}/switch-workspace`);
  };

  return (
    <View
      className="w-40 border-r border-border bg-background px-3"
      style={{ paddingTop: insets.top + 12, paddingBottom: insets.bottom + 12 }}
    >
      <Pressable
        onPress={openWorkspaceSwitcher}
        className="h-11 flex-row items-center gap-3 rounded-md px-2 active:bg-accent"
        accessibilityRole="button"
        accessibilityLabel="Switch workspace"
      >
        <WorkspaceAvatar
          name={workspace?.name ?? slug ?? "Multica"}
          avatarUrl={workspace?.avatar_url}
          size={28}
        />
        <Text className="min-w-0 flex-1 text-sm font-semibold" numberOfLines={1}>
          {workspace?.name ?? slug ?? "Multica"}
        </Text>
        <ExpoImage
          source="sf:chevron.down"
          tintColor={theme.mutedForeground}
          style={{ width: 12, height: 12 }}
        />
      </Pressable>

      <View className="mt-7 gap-1">
        {NAV_ITEMS.map((item) => {
          const route = state.routes.find((candidate) => candidate.name === item.route);
          if (!route) return null;
          const active = activeRoute === item.route;

          return (
            <Pressable
              key={item.route}
              onPress={() => {
                const event = navigation.emit({
                  type: "tabPress",
                  target: route.key,
                  canPreventDefault: true,
                });
                const defaultPrevented =
                  typeof event === "object" &&
                  event !== null &&
                  "defaultPrevented" in event &&
                  event.defaultPrevented === true;
                if (!active && !defaultPrevented) {
                  navigation.navigate(item.route);
                }
              }}
              onLongPress={() =>
                navigation.emit({ type: "tabLongPress", target: route.key })
              }
              className={cn(
                "h-11 flex-row items-center gap-3 rounded-md px-3",
                active ? "bg-accent" : "active:bg-accent/70",
              )}
              accessibilityRole="tab"
              accessibilityState={{ selected: active }}
            >
              <ExpoImage
                source={`sf:${active ? item.activeIcon : item.icon}`}
                tintColor={active ? theme.brand : theme.mutedForeground}
                style={{ width: 19, height: 19 }}
              />
              <Text
                className={cn(
                  "flex-1 text-sm",
                  active ? "font-semibold text-brand" : "text-muted-foreground",
                )}
              >
                {item.label}
              </Text>
              {item.route === "inbox" && inboxBadge ? (
                <View className="min-w-5 h-5 items-center justify-center rounded-full bg-brand px-1">
                  <Text className="text-[10px] font-semibold text-brand-foreground">
                    {inboxBadge}
                  </Text>
                </View>
              ) : null}
            </Pressable>
          );
        })}
      </View>

      <View className="mt-auto border-t border-border pt-3">
        <Pressable
          onPress={() => navigation.navigate("settings")}
          className="h-11 flex-row items-center gap-3 rounded-md px-2 active:bg-accent"
          accessibilityRole="button"
          accessibilityLabel="Account settings"
        >
          <View className="size-7 items-center justify-center rounded-full bg-primary">
            <Text className="text-xs font-semibold text-primary-foreground">
              {(user?.name ?? user?.email ?? "M").charAt(0).toUpperCase()}
            </Text>
          </View>
          <Text className="min-w-0 flex-1 text-xs font-medium" numberOfLines={1}>
            {user?.name ?? "Multica"}
          </Text>
          <ExpoImage
            source="sf:chevron.right"
            tintColor={theme.mutedForeground}
            style={{ width: 11, height: 11 }}
          />
        </Pressable>
      </View>
    </View>
  );
}
