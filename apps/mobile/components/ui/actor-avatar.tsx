import { Image, Text, View } from "react-native";
import { useActorName } from "@multica/core/workspace/hooks";

interface Props {
  type: "member" | "agent" | null;
  id: string | null;
  size?: number;
}

// Mobile mirror of @multica/views/common/actor-avatar.
// Renders avatar URL when present; falls back to initials chip.
// Agent uses a brand tint (bg-brand/15 + text-brand) — solid bg-brand on a
// 28×28 surface violates the token usage policy (see tokens.css: solid
// reserved for ≤8px elements). System / null types fall back to a neutral
// muted chip.
export function ActorAvatar({ type, id, size = 28 }: Props) {
  const { getActorInitials, getActorAvatarUrl } = useActorName();

  const dim = {
    width: size,
    height: size,
    borderRadius: size / 2,
  };

  if (!type || !id) {
    return (
      <View
        style={dim}
        className="bg-muted items-center justify-center"
      />
    );
  }

  const avatarUrl = getActorAvatarUrl(type, id);
  if (avatarUrl) {
    return <Image source={{ uri: avatarUrl }} style={dim} />;
  }

  const initials = getActorInitials(type, id) || "?";
  const isAgent = type === "agent";

  return (
    <View
      style={dim}
      className={
        isAgent
          ? "bg-brand/15 items-center justify-center"
          : "bg-muted items-center justify-center"
      }
    >
      <Text
        className={
          isAgent
            ? "text-brand text-xs font-semibold"
            : "text-muted-foreground text-xs font-semibold"
        }
      >
        {initials}
      </Text>
    </View>
  );
}
