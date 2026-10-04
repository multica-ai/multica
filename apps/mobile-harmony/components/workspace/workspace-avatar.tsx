/**
 * HarmonyOS port of apps/mobile/components/workspace/workspace-avatar.tsx.
 * Mirrors packages/views/workspace/workspace-avatar.tsx: a resolved
 * avatar_url renders as a rounded-square logo image; otherwise the
 * workspace's initial letter sits in a muted tile. Same fallback semantics
 * as web/desktop so a workspace looks identical across clients
 * (behavioral-parity rule).
 *
 * URL resolution goes through resolveAttachmentUrl (lib/attachment-url.ts)
 * because avatar_url comes back as a server-relative path on self-hosted
 * backends without a CDN signer, which RN's <Image> can't load without an
 * absolute origin.
 *
 * expo-image is replaced by the core RN <Image> (resizeMode="cover" ==
 * contentFit="cover"). Both branches render a `border border-border` tile;
 * the logo sits inside an overflow-hidden View rather than styling the
 * image directly, which is how the rest of the app borders/rounds images.
 */
import { Image, StyleSheet, View, type StyleProp, type ViewStyle } from "react-native";
import { Text } from "@/components/ui/text";
import { resolveAttachmentUrl } from "@/lib/attachment-url";
import { useThemeColors } from "@/lib/use-theme-colors";

export function WorkspaceAvatar({
  name,
  avatarUrl,
  size = 24,
  style,
}: {
  name: string;
  avatarUrl: string | null | undefined;
  size?: number;
  style?: StyleProp<ViewStyle>;
}) {
  const c = useThemeColors();
  const resolved = resolveAttachmentUrl(avatarUrl);
  const borderRadius = Math.round(size / 4);

  if (resolved) {
    return (
      <View
        style={[
          styles.tile,
          { width: size, height: size, borderRadius, borderColor: c.border },
          style,
        ]}
      >
        <Image
          source={{ uri: resolved }}
          resizeMode="cover"
          accessibilityLabel={name}
          style={styles.image}
        />
      </View>
    );
  }

  return (
    <View
      style={[
        styles.tile,
        styles.fallback,
        {
          width: size,
          height: size,
          borderRadius,
          borderColor: c.border,
          backgroundColor: c.muted,
        },
        style,
      ]}
    >
      <Text
        style={[
          styles.initial,
          { fontSize: Math.round(size * 0.48), color: c.mutedForeground },
        ]}
      >
        {name.charAt(0).toUpperCase()}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  tile: {
    overflow: "hidden",
    borderWidth: 1,
  },
  fallback: { alignItems: "center", justifyContent: "center" },
  image: { width: "100%", height: "100%" },
  // font-semibold
  initial: { fontWeight: "600" },
});
