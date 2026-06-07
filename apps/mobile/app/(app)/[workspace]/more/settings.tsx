/**
 * Settings page — account info, workspace switching, appearance, profile and
 * notifications subscreens, and sign out.
 *
 * Inherits the responsibilities the old More tab carried (account row,
 * workspace list, sign-out button) now that the More tab is gone and global
 * navigation lives in GlobalNavMenu.
 *
 * Subscreens push under more/settings/:
 *   - more/settings/profile        — edit name + avatar
 *   - more/settings/notifications  — per-group inbox + system toggles
 *
 * Theme picker stays inline (3 fixed options, fits in one section).
 */
import { Alert, ActivityIndicator, Pressable, ScrollView, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import type { Workspace } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { workspaceListOptions } from "@/data/queries/workspaces";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  useColorScheme,
  type ThemePreference,
} from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import { cn } from "@/lib/utils";

const THEME_OPTIONS: Array<{ value: ThemePreference; label: string }> = [
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
  { value: "system", label: "System" },
];

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

export default function SettingsPage() {
  const user = useAuthStore((s) => s.user);
  const logout = useAuthStore((s) => s.logout);
  const currentSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const setCurrentWorkspace = useWorkspaceStore((s) => s.setCurrentWorkspace);
  const clearWorkspace = useWorkspaceStore((s) => s.clear);
  const { data, isLoading, error } = useQuery(workspaceListOptions());
  const { preference, setPreference, colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;

  const onSwitch = async (ws: Workspace) => {
    if (ws.slug === currentSlug) return;
    await setCurrentWorkspace(ws.id, ws.slug);
    router.replace(`/${ws.slug}/inbox`);
  };

  const onSignOut = () => {
    Alert.alert(
      "Sign out",
      "You'll need to sign in again to use Multica on this device.",
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Sign out",
          style: "destructive",
          onPress: async () => {
            await clearWorkspace();
            await logout();
          },
        },
      ],
    );
  };

  const goProfile = () => router.push(`/${currentSlug}/more/settings/profile`);
  const goNotifications = () =>
    router.push(`/${currentSlug}/more/settings/notifications`);

  return (
    <ScrollView
      className="flex-1 bg-background"
      contentContainerClassName="px-4 py-4 gap-6"
    >
      <SectionGroup title="Account">
        <NavRow
          onPress={goProfile}
          chevronColor={mutedFg}
          leading={
            <Avatar alt={user?.name ?? "User avatar"} className="size-10">
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
        <Separator />
        <NavRow
          onPress={goNotifications}
          chevronColor={mutedFg}
          title="Notifications"
          subtitle="Inbox and system alerts"
        />
      </SectionGroup>

      <SectionGroup title="Workspaces">
        {isLoading ? (
          <View className="py-4 items-center">
            <ActivityIndicator />
          </View>
        ) : error ? (
          <View className="p-4">
            <Text className="text-sm text-destructive">
              Failed to load workspaces
            </Text>
          </View>
        ) : (
          data?.map((ws, idx) => {
            const isActive = ws.slug === currentSlug;
            const isLast = idx === (data?.length ?? 0) - 1;
            return (
              <View key={ws.id}>
                <WorkspaceRow
                  name={ws.name}
                  slug={ws.slug}
                  isActive={isActive}
                  iconColor={mutedFg}
                  onPress={() => onSwitch(ws)}
                />
                {!isLast ? <Separator /> : null}
              </View>
            );
          })
        )}
      </SectionGroup>

      <SectionGroup title="Appearance">
        {/* Two converging entry points by design, NOT a double-fire:
              - Tap on small radio circle  → RadioGroupItem (Pressable, inner) consumes → onValueChange fires
              - Tap on text / row padding  → outer Pressable.onPress fires
            RN's responder system gives inner Pressable priority, so each tap
            triggers exactly one setPreference. Both paths land at the same
            handler intentionally — the Pressable wrapper exists only to
            extend the tap target to the full row (iOS standard). */}
        <RadioGroup
          value={preference}
          onValueChange={(v) => setPreference(v as ThemePreference)}
          className="gap-0"
        >
          {THEME_OPTIONS.map((opt, idx) => {
            const isLast = idx === THEME_OPTIONS.length - 1;
            return (
              <View key={opt.value}>
                <Pressable
                  onPress={() => setPreference(opt.value)}
                  className="flex-row items-center px-4 py-3.5 active:bg-secondary gap-3"
                >
                  <RadioGroupItem value={opt.value} />
                  <Text className="flex-1 text-base font-medium text-foreground">
                    {opt.label}
                  </Text>
                </Pressable>
                {!isLast ? <Separator /> : null}
              </View>
            );
          })}
        </RadioGroup>
      </SectionGroup>

      <View className="pt-2">
        <Button variant="destructive" onPress={onSignOut}>
          <Text>Sign out</Text>
        </Button>
      </View>
    </ScrollView>
  );
}

function NavRow({
  onPress,
  leading,
  title,
  subtitle,
  chevronColor,
}: {
  onPress: () => void;
  leading?: React.ReactNode;
  title: string;
  subtitle?: string;
  chevronColor: string;
}) {
  return (
    <Pressable
      onPress={onPress}
      className={cn(
        "flex-row items-center px-4 py-3.5 active:bg-secondary gap-3",
      )}
    >
      {leading}
      <View className="flex-1">
        <Text className="text-base font-medium text-foreground">{title}</Text>
        {subtitle ? (
          <Text className="text-sm text-muted-foreground mt-0.5">
            {subtitle}
          </Text>
        ) : null}
      </View>
      <Ionicons name="chevron-forward" size={18} color={chevronColor} />
    </Pressable>
  );
}

function SectionGroup({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <View className="gap-2">
      <Text className="text-xs uppercase tracking-wider text-muted-foreground px-1">
        {title}
      </Text>
      <View className="rounded-md border border-border bg-card overflow-hidden">
        {children}
      </View>
    </View>
  );
}

function WorkspaceRow({
  name,
  slug,
  isActive,
  iconColor,
  onPress,
}: {
  name: string;
  slug: string;
  isActive: boolean;
  iconColor: string;
  onPress: () => void;
}) {
  return (
    <Pressable
      onPress={onPress}
      disabled={isActive}
      className="flex-row items-center px-4 py-3.5 active:bg-secondary"
    >
      <View className="flex-1">
        <Text className="text-base font-medium text-foreground">{name}</Text>
        <Text className="text-xs text-muted-foreground mt-0.5">/{slug}</Text>
      </View>
      <Ionicons
        name={isActive ? "checkmark" : "chevron-forward"}
        size={18}
        color={iconColor}
      />
    </Pressable>
  );
}
