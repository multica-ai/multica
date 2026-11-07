import { ActivityIndicator, Pressable, ScrollView, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { DraftAttachment } from "@/data/ai-sessions/drafts";
import { isPhoto } from "@/data/ai-sessions/photo-attachments";
import { Text } from "@/components/ui/text";
import { IconButton } from "@/components/ui/icon-button";
import { useLightbox } from "@/lib/markdown/lightbox-provider";
import { useT } from "@/lib/i18n";
import { THEME } from "@/lib/theme";
import { useColorScheme } from "@/lib/use-color-scheme";

export function SessionDraftAttachments({
  files,
  disabled,
  onOpen,
  onRemove,
  onRetry,
  failure,
}: {
  files: readonly DraftAttachment[];
  disabled: boolean;
  onOpen(file: DraftAttachment): void;
  onRemove(id: string): void;
  onRetry(id: string): void;
  failure(id: string): string | undefined;
}) {
  const { t } = useT("aiSessions");
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  const lightbox = useLightbox();
  if (!files.length) return null;
  const failed = files.some((file) => file.status === "failed");
  const uploading = files.some((file) => file.status === "uploading");
  const error = failure(
    files.find((file) => file.status === "failed")?.id ?? "",
  );
  const failureKey =
    error === "relay_transfer_limited"
      ? "photos.relay_limited"
      : error === "upload_too_large"
        ? "photos.upload_too_large"
        : error === "upload_stalled"
          ? "photos.stalled"
          : "attachment_failed";
  return (
    <View>
      <ScrollView
        horizontal
        keyboardShouldPersistTaps="handled"
        contentContainerStyle={{ gap: 8, paddingVertical: 4 }}
      >
        {files.map((file) => (
          <View
            key={file.id}
            className="max-w-64 flex-row items-center rounded-md border border-border bg-background"
            style={{ height: 28 }}
          >
            <Pressable
              accessibilityRole="button"
              accessibilityLabel={t("photos.preview", { name: file.name })}
              disabled={!file.uri && file.status !== "uploaded"}
              className="flex-row shrink items-center gap-1 px-2"
              style={{ height: 26 }}
              onPress={() =>
                file.uri && isPhoto(file)
                  ? lightbox.open(file.uri)
                  : onOpen(file)
              }
            >
              {file.status === "uploading" ? (
                <ActivityIndicator size="small" />
              ) : (
                <Ionicons
                  name={
                    failed && file.status === "failed"
                      ? "alert-circle-outline"
                      : isPhoto(file)
                        ? "image-outline"
                        : "document-outline"
                  }
                  size={16}
                  color={
                    file.status === "failed"
                      ? theme.destructive
                      : theme.foreground
                  }
                />
              )}
              <Text
                numberOfLines={1}
                style={{ fontSize: 12, lineHeight: 16, flexShrink: 1 }}
                className={
                  file.status === "failed"
                    ? "text-destructive"
                    : "text-foreground"
                }
              >
                {file.name}
              </Text>
            </Pressable>
            {file.status === "failed" && (
              <IconButton
                name="refresh"
                iconSize={16}
                accessibilityLabel={t("photos.retry", { name: file.name })}
                disabled={disabled}
                style={{ height: 26, width: 28 }}
                onPress={() => onRetry(file.id)}
              />
            )}
            <IconButton
              name="close"
              iconSize={16}
              accessibilityLabel={t("remove_file", { name: file.name })}
              disabled={disabled}
              style={{ height: 26, width: 28 }}
              onPress={() => onRemove(file.id)}
            />
          </View>
        ))}
      </ScrollView>
      <Text
        accessibilityRole={failed ? "alert" : undefined}
        style={{ fontSize: 12, lineHeight: 16 }}
        className={failed ? "text-destructive" : "text-muted-foreground"}
      >
        {t(failed ? failureKey : uploading ? "uploading" : "photos.ready", {
          count: files.length,
        })}
      </Text>
    </View>
  );
}
