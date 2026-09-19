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
  /** Timeline-provided identity for actors no longer in the live directory. */
  name?: string;
  avatarUrl?: string | null;
  size?: number;
  /**
   * Overlay a 3-state presence dot at the bottom-right corner. No-op for
   * non-agent actors. Opt-in to keep useAgentPresence — and its three
   * subscriptions — off thumbnails that don't need it.
   */
  showPresence?: boolean;
}

export function ActorAvatar({
  type,
  id,
  name,
  avatarUrl,
  size = 32,
  showPresence,
}: Props) {
  const avatar = (
    <BareAvatar
      type={type}
      id={id}
      name={name}
      avatarUrl={avatarUrl}
      size={size}
    />
  );

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
  name,
  avatarUrl,
  size,
}: {
  type: Props["type"];
  id: Props["id"];
  name: Props["name"];
  avatarUrl: Props["avatarUrl"];
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
  const rawUrl = avatarUrl === undefined
    ? type && type !== "system"
      ? getAvatarUrl(type, id)
      : null
    : avatarUrl;
  const displayName =
    name ?? (type === "system" ? "Multica" : getName(type, id));
  const brand = parseBrandAvatar(rawUrl);
  const emoji =
    !brand && rawUrl?.startsWith("emoji:")
      ? rawUrl.slice("emoji:".length).trim() || null
      : null;
  const url =
    !emoji && !brand && rawUrl && /^(https?:|data:|file:|asset:)/.test(rawUrl)
      ? rawUrl
      : null;

  if (brand) {
    const ringWidth = Math.max(2, Math.round(size * 0.06));
    return (
      <View
        accessibilityLabel={displayName}
        style={{
          width: size,
          height: size,
          borderRadius: radius,
          backgroundColor: brand.bg,
          borderWidth: brand.ring ? ringWidth : 0,
          borderColor: brand.ring ?? undefined,
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        <Text
          style={{
            color: brand.fg,
            fontSize: Math.round(size * 0.42),
            fontWeight: "600",
          }}
        >
          {brand.letter}
        </Text>
      </View>
    );
  }

  if (emoji) {
    return (
      <View
        style={{ width: size, height: size, borderRadius: radius }}
        className="items-center justify-center bg-muted"
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
        {getInitials(displayName)}
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

// Keep in lockstep with `packages/ui/lib/avatar-brand.ts`. Mobile cannot
// import `@multica/ui`, so the marker parser and tile colors live here as
// the same `brand:<id>[/<ring>]` contract the web picker persists.
const BRAND_FACE: Record<string, { bg: string; fg: string; letter: string }> = {
  grok: { bg: "#0A0A0A", fg: "#F4F4F5", letter: "G" },
  gpt: { bg: "#10A37F", fg: "#FFFFFF", letter: "G" },
  claude: { bg: "#D97757", fg: "#FFFFFF", letter: "C" },
  gemini: { bg: "#1A73E8", fg: "#FFFFFF", letter: "G" },
  deepseek: { bg: "#4D6BFE", fg: "#FFFFFF", letter: "D" },
  kimi: { bg: "#1F1147", fg: "#FFFFFF", letter: "K" },
  glm: { bg: "#1A56DB", fg: "#FFFFFF", letter: "Z" },
  qwen: { bg: "#6A3DE8", fg: "#FFFFFF", letter: "Q" },
  llama: { bg: "#12101A", fg: "#ED9D3C", letter: "L" },
  mistral: { bg: "#FA520F", fg: "#FFFFFF", letter: "M" },
  devin: { bg: "#0B1220", fg: "#5EEAD4", letter: "D" },
  cursor: { bg: "#0A0A0A", fg: "#F4F4F5", letter: "C" },
  copilot: { bg: "#0D1117", fg: "#F0F6FC", letter: "C" },
  opencode: { bg: "#3F3F46", fg: "#E4E4E7", letter: "O" },
};

const BRAND_RING: Record<string, string> = {
  flagship: "#22D3EE",
  standard: "#A78BFA",
  fast: "#34D399",
};

function parseBrandAvatar(raw: string | null | undefined): {
  bg: string;
  fg: string;
  letter: string;
  ring: string | null;
} | null {
  if (!raw?.startsWith("brand:")) return null;
  const rest = raw.slice("brand:".length).trim();
  if (!rest) return null;
  const slash = rest.indexOf("/");
  const id = slash === -1 ? rest : rest.slice(0, slash);
  const ringRaw = slash === -1 ? "" : rest.slice(slash + 1);
  const face = BRAND_FACE[id];
  if (!face) return null;
  return { ...face, ring: ringRaw ? (BRAND_RING[ringRaw] ?? null) : null };
}
