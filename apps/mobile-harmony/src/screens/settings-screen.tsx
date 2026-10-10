/**
 * HarmonyOS port of apps/mobile/app/(app)/[workspace]/more/settings.tsx —
 * account info, workspace switching, appearance, profile and notifications
 * subscreens, and sign out.
 *
 * Navigation deltas from the iOS file (expo-router): the subscreen pushes,
 * the stack back gesture and the post-switch stack reset are owned by the
 * app shell, so they arrive as optional callbacks (onOpenProfile,
 * onOpenNotifications, onBack, onSignedOut). Switching a workspace inline
 * only updates the workspace store — the shell's session effect observes the
 * slug change and resets the stack, which is the `router.replace` equivalent.
 *
 * Theme persistence goes through lib/use-color-scheme (device-storage here,
 * SecureStore on iOS).
 */
import React from "react";
import { ActivityIndicator, Alert, ScrollView, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { Workspace } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { IconButton } from "@/components/ui/icon-button";
import { Icon } from "@/components/ui/icon";
import { SafeAreaView, useSafeAreaInsets } from "@/lib/safe-area";
import { workspaceListOptions } from "@/data/queries/workspaces";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  useColorScheme,
  type ThemePreference,
} from "@/lib/use-color-scheme";
import type { ThemeColors } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

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

export function SettingsScreen({
  onOpenProfile,
  onOpenNotifications,
  onBack,
  onSignedOut,
}: {
  onOpenProfile?: () => void;
  onOpenNotifications?: () => void;
  onBack?: () => void;
  onSignedOut?: () => void;
}) {
  const c = useThemeColors();
  const s = styles(c);
  const insets = useSafeAreaInsets();
  const queryClient = useQueryClient();
  const user = useAuthStore((state) => state.user);
  const logout = useAuthStore((state) => state.logout);
  const currentSlug = useWorkspaceStore((state) => state.currentWorkspaceSlug);
  const setCurrentWorkspace = useWorkspaceStore(
    (state) => state.setCurrentWorkspace,
  );
  const { data, isLoading, error } = useQuery(workspaceListOptions());
  const { preference, setPreference } = useColorScheme();

  const onSwitch = async (ws: Workspace) => {
    if (ws.slug === currentSlug) return;
    await setCurrentWorkspace(ws.id, ws.slug);
    // Stack reset is the shell's job: it observes the slug change.
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
          onPress: () => {
            void (async () => {
              // Same teardown order as the shell's 401 path: clear the
              // persisted workspace first, then the credential, then every
              // cache. The shell's session effect lands on the login screen.
              await useWorkspaceStore.getState().clear();
              await logout();
              queryClient.clear();
              onSignedOut?.();
            })();
          },
        },
      ],
    );
  };

  return (
    <SafeAreaView edges={["top"]} style={s.screen}>
      {onBack ? (
        <View style={[s.titleRow, { paddingRight: Math.max(insets.right, 16) }]}>
          <IconButton
            name="chevron-back"
            onPress={onBack}
            accessibilityLabel="Back"
          />
          <Text style={s.title}>Settings</Text>
        </View>
      ) : null}
      <ScrollView
        style={s.flex}
        contentContainerStyle={s.content}
        showsVerticalScrollIndicator={false}
      >
        <SectionGroup title="Account" c={c} s={s}>
          <NavRow
            onPress={() => onOpenProfile?.()}
            chevronColor={c.mutedForeground}
            s={s}
            leading={
              <Avatar
                accessibilityLabel={user?.name ?? "User avatar"}
                style={s.avatar}
              >
                {user?.avatar_url ? (
                  <AvatarImage source={{ uri: user.avatar_url }} />
                ) : null}
                <AvatarFallback>
                  <Text style={s.avatarInitials}>
                    {initialsOf(user?.name)}
                  </Text>
                </AvatarFallback>
              </Avatar>
            }
            title={user?.name ?? "—"}
            subtitle={user?.email}
            c={c}
          />
          <Separator />
          <NavRow
            onPress={() => onOpenNotifications?.()}
            chevronColor={c.mutedForeground}
            title="Notifications"
            subtitle="Inbox and system alerts"
            c={c}
            s={s}
          />
        </SectionGroup>

        <SectionGroup title="Workspaces" c={c} s={s}>
          {isLoading ? (
            <View style={s.loadingWrap}>
              <ActivityIndicator color={c.mutedForeground} />
            </View>
          ) : error ? (
            <View style={s.errorWrap}>
              <Text style={s.errorText}>Failed to load workspaces</Text>
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
                    iconColor={c.mutedForeground}
                    onPress={() => {
                      void onSwitch(ws);
                    }}
                    c={c}
                    s={s}
                  />
                  {!isLast ? <Separator /> : null}
                </View>
              );
            })
          )}
        </SectionGroup>

        <SectionGroup title="Appearance" c={c} s={s}>
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
            style={s.radioGroup}
          >
            {THEME_OPTIONS.map((opt, idx) => {
              const isLast = idx === THEME_OPTIONS.length - 1;
              return (
                <View key={opt.value}>
                  <Pressable
                    onPress={() => setPreference(opt.value)}
                    style={({ pressed }) => [
                      s.appearanceRow,
                      pressed ? s.pressedRow : null,
                    ]}
                  >
                    <RadioGroupItem value={opt.value} />
                    <Text style={s.appearanceLabel}>{opt.label}</Text>
                  </Pressable>
                  {!isLast ? <Separator /> : null}
                </View>
              );
            })}
          </RadioGroup>
        </SectionGroup>

        <View style={s.signOutWrap}>
          <Button variant="destructive" onPress={onSignOut}>
            <Text>Sign out</Text>
          </Button>
        </View>
      </ScrollView>
    </SafeAreaView>
  );
}

function NavRow({
  onPress,
  leading,
  title,
  subtitle,
  chevronColor,
  c,
  s,
}: {
  onPress: () => void;
  leading?: React.ReactNode;
  title: string;
  subtitle?: string;
  chevronColor: string;
  c: ThemeColors;
  s: ReturnType<typeof styles>;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [
        s.row,
        pressed ? s.pressedRow : null,
      ]}
    >
      {leading}
      <View style={s.rowTextWrap}>
        <Text style={s.rowTitle}>{title}</Text>
        {subtitle ? (
          <Text style={[s.rowSubtitle, { color: c.mutedForeground }]}>
            {subtitle}
          </Text>
        ) : null}
      </View>
      <Icon name="chevron-forward" size={18} color={chevronColor} />
    </Pressable>
  );
}

function SectionGroup({
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
      <Text style={[s.sectionTitle, { color: c.mutedForeground }]}>
        {title}
      </Text>
      <View
        style={[
          s.card,
          { borderColor: c.border, backgroundColor: c.card },
        ]}
      >
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
  c,
  s,
}: {
  name: string;
  slug: string;
  isActive: boolean;
  iconColor: string;
  onPress: () => void;
  c: ThemeColors;
  s: ReturnType<typeof styles>;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      disabled={isActive}
      style={({ pressed }) => [
        s.row,
        pressed && !isActive ? s.pressedRow : null,
      ]}
    >
      <View style={s.rowTextWrap}>
        <Text style={s.rowTitle}>{name}</Text>
        <Text style={[s.rowSlug, { color: c.mutedForeground }]}>/{slug}</Text>
      </View>
      <Icon
        name={isActive ? "checkmark" : "chevron-forward"}
        size={18}
        color={iconColor}
      />
    </Pressable>
  );
}

const styles = (c: ThemeColors) =>
  StyleSheet.create({
    flex: { flex: 1 },
    screen: { flex: 1, backgroundColor: c.background },
    content: { paddingHorizontal: 16, paddingVertical: 16, gap: 24 },
    titleRow: {
      flexDirection: "row",
      alignItems: "center",
      gap: 4,
      paddingLeft: 8,
      height: 48,
    },
    title: { fontSize: 18, fontWeight: "600", color: c.foreground },
    section: { gap: 8 },
    sectionTitle: {
      fontSize: 12,
      textTransform: "uppercase",
      letterSpacing: 0.8,
      paddingHorizontal: 4,
    },
    card: {
      borderRadius: 6,
      borderWidth: 1,
      overflow: "hidden",
    },
    row: {
      flexDirection: "row",
      alignItems: "center",
      paddingHorizontal: 16,
      paddingVertical: 14,
      gap: 12,
    },
    pressedRow: { backgroundColor: c.secondary },
    rowTextWrap: { flex: 1 },
    rowTitle: { fontSize: 16, fontWeight: "500", color: c.foreground },
    rowSubtitle: { fontSize: 14, marginTop: 2 },
    rowSlug: { fontSize: 12, marginTop: 2 },
    avatar: { width: 40, height: 40 },
    avatarInitials: {
      fontSize: 14,
      fontWeight: "600",
      color: c.mutedForeground,
    },
    loadingWrap: { paddingVertical: 16, alignItems: "center" },
    errorWrap: { padding: 16 },
    errorText: { fontSize: 14, color: c.destructive },
    radioGroup: { gap: 0 },
    appearanceRow: {
      flexDirection: "row",
      alignItems: "center",
      paddingHorizontal: 16,
      paddingVertical: 14,
      gap: 12,
    },
    appearanceLabel: {
      flex: 1,
      fontSize: 16,
      fontWeight: "500",
      color: c.foreground,
    },
    signOutWrap: { paddingTop: 8 },
  });
