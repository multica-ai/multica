/**
 * Mobile ActorAvatar. Mirrors the role of packages/views/common/actor-avatar.tsx
 * (member/agent → avatar URL or initials chip), stripped down for phone use:
 * no hover card, no nested focus management.
 *
 * Behavioral parity rules (apps/mobile/CLAUDE.md):
 *   - Same actor type → same name → same initials. Lookup is shared via
 *     useActorLookup which reads the same MemberWithUser / Agent lists.
 *   - Agents get distinct visual treatment (brand-tinted background) to
 *     match web's "agents render with distinct styling" rule from the
 *     repo-root CLAUDE.md "Agent Assignees" section.
 *
 * Presence dot: opt-in via `showPresence`. Mirrors web's `showStatusDot`
 * (`packages/views/common/actor-avatar.tsx:51`). The prop is opt-in (default
 * false) because the dot mounts `useAgentPresence` — three queries +
 * 30s wall-clock tick — and we don't want every comment-author thumbnail
 * subscribing to that.
 */
import { Image, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useColorScheme } from "nativewind";
import { Text } from "@/components/ui/text";
import { cn } from "@/lib/utils";
import { useActorLookup, getInitials } from "@/data/use-actor-name";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useAgentPresence } from "@/lib/use-agent-presence";
import { PresenceDot } from "@/components/ui/presence-dot";
import { THEME } from "@/lib/theme";

// `system` actors are server-side automation (state changes triggered by the
// platform itself, not a member or an agent). InboxItem.actor_type carries
// this third value (packages/core/types/inbox.ts:28). `squad` is a third
// assignee polymorph (packages/core/types/issue.ts IssueAssigneeType) — when
// a squad has an avatar_url we render it; otherwise fall back to a generic
// group glyph so squad-assigned issues from web never render blank.
interface Props {
  type: "member" | "agent" | "system" | "squad" | null | undefined;
  id: string | null | undefined;
  size?: number;
  /**
   * Overlay a 3-state presence dot at the bottom-right corner. No-op for
   * non-agent actors. Opt-in to keep useAgentPresence — and its three
   * subscriptions — off thumbnails that don't need it.
   */
  showPresence?: boolean;
}

export function ActorAvatar({ type, id, size = 32, showPresence }: Props) {
  const avatar = <BareAvatar type={type} id={id} size={size} />;

  if (!showPresence || type !== "agent" || !id) {
    return avatar;
  }
  return <AgentAvatarWithPresence id={id} size={size}>{avatar}</AgentAvatarWithPresence>;
}

// Pure avatar render — no presence subscription, no workspace lookup. Kept
// separate so non-agent avatars and `showPresence=false` agent avatars do
// zero presence work.
function BareAvatar({
  type,
  id,
  size,
}: {
  type: Props["type"];
  id: Props["id"];
  size: number;
}) {
  const { getName, getAvatarUrl } = useActorLookup();
  const { colorScheme } = useColorScheme();
  // Ionicons takes a hex string, not a className — go through THEME so the
  // glyph follows light/dark instead of locking to a single hardcoded zinc.
  const iconColor =
    colorScheme === "dark"
      ? THEME.dark.mutedForeground
      : THEME.light.mutedForeground;

  // Squad gets a soft-square tile (matches web actor-avatar.tsx:42 which uses
  // rounded-md) so a group never reads as a single person at a glance.
  // Everyone else stays round.
  const radius = type === "squad" ? Math.round(size * 0.22) : size / 2;

  // URL lookup runs BEFORE the squad/system icon fallbacks so a squad with
  // an avatar_url renders its image instead of the generic group glyph.
  // Squad.avatar_url exists (packages/core/types/squad.ts) and useActorLookup
  // already returns it — the previous early-return for type==="squad" meant
  // that value was silently dropped.
  // Only treat a URL as renderable if it actually looks like one — RN <Image>
  // can crash native-side on malformed sources (empty string, plain "foo",
  // etc.). Cheap regex; falsy / bad input falls through to the icon fallback.
  const rawUrl = type && type !== "system" ? getAvatarUrl(type, id) : null;
  const emoji = rawUrl?.startsWith("emoji:")
    ? rawUrl.slice("emoji:".length).trim() || null
    : null;
  const url =
    !emoji && rawUrl && /^(https?:|data:|file:|asset:)/.test(rawUrl)
      ? rawUrl
      : null;

  if (emoji) {
    return (
      <View
        style={{ width: size, height: size, borderRadius: radius }}
        className="items-center justify-center bg-muted"
      >
        <Text
          accessibilityLabel={type === "system" ? "" : getName(type, id)}
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
        style={{ width: size, height: size, borderRadius: radius }}
        className="bg-muted"
      />
    );
  }

  if (type === "system") {
    return (
      <View
        style={{ width: size, height: size, borderRadius: radius }}
        className="items-center justify-center bg-muted"
      >
        <Ionicons name="cog" size={Math.round(size * 0.55)} color={iconColor} />
      </View>
    );
  }

  if (type === "squad") {
    return (
      <View
        style={{ width: size, height: size, borderRadius: radius }}
        className="items-center justify-center bg-muted"
      >
        <Ionicons name="people" size={Math.round(size * 0.55)} color={iconColor} />
      </View>
    );
  }

  const name = getName(type, id);
  const isAgent = type === "agent";
  return (
    <View
      style={{ width: size, height: size, borderRadius: radius }}
      className={cn(
        "items-center justify-center",
        isAgent ? "bg-brand/15" : "bg-muted",
      )}
    >
      <Text
        className={cn(
          "text-xs font-medium",
          isAgent ? "text-brand" : "text-muted-foreground",
        )}
      >
        {getInitials(name)}
      </Text>
    </View>
  );
}

// Wraps an agent avatar in a `relative` container with a corner dot. The
// dot is suppressed while presence is still loading so the avatar never
// flashes a speculative "offline" gray before the queries resolve.
function AgentAvatarWithPresence({
  id,
  size,
  children,
}: {
  id: string;
  size: number;
  children: React.ReactNode;
}) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const detail = useAgentPresence(wsId, id);
  // Match web's size threshold (packages/views/common/actor-avatar.tsx:194).
  const dotSize = size >= 24 ? 8 : 6;

  return (
    <View
      style={{ width: size, height: size }}
      className="relative"
    >
      {children}
      {detail !== "loading" && (
        <View
          style={{ position: "absolute", bottom: -1, right: -1 }}
          pointerEvents="none"
        >
          <PresenceDot availability={detail.availability} size={dotSize} />
        </View>
      )}
    </View>
  );
}
