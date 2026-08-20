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
import { createContext, use, useMemo, useState, type ReactNode } from "react";
import { View } from "react-native";
import ImageView from "react-native-image-viewing";
import { Text } from "@/components/ui/text";

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
  index: number;
}

export function LightboxProvider({ children }: { children: ReactNode }) {
  const [viewing, setViewing] = useState<Viewing | null>(null);
  // Tracked separately from `viewing.index` so the counter follows a swipe;
  // `viewing` itself is the frozen snapshot taken when the viewer opened.
  const [currentIndex, setCurrentIndex] = useState(0);

  const api = useMemo<LightboxApi>(
    () => ({
      open: (uri, sequence) => {
        const uris = sequence?.length ? [...sequence] : [uri];
        const found = uris.indexOf(uri);
        // A URI the sequence doesn't know still opens, on its own — same
        // fallback web takes when `openAt` reports the key is unknown.
        const images = found >= 0 ? uris : [uri];
        const index = found >= 0 ? found : 0;
        setCurrentIndex(index);
        setViewing({ images: images.map((u) => ({ uri: u })), index });
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
