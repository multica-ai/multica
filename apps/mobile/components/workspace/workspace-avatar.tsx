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
      <ExpoImage
        source={{ uri: resolved }}
        contentFit="cover"
        accessibilityLabel={name}
        style={{ width: size, height: size, borderRadius }}
      />
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
