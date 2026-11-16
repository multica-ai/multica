/**
 * App-level lightbox provider for tap-to-zoom image viewing.
 *
 * Single instance mounted at the root layout. `useLightbox().open(uri)`
 * displays the image fullscreen with pinch-to-zoom, double-tap, and
 * swipe-down-to-dismiss — all handled by `react-native-image-viewing`.
 *
 * `open(uri, sequence)` opens the same viewer positioned inside a whole
 * screen's images (MUL-5752), so a horizontal swipe walks to the next one
 * and a "3 / 7" counter says where the reader is. `ImageSequenceProvider`
 * builds that array; screens that don't mount one keep passing a single URI
 * and get the previous single-image behaviour.
 *
 * Parity with web/desktop, all from the same product brief:
 *   - images only, never mixed with other attachment kinds;
 *   - the sequence is frozen when the viewer opens, so an arriving comment
 *     cannot shift the index under the reader;
 *   - boundaries stop rather than wrap. `react-native-image-viewing` pages a
 *     fixed array and does not loop, so this comes for free.
 *
 * The gesture is the platform's own horizontal paging rather than the
 * desktop chevrons — same semantics, phone-native interaction.
 */
import { createContext, use, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { AppState, View } from "react-native";
import ImageView from "react-native-image-viewing";
import { Text } from "@/components/ui/text";
import { freshAttachmentUrl } from "@/data/attachment-media";
import { registerSessionConnection } from "@/data/realtime/session-connections";
import { currentSessionEpoch, sessionEpochChanged } from "@/data/session-epoch";

interface LightboxApi {
  /**
   * Show `uri` fullscreen. When `sequence` contains it, the viewer opens at
   * its real position and the reader can swipe through the rest; otherwise
   * the viewer shows that one image.
   */
  open: (uri: string, sequence?: readonly string[]) => void;
}

const LightboxContext = createContext<LightboxApi>({
  open: () => {
    // No-op fallback when used outside provider — markdown rendering
    // shouldn't crash if a screen forgets to mount the provider.
  },
});

export function useLightbox(): LightboxApi {
  return use(LightboxContext);
}

interface Viewing {
  images: { uri: string }[];
  sources: string[];
  index: number;
  epoch: number;
}

export function LightboxProvider({ children }: { children: ReactNode }) {
  const [viewing, setViewing] = useState<Viewing | null>(null);
  // Tracked separately from `viewing.index` so the counter follows a swipe;
  // `viewing` itself is the frozen snapshot taken when the viewer opened.
  const [currentIndex, setCurrentIndex] = useState(0);
  const openSequence = useRef(0);

  useEffect(() => registerSessionConnection(() => {
    openSequence.current++;
    setViewing(null);
  }), [viewing]);

  useEffect(() => {
    if (!viewing) return;
    let cancelled = false;
    const controller = new AbortController();
    const refresh = () => {
      void freshAttachmentUrl(viewing.sources[currentIndex], false, controller.signal)
        .then((uri) => {
          if (cancelled || sessionEpochChanged(viewing.epoch)) return;
          setViewing((current) => {
            if (!current || current.images[currentIndex]?.uri === uri) return current;
            const images = [...current.images];
            images[currentIndex] = { uri };
            return { ...current, images };
          });
        }).catch(() => {});
    };
    // Keep the selected image's 60-second proxy capability usable while the
    // viewer is open, on swipes and on return from the background.
    refresh();
    const timer = setInterval(refresh, 45_000);
    const sub = AppState.addEventListener("change", (state) => {
      if (state === "active") refresh();
    });
    return () => { cancelled = true; controller.abort(); clearInterval(timer); sub.remove(); };
  }, [viewing?.sources, viewing?.epoch, currentIndex]);

  const api = useMemo<LightboxApi>(
    () => ({
      open: (uri, sequence) => {
        const epoch = currentSessionEpoch();
        const attempt = ++openSequence.current;
        const uris = sequence?.length ? [...sequence] : [uri];
        const found = uris.indexOf(uri);
        // A URI the sequence doesn't know still opens, on its own — same
        // fallback web takes when `openAt` reports the key is unknown.
        const sources = found >= 0 ? uris : [uri];
        const index = found >= 0 ? found : 0;
        void Promise.all(sources.map((source) => freshAttachmentUrl(source))).then((urls) => {
          if (attempt !== openSequence.current || sessionEpochChanged(epoch)) return;
          setCurrentIndex(index);
          setViewing({ images: urls.map((u) => ({ uri: u })), sources, index, epoch });
        }).catch(() => {});
      },
    }),
    [],
  );

  const total = viewing?.images.length ?? 0;
  // Only a sequence gets a counter — a lone image has nothing to count.
  const Footer = useMemo(
    () =>
      total > 1
        ? function LightboxCounter() {
            return (
              <View className="items-center pb-10">
                <Text className="text-sm text-white">
                  {currentIndex + 1} / {total}
                </Text>
              </View>
            );
          }
        : undefined,
    [currentIndex, total],
  );

  return (
    <LightboxContext.Provider value={api}>
      {children}
      <ImageView
        images={viewing?.images ?? []}
        imageIndex={viewing?.index ?? 0}
        visible={!!viewing}
        onRequestClose={() => setViewing(null)}
        onImageIndexChange={setCurrentIndex}
        FooterComponent={Footer}
      />
    </LightboxContext.Provider>
  );
}
