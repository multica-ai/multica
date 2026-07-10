/**
 * More tab — a real pushed screen, replacing the old dropdown-popover hack
 * (which only existed to work around the JS tab bar). Now that the tab bar is
 * native, "More" is a normal tab destination: an iOS-style list with the
 * account (→ settings) and the secondary nav (Pinned / Issues / Projects).
 *
 * Workspace context lives in the header (WorkspaceSwitcherButton, from the
 * workspace-switcher branch this is stacked on), so it's deliberately not
 * duplicated here.
 */
import { type ReactNode } from "react";
import { Image, Pressable, ScrollView, View } from "react-native";
import { Image as ExpoImage } from "expo-image";
import { router } from "expo-router";
import { Text } from "@/components/ui/text";
import { Header } from "@/components/ui/header";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

const NAV_ITEMS = [
  { label: "Pinned", icon: "pin", path: "/more/pins" },
  { label: "Issues", icon: "list.bullet", path: "/more/issues" },
  { label: "Projects", icon: "square.stack", path: "/more/projects" },
] as const;

export default function MoreTab() {
  const slug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const user = useAuthStore((s) => s.user);
  const { colorScheme } = useColorScheme();
  const t = THEME[colorScheme];

  const go = (path: string) => {
    if (slug) router.push(`/${slug}${path}`);
  };
  const initial = (user?.name ?? user?.email ?? "U").charAt(0).toUpperCase();

  return (
    <View className="flex-1 bg-background">
      <Header title="More" />
      <ScrollView contentContainerClassName="py-3">
        {/* Account → settings */}
        <Row onPress={() => go("/more/settings")} chevronTint={t.mutedForeground}>
          {user?.avatar_url ? (
            <Image
              source={{ uri: user.avatar_url }}
              className="size-9 rounded-full bg-muted"
            />
          ) : (
            <View className="size-9 rounded-full bg-muted items-center justify-center">
              <Text className="text-sm font-medium text-muted-foreground">
                {initial}
              </Text>
            </View>
          )}
          <View className="flex-1 min-w-0">
            <Text
              className="text-base font-medium text-foreground"
              numberOfLines={1}
            >
              {user?.name ?? "—"}
            </Text>
            {user?.email ? (
              <Text className="text-xs text-muted-foreground" numberOfLines={1}>
                {user.email}
              </Text>
            ) : null}
          </View>
        </Row>

        <View className="h-4" />

        {NAV_ITEMS.map((item) => (
          <Row
            key={item.path}
            onPress={() => go(item.path)}
            chevronTint={t.mutedForeground}
          >
            <ExpoImage
              source={`sf:${item.icon}`}
              tintColor={t.foreground}
              style={{ width: 22, height: 22 }}
            />
            <Text className="flex-1 text-base text-foreground">{item.label}</Text>
          </Row>
        ))}
      </ScrollView>
    </View>
  );
}

/** iOS list row: leading content + trailing disclosure chevron when tappable. */
function Row({
  children,
  onPress,
  chevronTint,
}: {
  children: ReactNode;
  onPress?: () => void;
  chevronTint: string;
}) {
  return (
    <Pressable
      onPress={onPress}
      disabled={!onPress}
      className="flex-row items-center gap-3 px-4 h-14 border-b border-border active:bg-secondary"
    >
      {children}
      {onPress ? (
        <ExpoImage
          source="sf:chevron.right"
          tintColor={chevronTint}
          style={{ width: 12, height: 12 }}
        />
      ) : null}
    </Pressable>
  );
}
