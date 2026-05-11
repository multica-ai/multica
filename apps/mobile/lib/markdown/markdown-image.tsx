/**
 * Block-level image with real aspect ratio + tap-to-lightbox.
 *
 *   - Aspect ratio detection uses RN's `Image.getSize` (cross-platform,
 *     network-friendly). While dimensions resolve we lay out at 16:9 as
 *     a placeholder — same width-100% so the surrounding flow is stable
 *     and only the height shifts once the real ratio lands.
 *   - Rendering uses `expo-image` for on-disk caching + smooth fade-in
 *     transition (the user sees the muted placeholder background, then
 *     the image fades in).
 *   - Tap dispatches into the global LightboxProvider for fullscreen
 *     viewing with pinch-zoom + swipe-down-to-dismiss.
 *
 * Cancellation: a content re-render that swaps the URI must not let the
 * previous getSize callback overwrite state — guard with a `cancelled`
 * flag in the cleanup path.
 */
import { useEffect, useState } from "react";
import { Image as RNImage, Pressable, View } from "react-native";
import { Image as ExpoImage } from "expo-image";
import { useLightbox } from "./lightbox-provider";

interface Props {
  uri: string;
  alt?: string;
}

export function MarkdownImage({ uri }: Props) {
  const { open } = useLightbox();
  const [aspect, setAspect] = useState<number | null>(null);

  useEffect(() => {
    let cancelled = false;
    RNImage.getSize(
      uri,
      (w, h) => {
        if (cancelled || !w || !h) return;
        setAspect(w / h);
      },
      () => {
        // Network failure / decode failure / 404 — keep the 16:9 fallback
        // so the slot still shows the muted background instead of
        // collapsing.
        if (!cancelled) setAspect(16 / 9);
      },
    );
    return () => {
      cancelled = true;
    };
  }, [uri]);

  return (
    <Pressable onPress={() => open(uri)}>
      <View className="rounded-lg overflow-hidden bg-muted">
        <ExpoImage
          source={{ uri }}
          style={{ width: "100%", aspectRatio: aspect ?? 16 / 9 }}
          contentFit="contain"
          transition={150}
        />
      </View>
    </Pressable>
  );
}
