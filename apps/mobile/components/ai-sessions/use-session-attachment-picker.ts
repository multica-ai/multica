import { useEffect, useRef, useState } from "react";
import { Alert, Keyboard, Linking } from "react-native";
import * as ImagePicker from "expo-image-picker";
import * as DocumentPicker from "expo-document-picker";
import type { AISessionClient, SessionScope } from "@/data/ai-sessions/client";
import {
  useAIDrafts,
  EMPTY_DRAFT_ATTACHMENTS,
} from "@/data/ai-sessions/drafts";
import { draftUploads } from "@/data/ai-sessions/uploads";
import {
  importPickedAttachments,
  MAX_ATTACHMENTS,
  MAX_PHOTOS,
  photoPickerOptions,
} from "@/data/ai-sessions/photo-attachments";
import { photoPreparation } from "@/data/ai-sessions/photo-files";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useT } from "@/lib/i18n";

export function useSessionAttachmentPicker(
  client: AISessionClient | null,
  scope: SessionScope,
  draftKey: string,
  active: boolean,
  enabled: boolean,
) {
  const { t } = useT("aiSessions");
  const [selecting, setSelecting] = useState(false);
  const locked = useRef(false);
  const current = useRef({ draftKey, active });
  useEffect(() => {
    current.current = { draftKey, active };
  }, [draftKey, active]);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const pick = async (photosOnly: boolean) => {
    if (!client || !enabled || locked.current) return;
    const existing = () =>
      useAIDrafts.getState().attachments[draftKey] ?? EMPTY_DRAFT_ATTACHMENTS;
    const options = photoPickerOptions(existing());
    if ((photosOnly && !options) || existing().length >= MAX_ATTACHMENTS) {
      Alert.alert(
        t("error_title"),
        t(
          existing().length >= MAX_ATTACHMENTS
            ? "photos.attachment_limit"
            : "photos.photo_limit",
          {
            max:
              existing().length >= MAX_ATTACHMENTS
                ? MAX_ATTACHMENTS
                : MAX_PHOTOS,
          },
        ),
      );
      return;
    }
    const isCurrent = () =>
      mounted.current &&
      current.current.active &&
      current.current.draftKey === draftKey &&
      useAuthStore.getState().user?.id === scope.userId &&
      useWorkspaceStore.getState().currentWorkspaceId === scope.workspaceId;
    locked.current = true;
    setSelecting(true);
    Keyboard.dismiss();
    try {
      const result = photosOnly
        ? await ImagePicker.launchImageLibraryAsync(options!)
        : await DocumentPicker.getDocumentAsync({
            multiple: true,
            copyToCacheDirectory: true,
          });
      if (result.canceled || !isCurrent()) return;
      const assets = result.assets.map((asset, index) => ({
        uri: asset.uri,
        name:
          "name" in asset
            ? asset.name
            : (asset.fileName ?? `photo-${Date.now()}-${index + 1}`),
        mimeType: asset.mimeType,
      }));
      const errors = await importPickedAttachments(assets, photosOnly, {
        ...photoPreparation,
        existing,
        isCurrent,
        accept: (asset) => {
          draftUploads.start(client, draftKey, asset);
        },
      });
      if (isCurrent() && errors.length)
        Alert.alert(
          t("error_title"),
          errors
            .map(({ name, code }) =>
              t(`photos.${code}`, {
                name,
                max: code === "attachment_limit" ? MAX_ATTACHMENTS : MAX_PHOTOS,
              }),
            )
            .join("\n"),
        );
    } catch (error) {
      if (!isCurrent()) return;
      const denied =
        error instanceof Error &&
        /permission|denied|restricted|access/i.test(error.message);
      Alert.alert(
        t("error_title"),
        t(denied ? "photos.permission" : "photos.read_failed"),
        denied
          ? [
              { text: t("cancel"), style: "cancel" },
              {
                text: t("photos.settings"),
                onPress: () => {
                  void Linking.openSettings();
                },
              },
            ]
          : undefined,
      );
    } finally {
      locked.current = false;
      if (mounted.current) setSelecting(false);
    }
  };
  return { pick, selecting };
}
