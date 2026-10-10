/**
 * HarmonyOS port of apps/mobile/app/(app)/[workspace]/more/settings/
 * profile.tsx — name + avatar.
 *
 * Avatar tap opens a chooser. iOS uses an ActionSheetIOS (Take Photo /
 * Choose from Library / Remove); RNOH has no action-sheet bridge, so the
 * same choices render through RN Alert.alert and the actual picking goes
 * through @/lib/media-picker's pickImage() (camera/library handled there).
 *
 * Save runs PATCH /api/me then writes the returned user back to the auth
 * store via setUser — same source-of-truth pattern as web (server response
 * is authoritative, never the local form state).
 */
import React, { useEffect, useState } from "react";
import {
  ActivityIndicator,
  Alert,
  Pressable,
  ScrollView,
  StyleSheet,
  View,
} from "react-native";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { TextField } from "@/components/ui/text-field";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Separator } from "@/components/ui/separator";
import { IconButton } from "@/components/ui/icon-button";
import { SafeAreaView } from "@/lib/safe-area";
import { useAuthStore } from "@/data/auth-store";
import { api } from "@/data/api";
import type { FileAsset } from "@/data/api";
import {
  filenameFromUri,
  mimeTypeFromFilename,
  pickImage,
} from "@/lib/media-picker";
import type { ThemeColors } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

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

export function SettingsProfileScreen({ onBack }: { onBack?: () => void }) {
  const c = useThemeColors();
  const s = styles(c);
  const user = useAuthStore((state) => state.user);
  const setUser = useAuthStore((state) => state.setUser);

  const [name, setName] = useState(user?.name ?? "");
  const [saving, setSaving] = useState(false);
  const [uploading, setUploading] = useState(false);

  // Resync if `user` updates from outside (avatar upload, refetch, login as
  // different user). Without this the form would render stale init forever.
  useEffect(() => {
    setName(user?.name ?? "");
  }, [user]);

  const dirty = name.trim() !== (user?.name ?? "") && name.trim().length > 0;

  const handleAvatarPick = () => {
    if (uploading) return;
    const buttons: Array<{
      text: string;
      style?: "cancel" | "destructive";
      onPress?: () => void;
    }> = [
      {
        text: "Choose Photo",
        onPress: () => {
          void pickAndUpload();
        },
      },
    ];
    if (user?.avatar_url) {
      buttons.push({
        text: "Remove Photo",
        style: "destructive",
        onPress: () => {
          void removeAvatar();
        },
      });
    }
    buttons.push({ text: "Cancel", style: "cancel" });
    // iOS's ActionSheetIOS stand-in: same choices, same order.
    Alert.alert("Profile photo", undefined, buttons);
  };

  const pickAndUpload = async () => {
    const uri = await pickImage();
    if (!uri) return; // Cancelled or picker unavailable.
    const name = filenameFromUri(uri);
    const fileAsset: FileAsset = {
      uri,
      name,
      type: mimeTypeFromFilename(name, "image/jpeg"),
    };

    setUploading(true);
    try {
      const attachment = await api.uploadFile(fileAsset);
      const updated = await api.updateMe({ avatar_url: attachment.url });
      setUser(updated);
    } catch (err) {
      Alert.alert(
        "Upload failed",
        err instanceof Error ? err.message : "Could not upload avatar.",
      );
    } finally {
      setUploading(false);
    }
  };

  const removeAvatar = async () => {
    setUploading(true);
    try {
      const updated = await api.updateMe({ avatar_url: "" });
      setUser(updated);
    } catch (err) {
      Alert.alert(
        "Remove failed",
        err instanceof Error ? err.message : "Could not remove avatar.",
      );
    } finally {
      setUploading(false);
    }
  };

  const handleSave = async () => {
    if (!dirty) return;
    setSaving(true);
    try {
      const updated = await api.updateMe({ name: name.trim() });
      setUser(updated);
    } catch (err) {
      Alert.alert(
        "Save failed",
        err instanceof Error ? err.message : "Could not update profile.",
      );
    } finally {
      setSaving(false);
    }
  };

  return (
    <SafeAreaView edges={["top"]} style={s.screen}>
      {onBack ? (
        <View style={s.titleRow}>
          <IconButton name="chevron-back" onPress={onBack} accessibilityLabel="Back" />
          <Text style={s.title}>Profile</Text>
        </View>
      ) : null}
      <ScrollView
        style={s.flex}
        contentContainerStyle={s.content}
        keyboardShouldPersistTaps="handled"
        showsVerticalScrollIndicator={false}
      >
        <View style={s.avatarSection}>
          <Pressable onPress={handleAvatarPick} disabled={uploading}>
            <Avatar
              accessibilityLabel={user?.name ?? "Your avatar"}
              style={s.avatar}
            >
              {user?.avatar_url ? (
                <AvatarImage source={{ uri: user.avatar_url }} />
              ) : null}
              <AvatarFallback>
                <Text style={s.avatarInitials}>{initialsOf(user?.name)}</Text>
              </AvatarFallback>
            </Avatar>
          </Pressable>
          {uploading ? (
            <ActivityIndicator color={c.mutedForeground} />
          ) : (
            <Text style={[s.hint, { color: c.mutedForeground }]}>
              Tap to change photo
            </Text>
          )}
        </View>

        <Separator />

        <View style={s.form}>
          <View>
            <Text style={[s.fieldLabel, { color: c.mutedForeground }]}>Name</Text>
            <TextField
              value={name}
              onChangeText={setName}
              placeholder="Your name"
              placeholderTextColor={c.mutedForeground}
              autoCapitalize="words"
              autoCorrect={false}
              returnKeyType="done"
            />
          </View>
          <View>
            <Text style={[s.fieldLabel, { color: c.mutedForeground }]}>Email</Text>
            <View
              style={[
                s.readonlyField,
                { borderColor: c.border, backgroundColor: c.muted },
              ]}
            >
              <Text style={[s.readonlyValue, { color: c.mutedForeground }]}>
                {user?.email ?? "—"}
              </Text>
            </View>
            <Text style={[s.fieldHint, { color: c.mutedForeground }]}>
              Email is set at sign-up and can't be changed here.
            </Text>
          </View>
        </View>

        <Button onPress={() => void handleSave()} disabled={!dirty || saving}>
          <Text>{saving ? "Saving…" : "Save"}</Text>
        </Button>
      </ScrollView>
    </SafeAreaView>
  );
}

const styles = (c: ThemeColors) =>
  StyleSheet.create({
    flex: { flex: 1 },
    screen: { flex: 1, backgroundColor: c.background },
    content: { paddingHorizontal: 16, paddingVertical: 24, gap: 24 },
    titleRow: {
      flexDirection: "row",
      alignItems: "center",
      height: 48,
      paddingLeft: 8,
    },
    title: { fontSize: 18, fontWeight: "600", color: c.foreground },
    avatarSection: { alignItems: "center", gap: 12 },
    avatar: { width: 96, height: 96 },
    avatarInitials: {
      fontSize: 24,
      fontWeight: "600",
      color: c.mutedForeground,
    },
    hint: { fontSize: 12 },
    form: { gap: 16 },
    fieldLabel: { fontSize: 12, marginBottom: 6 },
    readonlyField: {
      borderRadius: 6,
      borderWidth: 1,
      paddingHorizontal: 12,
      paddingVertical: 10,
    },
    readonlyValue: { fontSize: 16 },
    fieldHint: { fontSize: 12, marginTop: 6 },
  });
