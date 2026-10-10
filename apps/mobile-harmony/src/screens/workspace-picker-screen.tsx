/**
 * HarmonyOS port of apps/mobile/app/(app)/select-workspace.tsx — the
 * post-login workspace chooser. "Signed in as" header, one CardPressable
 * per workspace (name, /slug, optional description), a Retry button on
 * load errors, an empty-state hint, and a Sign out escape hatch.
 *
 * Keeps this slice's props contract (onSelected) — app-shell.tsx owns the
 * post-selection navigation. Sign out is fire-and-forget exactly like the
 * iOS screen (`logout()` unawaited); the shell's session effect lands on
 * the login screen.
 */
import React from "react";
import { ActivityIndicator, ScrollView, StyleSheet, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import type { Workspace } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { CardPressable } from "@/components/ui/card";
import { SafeAreaView } from "@/lib/safe-area";
import { workspaceListOptions } from "@/data/queries/workspaces";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import type { ThemeColors } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

export function WorkspacePickerScreen({
  onSelected,
}: {
  onSelected: (workspace: Workspace) => void;
}) {
  const c = useThemeColors();
  const s = styles(c);
  const user = useAuthStore((state) => state.user);
  const logout = useAuthStore((state) => state.logout);
  const setCurrentWorkspace = useWorkspaceStore(
    (state) => state.setCurrentWorkspace,
  );
  const { data, isLoading, error, refetch } = useQuery(workspaceListOptions());

  const onSelect = async (ws: Workspace) => {
    await setCurrentWorkspace(ws.id, ws.slug);
    onSelected(ws);
  };

  return (
    <SafeAreaView style={s.screen}>
      <ScrollView contentContainerStyle={s.content}>
        <View style={s.signedInAs}>
          <Text style={[s.signedInAsLabel, { color: c.mutedForeground }]}>
            Signed in as
          </Text>
          <Text style={[s.signedInAsValue, { color: c.foreground }]}>
            {user?.email}
          </Text>
        </View>

        <View style={s.section}>
          <Text style={[s.title, { color: c.foreground }]}>
            Select a workspace
          </Text>

          {isLoading ? (
            <View style={s.loadingWrap}>
              <ActivityIndicator color={c.mutedForeground} />
            </View>
          ) : error ? (
            <View style={s.errorWrap}>
              <Text style={[s.errorText, { color: c.destructive }]}>
                Failed to load workspaces:{" "}
                {error instanceof Error ? error.message : "unknown error"}
              </Text>
              <Button variant="outline" onPress={() => void refetch()}>
                <Text>Retry</Text>
              </Button>
            </View>
          ) : !data || data.length === 0 ? (
            <Text style={[s.emptyText, { color: c.mutedForeground }]}>
              You don't belong to any workspaces yet. Contact your workspace
              admin to be invited.
            </Text>
          ) : (
            <View style={s.cardList}>
              {data.map((ws) => (
                <CardPressable
                  key={ws.id}
                  accessibilityLabel={`Select workspace ${ws.name}`}
                  onPress={() => {
                    void onSelect(ws);
                  }}
                >
                  <Text style={[s.cardName, { color: c.foreground }]}>
                    {ws.name}
                  </Text>
                  <Text style={[s.cardSlug, { color: c.mutedForeground }]}>
                    /{ws.slug}
                  </Text>
                  {ws.description ? (
                    <Text style={[s.cardDescription, { color: c.mutedForeground }]}>
                      {ws.description}
                    </Text>
                  ) : null}
                </CardPressable>
              ))}
            </View>
          )}
        </View>

        <View style={s.signOutWrap}>
          <Button
            variant="outline"
            onPress={() => {
              void logout();
            }}
          >
            <Text>Sign out</Text>
          </Button>
        </View>
      </ScrollView>
    </SafeAreaView>
  );
}

const styles = (c: ThemeColors) =>
  StyleSheet.create({
    screen: { flex: 1, backgroundColor: c.background },
    content: { paddingHorizontal: 24, paddingVertical: 24, gap: 24 },
    signedInAs: { gap: 4 },
    signedInAsLabel: {
      fontSize: 12,
      textTransform: "uppercase",
      letterSpacing: 0.8,
    },
    signedInAsValue: { fontSize: 16 },
    section: { gap: 12 },
    title: { fontSize: 24, fontWeight: "600", color: c.foreground },
    loadingWrap: { paddingVertical: 32, alignItems: "center" },
    errorWrap: { gap: 12 },
    errorText: { fontSize: 14 },
    emptyText: { fontSize: 14 },
    cardList: { gap: 12 },
    cardName: { fontSize: 16, fontWeight: "600" },
    cardSlug: { fontSize: 12, marginTop: 4 },
    cardDescription: { fontSize: 14, marginTop: 8 },
    // pt-4 border-t border-border
    signOutWrap: {
      paddingTop: 16,
      borderTopWidth: 1,
      borderTopColor: c.border,
    },
  });
