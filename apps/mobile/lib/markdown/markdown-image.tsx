/**
 * Block-level image with real aspect ratio + tap-to-lightbox.
 *
 *   - Aspect ratio detection uses RN's `Image.getSize` (cross-platform,
 *     network-friendly). While dimensions resolve we lay out at 16:9 as
 *     a placeholder — same width-100% so the surrounding flow is stable
 *     and only the height shifts once the real ratio lands.
 *   - Rendering uses `expo-image` for on-disk caching + smooth fade-in
 *     transition.
 *   - Tap dispatches into the global LightboxProvider for fullscreen
 *     viewing with pinch-zoom + swipe-down-to-dismiss.
 *
 * URI resolution: markdown content authored in Multica stores image
 * references using the internal `mc://file/<id>` scheme rather than
 * baking signed HTTPS URLs into the content (signed URLs expire). iOS
 * doesn't understand `mc://`, so we look the URI up in the supplied
 * `attachments` list and swap it for the matching `download_url` before
 * passing to any image API. Unmatched URIs fall through unchanged —
 * external https links and well-known schemes load directly; an
 * unknown reference fails the getSize callback and we fall back to a
 * 16:9 placeholder slot.
 *
 * Cancellation: a content re-render that swaps the URI must not let the
 * previous getSize callback overwrite state — guard with a `cancelled`
 * flag in the cleanup path.
 */
import { useEffect, useMemo, useState } from "react";
import { Image as RNImage, Pressable, View } from "react-native";
import { Image as ExpoImage } from "expo-image";
import type { Attachment } from "@multica/core/types";
import { useLightbox } from "./lightbox-provider";

interface Props {
  uri: string;
  alt?: string;
  attachments?: Attachment[];
}

export function MarkdownImage({ uri, attachments }: Props) {
  const { open } = useLightbox();
  const [aspect, setAspect] = useState<number | null>(null);

  const resolvedUri = useMemo(() => {
    if (!attachments || attachments.length === 0) return uri;
    const match = attachments.find((a) => a.url === uri);
    return match?.download_url || uri;
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
        // Network failure / decode failure / 404 / unknown URI scheme
        // (e.g. unresolved mc://) — keep the 16:9 fallback so the slot
        // still shows the muted background instead of collapsing.
        if (!cancelled) setAspect(16 / 9);
      },
    );
    return () => {
      cancelled = true;
    };
  }, [resolvedUri]);

  return (
    <Pressable onPress={() => open(resolvedUri)}>
      <View className="rounded-lg overflow-hidden bg-muted">
        <ExpoImage
          source={{ uri: resolvedUri }}
          style={{ width: "100%", aspectRatio: aspect ?? 16 / 9 }}
          contentFit="contain"
          transition={150}
        />
      </View>
    </Pressable>
  );
}
