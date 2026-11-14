import { useMemo, useState } from "react";
import { Image, Modal, Pressable, StyleSheet, Text, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Icon } from "../icons/Icon";
import { useTheme } from "../theme";
import { useMarkdownHost } from "./host";

/** Taller images are letterboxed rather than filling the whole screen. */
const TALLEST = 0.75;

/** An image in a body: full width at its own proportions; a tap shows it alone. */
export function MarkdownImage({ url, alt }: { url: string; alt: string }) {
  const host = useMarkdownHost();
  const palette = useTheme();
  const insets = useSafeAreaInsets();
  const source = useMemo(() => host.imageSource(url), [host, url]);
  const [ratio, setRatio] = useState<number | null>(null);
  const [failed, setFailed] = useState(false);
  const [open, setOpen] = useState(false);

  if (!source || failed) {
    return (
      <Text style={[styles.fallback, { color: palette.accent }]} onPress={() => host.openLink(url)} accessibilityRole="link">
        {alt || url}
      </Text>
    );
  }
  return (
    <>
      <Pressable accessibilityRole="imagebutton" accessibilityLabel={alt} onPress={() => setOpen(true)}>
        <Image
          source={source}
          accessibilityIgnoresInvertColors
          resizeMode="contain"
          onLoad={(event) => {
            const { width, height } = event.nativeEvent.source;
            if (width > 0 && height > 0) setRatio(width / height);
          }}
          onError={() => setFailed(true)}
          style={[styles.image, { aspectRatio: Math.max(ratio ?? 16 / 10, TALLEST), backgroundColor: palette.fill }]}
        />
      </Pressable>
      <Modal visible={open} transparent animationType="fade" onRequestClose={() => setOpen(false)} supportedOrientations={["portrait", "landscape"]}>
        <Pressable accessibilityRole="button" accessibilityLabel={alt} onPress={() => setOpen(false)} style={[styles.viewer, { paddingTop: insets.top, paddingBottom: insets.bottom }]}>
          <Image source={source} resizeMode="contain" accessibilityIgnoresInvertColors style={styles.full} />
          <View style={[styles.close, { top: insets.top + 12 }]}>
            <Icon name="X" size={22} color="#ffffff" />
          </View>
        </Pressable>
      </Modal>
    </>
  );
}

const styles = StyleSheet.create({
  fallback: { fontSize: 15, lineHeight: 22 },
  image: { width: "100%", borderRadius: 12 },
  viewer: { flex: 1, backgroundColor: "rgba(0,0,0,0.94)" },
  full: { flex: 1, width: "100%" },
  close: { position: "absolute", right: 20 },
});
