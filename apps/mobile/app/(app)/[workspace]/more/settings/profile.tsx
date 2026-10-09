/**
 * Profile edit subscreen — name + avatar.
 *
 * Avatar tap opens an iOS native ActionSheet (Take Photo / Choose from Library
 * / Remove). Mirrors the avatar upload flow in
 * packages/views/settings/components/account-tab.tsx but the picker uses
 * native APIs per CLAUDE.md "iOS native > RNR > discuss" waterfall.
 *
 * Save runs PATCH /api/me then writes the returned user back to the auth
 * store via setUser — same source-of-truth pattern as web (server response
 * is authoritative, never the local form state).
 */
import { useEffect, useState } from "react";
import {
  ActionSheetIOS,
  Alert,
  ActivityIndicator,
  Pressable,
  ScrollView,
  View,
} from "react-native";
import * as ImagePicker from "expo-image-picker";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { TextField } from "@/components/ui/text-field";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Separator } from "@/components/ui/separator";
import { useAuthStore } from "@/data/auth-store";
import { api } from "@/data/api";
import type { FileAsset } from "@/data/api";
import { useT } from "@/lib/i18n";

const MAX_AVATAR_BYTES = 5 * 1024 * 1024; // 5 MB — matches what's reasonable on cellular.

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

export default function ProfileSettingsScreen() {
  const user = useAuthStore((s) => s.user);
  const setUser = useAuthStore((s) => s.setUser);
  const { t } = useT("settings");

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
    const actions = ["camera", "library"];
    if (user?.avatar_url) actions.push("remove");
    actions.push("cancel");
    const removeIndex = user?.avatar_url ? 2 : -1;
    const cancelIndex = actions.length - 1;
    const visibleOptions = actions.map((action) =>
      action === "camera"
        ? t("avatar.take")
        : action === "library"
          ? t("avatar.library")
          : action === "remove"
            ? t("avatar.remove")
            : t("common:actions.cancel"),
    );

    ActionSheetIOS.showActionSheetWithOptions(
      {
        options: visibleOptions,
        cancelButtonIndex: cancelIndex,
        destructiveButtonIndex: removeIndex >= 0 ? removeIndex : undefined,
      },
      async (index) => {
        if (index === cancelIndex) return;
        const action = actions[index];
        if (action === "camera") await pickFromCamera();
        else if (action === "library") await pickFromLibrary();
        else if (action === "remove") await removeAvatar();
      },
    );
  };

  const pickFromCamera = async () => {
    const perm = await ImagePicker.requestCameraPermissionsAsync();
    if (!perm.granted) {
      Alert.alert(
        t("avatar.permission_title"),
        t("avatar.permission_message"),
      );
      return;
    }
    const result = await ImagePicker.launchCameraAsync({
      mediaTypes: ["images"],
      allowsEditing: true,
      aspect: [1, 1],
      quality: 0.8,
    });
    if (!result.canceled) await uploadAvatar(result.assets[0]);
  };

  const pickFromLibrary = async () => {
    const result = await ImagePicker.launchImageLibraryAsync({
      mediaTypes: ["images"],
      allowsEditing: true,
      aspect: [1, 1],
      quality: 0.8,
    });
    if (!result.canceled) await uploadAvatar(result.assets[0]);
  };

  const uploadAvatar = async (asset: ImagePicker.ImagePickerAsset) => {
    if (asset.fileSize && asset.fileSize > MAX_AVATAR_BYTES) {
      Alert.alert(t("avatar.too_large_title"), t("avatar.too_large_message"));
      return;
    }
    const fileAsset: FileAsset = {
      uri: asset.uri,
      // expo-image-picker doesn't always supply a fileName (camera captures);
      // fabricate one from the URI so the multipart upload has a stable name.
      name: asset.fileName ?? `avatar-${Date.now()}.jpg`,
      type: asset.mimeType ?? "image/jpeg",
    };

    setUploading(true);
    try {
      const attachment = await api.uploadFile(fileAsset);
      const updated = await api.updateMe({ avatar_url: attachment.url });
      setUser(updated);
    } catch (err) {
      Alert.alert(
        t("avatar.upload_failed"),
        err instanceof Error
          ? err.message
          : t("avatar.upload_failed_default"),
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
        t("avatar.remove_failed"),
        err instanceof Error
          ? err.message
          : t("avatar.remove_failed_default"),
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
        t("errors.save_failed"),
        err instanceof Error ? err.message : t("errors.save_failed_default"),
      );
    } finally {
      setSaving(false);
    }
  };

  return (
    <ScrollView
      className="flex-1 bg-background"
      contentContainerClassName="px-4 py-6 gap-6"
      keyboardShouldPersistTaps="handled"
    >
      <View className="items-center gap-3">
        <Pressable onPress={handleAvatarPick} disabled={uploading}>
          <Avatar alt={user?.name ?? t("profile.name")} className="size-24">
            {user?.avatar_url ? (
              <AvatarImage source={{ uri: user.avatar_url }} />
            ) : null}
            <AvatarFallback>
              <Text className="text-2xl font-semibold text-muted-foreground">
                {initialsOf(user?.name)}
              </Text>
            </AvatarFallback>
          </Avatar>
        </Pressable>
        {uploading ? (
          <ActivityIndicator />
        ) : (
          <Text className="text-xs text-muted-foreground">
            {t("avatar.change_hint")}
          </Text>
        )}
      </View>

      <Separator />

      <View className="gap-4">
        <View>
          <Text className="text-xs text-muted-foreground mb-1.5">
            {t("profile.name")}
          </Text>
          <TextField
            value={name}
            onChangeText={setName}
            placeholder={t("profile.name")}
            autoCapitalize="words"
            autoCorrect={false}
            returnKeyType="done"
          />
        </View>
        <View>
          <Text className="text-xs text-muted-foreground mb-1.5">
            {t("profile.email")}
          </Text>
          <View className="rounded-md border border-border bg-muted px-3 py-2.5">
            <Text className="text-base text-muted-foreground">
              {user?.email ?? "—"}
            </Text>
          </View>
          <Text className="text-xs text-muted-foreground mt-1.5">
            {t("email_fixed")}
          </Text>
        </View>
      </View>

      <Button onPress={handleSave} disabled={!dirty || saving}>
        <Text>{saving ? t("profile.saving") : t("common:actions.save")}</Text>
      </Button>
    </ScrollView>
  );
}
