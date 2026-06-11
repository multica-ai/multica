/**
 * Mobile WorkspaceAvatar. Mirrors packages/views/workspace/workspace-avatar.tsx:
 * a resolved avatar_url renders as a rounded-square logo image; otherwise the
 * workspace's initial letter sits in a muted tile. Same fallback semantics as
 * web/desktop so a workspace looks identical across clients (apps/mobile/CLAUDE.md
 * behavioral-parity rule).
 *
 * URL resolution goes through resolveAttachmentUrl — the mobile mirror of
 * core's resolvePublicFileUrl — because avatar_url comes back as a server-
 * relative path on self-hosted backends without a CDN signer, which RN's
 * <Image> can't load without an absolute origin.
 *
 * Both branches render a `border border-border` tile and thread `className`
 * through, matching web's <img>/<span> (both carry `border` + `className`).
 * The logo sits inside an overflow-hidden View rather than styling the
 * <ExpoImage> directly: NativeWind has no cssInterop for expo-image, so
 * className/border utilities are silently dropped on <ExpoImage>. The wrapper
 * View is how the rest of the app borders/rounds an expo-image — see
 * lib/markdown/markdown-image.tsx.
 */
import { View } from "react-native";
import { Image as ExpoImage } from "expo-image";
import { Text } from "@/components/ui/text";
import { resolveAttachmentUrl } from "@/lib/attachment-url";
import { cn } from "@/lib/utils";

export function WorkspaceAvatar({
  name,
  avatarUrl,
  size = 24,
  className,
}: {
  name: string;
  avatarUrl: string | null | undefined;
  size?: number;
  className?: string;
}) {
  const resolved = resolveAttachmentUrl(avatarUrl);
  const borderRadius = Math.round(size / 4);

  if (resolved) {
    return (
      <View
        className={cn("overflow-hidden border border-border", className)}
        style={{ width: size, height: size, borderRadius }}
      >
        <ExpoImage
          source={{ uri: resolved }}
          contentFit="cover"
          accessibilityLabel={name}
          style={{ width: "100%", height: "100%" }}
        />
      </View>
    );
  }

  return (
    <View
      className={cn("items-center justify-center bg-muted border border-border", className)}
      style={{ width: size, height: size, borderRadius }}
    >
      <Text
        className="font-semibold text-muted-foreground"
        style={{ fontSize: Math.round(size * 0.48) }}
      >
        {name.charAt(0).toUpperCase()}
      </Text>
    </View>
  );
}
