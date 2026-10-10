/**
 * Block-level image with real aspect ratio + tap-to-fullscreen — HarmonyOS
 * port of apps/mobile/lib/markdown/markdown-image.tsx. Divergences:
 * RN <Image> instead of expo-image (no fade-in transition), and the
 * fullscreen viewer is a per-image RN <Modal> instead of the shared
 * LightboxProvider + image-sequence (swipe between a message's images is
 * roadmap work). mc:// resolution, aspect-ratio probing and the 16:9
 * fallback behave identically.
 */
import { useEffect, useMemo, useState } from "react";
import {
  Image as RNImage,
  Modal,
  Pressable,
  StyleSheet,
  View,
} from "react-native";
import type { Attachment } from "@multica/core/types";
import { matchAttachmentByURL } from "@multica/core/attachments/image-sequence";
import { resolveAttachmentUrl } from "@/lib/attachment-url";
import { useThemeColors } from "@/lib/use-theme-colors";
import { MOBILE_RADIUS } from "@/lib/radius";

export function MarkdownImage({
  uri,
  attachments,
}: {
  uri: string;
  alt?: string;
  attachments?: Attachment[];
}) {
  const c = useThemeColors();
  const [aspect, setAspect] = useState<number | null>(null);
  const [full, setFull] = useState(false);

  const resolvedUri = useMemo(() => {
    const match = matchAttachmentByURL(uri, attachments);
    const candidate: string | null | undefined = match
      ? match.download_url || match.markdown_url || match.url
      : uri;
    return resolveAttachmentUrl(candidate) ?? uri;
  }, [uri, attachments]);

  useEffect(() => {
    let cancelled = false;
    RNImage.getSize(
      resolvedUri,
      (w, h) => {
        if (cancelled || !w || !h) return;
        setAspect(w / h);
      },
      () => {
        if (!cancelled) setAspect(16 / 9);
      },
    );
    return () => {
      cancelled = true;
    };
  }, [resolvedUri]);

  return (
    <>
      <Pressable onPress={() => setFull(true)}>
        <View
          style={[
            styles.frame,
            { backgroundColor: c.muted, borderRadius: MOBILE_RADIUS.md },
          ]}
        >
          <RNImage
            source={{ uri: resolvedUri }}
            style={{ width: "100%", aspectRatio: aspect ?? 16 / 9 }}
            resizeMode="contain"
          />
        </View>
      </Pressable>
      <Modal visible={full} transparent animationType="fade" onRequestClose={() => setFull(false)}>
        <Pressable style={styles.lightbox} onPress={() => setFull(false)}>
          <RNImage
            source={{ uri: resolvedUri }}
            style={styles.lightboxImage}
            resizeMode="contain"
          />
        </Pressable>
      </Modal>
    </>
  );
}

const styles = StyleSheet.create({
  frame: { overflow: "hidden" },
  lightbox: {
    flex: 1,
    backgroundColor: "rgba(0,0,0,0.92)",
    alignItems: "center",
    justifyContent: "center",
  },
  lightboxImage: { width: "100%", height: "100%" },
});
