/**
 * HarmonyOS port of apps/mobile/components/ui/actor-avatar.tsx. Mirrors the
 * role of packages/views/common/actor-avatar.tsx (member/agent → avatar URL
 * or initials chip), stripped down for phone use: no hover card, no nested
 * focus management.
 *
 * Deltas from the iOS file, both data-layer seams that have not been
 * ported to this vertical slice (see apps/mobile-harmony/AGENTS.md):
 *   - Directory lookup: iOS resolves a missing `avatarUrl` / display name
 *     through useActorLookup (member/agent/squad directory queries). That
 *     data layer does not exist here yet, so callers must pass `name` and
 *     `avatarUrl` explicitly; an omitted avatarUrl falls through to the
 *     initials chip. getInitials is mirrored below (pure function, same
 *     rule: first letter of each word, uppercased, max 2).
 *   - Presence: the `showPresence` overlay relied on useAgentPresence
 *     (three queries + a 30s tick). The PresenceDot component is ported;
 *     wiring it to live presence waits for use-agent-presence.
 *
 * Kept faithful: agents get a brand-tinted chip (web's "agents render with
 * distinct styling" rule); `system` actors render the settings glyph;
 * `squad` gets a soft-square tile so a group never reads as a single
 * person; `emoji:`-prefixed avatar urls render as text; only URLs that
 * actually look renderable reach <Image> (RN can crash native-side on
 * malformed sources).
 */
import { Image, StyleSheet, View } from "react-native";
import { Text } from "@/components/ui/text";
import { Icon } from "@/components/ui/icon";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

// `system` actors are server-side automation (state changes triggered by the
// platform itself, not a member or an agent). InboxItem.actor_type carries
// this third value. `squad` is a third assignee polymorph — when a squad has
// an avatar_url we render it; otherwise fall back to a generic group glyph
// so squad-assigned issues from web never render blank.
interface Props {
  type: "member" | "agent" | "system" | "squad" | null | undefined;
  id: string | null | undefined;
  /** Timeline-provided identity for actors no longer in the live directory. */
  name?: string;
  avatarUrl?: string | null;
  size?: number;
}

// Mirrors apps/mobile/data/use-actor-name.ts getInitials (pure function).
function getInitials(name: string): string {
  return name
    .split(" ")
    .map((w) => w[0])
    .filter(Boolean)
    .join("")
    .toUpperCase()
    .slice(0, 2);
}

export function ActorAvatar({
  type,
  name,
  avatarUrl,
  size = 32,
}: Props) {
  const c = useThemeColors();

  // Squad gets a soft-square tile (matches web actor-avatar.tsx which uses
  // rounded-md) so a group never reads as a single person at a glance.
  // Everyone else stays round.
  const radius = type === "squad" ? Math.round(size * 0.22) : size / 2;

  // Directory lookup is not ported: an omitted avatarUrl no longer resolves
  // through the member/agent lists (iOS behavior) — callers pass it.
  const rawUrl = avatarUrl ?? null;
  const displayName = name ?? (type === "system" ? "Multica" : "");
  const emoji = rawUrl?.startsWith("emoji:")
    ? rawUrl.slice("emoji:".length).trim() || null
    : null;
  // Only treat a URL as renderable if it actually looks like one — RN
  // <Image> can crash native-side on malformed sources (empty string, plain
  // "foo", etc.). Cheap regex; falsy / bad input falls through to the icon
  // fallback.
  const url =
    !emoji && rawUrl && /^(https?:|data:|file:|asset:)/.test(rawUrl)
      ? rawUrl
      : null;

  if (emoji) {
    return (
      <View
        style={[
          styles.center,
          { width: size, height: size, borderRadius: radius, backgroundColor: c.muted },
        ]}
      >
        <Text
          accessibilityLabel={type === "system" ? "" : displayName}
          style={{ fontSize: Math.round(size * 0.58), lineHeight: size }}
        >
          {emoji}
        </Text>
      </View>
    );
  }

  if (url) {
    return (
      <Image
        source={{ uri: url }}
        accessibilityLabel={displayName}
        style={{
          width: size,
          height: size,
          borderRadius: radius,
          backgroundColor: c.muted,
        }}
      />
    );
  }

  if (type === "system") {
    return (
      <View
        style={[
          styles.center,
          { width: size, height: size, borderRadius: radius, backgroundColor: c.muted },
        ]}
      >
        <Icon
          name="cog-outline"
          size={Math.round(size * 0.55)}
          color={c.mutedForeground}
        />
      </View>
    );
  }

  if (type === "squad") {
    return (
      <View
        style={[
          styles.center,
          { width: size, height: size, borderRadius: radius, backgroundColor: c.muted },
        ]}
      >
        <Icon
          name="people"
          size={Math.round(size * 0.55)}
          color={c.mutedForeground}
        />
      </View>
    );
  }

  const isAgent = type === "agent";
  return (
    <View
      style={[
        styles.center,
        {
          width: size,
          height: size,
          borderRadius: radius,
          backgroundColor: isAgent ? withAlpha(c.brand, 0.15) : c.muted,
        },
      ]}
    >
      <Text
        style={[
          styles.initials,
          { color: isAgent ? c.brand : c.mutedForeground },
        ]}
      >
        {getInitials(displayName)}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  center: { alignItems: "center", justifyContent: "center" },
  // text-xs font-medium
  initials: { fontSize: 12, fontWeight: "500" },
});
