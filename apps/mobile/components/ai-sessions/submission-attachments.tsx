import { Image, Pressable, View } from "react-native";
import type { MobileSubmission } from "@/data/ai-sessions/submission-outbox";
import { isPhoto } from "@/data/ai-sessions/photo-attachments";
import { Text } from "@/components/ui/text";
import { useLightbox } from "@/lib/markdown/lightbox-provider";
import { SessionAttachment } from "./session-attachment";

export function SubmissionAttachments({
  record,
}: {
  record: MobileSubmission;
}) {
  const lightbox = useLightbox();
  return (
    <View className="gap-2">
      {record.request.attachments.map((id) => {
        const asset = record.assets?.find((file) => file.id === id);
        return asset && isPhoto(asset) && !record.confirmed ? (
          <Pressable
            key={id}
            accessibilityRole="button"
            accessibilityLabel={asset.name}
            onPress={() => lightbox.open(asset.uri)}
            className="gap-1 mt-2"
          >
            <Image
              source={{ uri: asset.uri }}
              resizeMode="contain"
              style={{ width: 220, maxWidth: "100%", aspectRatio: 1.5 }}
            />
            <Text
              numberOfLines={1}
              className="text-muted-foreground"
              style={{ fontSize: 12, lineHeight: 16 }}
            >
              {asset.name}
            </Text>
          </Pressable>
        ) : (
          <SessionAttachment key={id} id={id} />
        );
      })}
    </View>
  );
}
