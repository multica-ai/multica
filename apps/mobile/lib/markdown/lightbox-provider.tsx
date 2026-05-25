/**
 * App-level lightbox provider for tap-to-zoom image viewing.
 *
 * Single instance mounted at the root layout. `useLightbox().open(uri)`
 * displays the image fullscreen with pinch-to-zoom, double-tap, and
 * swipe-down-to-dismiss — all handled by `react-native-image-viewing`.
 *
 * V2.1 only opens single images. A future iteration could collect every
 * `![]()` URL while rendering a comment and pass the array through so
 * a left/right swipe walks the gallery.
 */
import { createContext, useContext, useState, type ReactNode } from "react";
import ImageView from "react-native-image-viewing";

interface LightboxApi {
  open: (uri: string) => void;
}

const LightboxContext = createContext<LightboxApi>({
  open: () => {
    // No-op fallback when used outside provider — markdown rendering
    // shouldn't crash if a screen forgets to mount the provider.
  },
});

export function useLightbox(): LightboxApi {
  return useContext(LightboxContext);
}

export function LightboxProvider({ children }: { children: ReactNode }) {
  const [images, setImages] = useState<{ uri: string }[]>([]);
  const open = (uri: string) => setImages([{ uri }]);
  const close = () => setImages([]);
  return (
    <LightboxContext.Provider value={{ open }}>
      {children}
      <ImageView
        images={images}
        imageIndex={0}
        visible={images.length > 0}
        onRequestClose={close}
      />
    </LightboxContext.Provider>
  );
}
