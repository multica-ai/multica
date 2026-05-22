/**
 * Overlapping avatar stack — mobile equivalent of web's
 * `packages/ui/components/ui/avatar.tsx` `AvatarGroup`. Mobile cannot import
 * the web component (sharing rules in apps/mobile/CLAUDE.md), so this is a
 * native re-implementation built on top of ActorAvatar.
 *
 * Dedupes input by `${type}:${id}` before slicing — multiple active tasks
 * from the same agent collapse to a single avatar (otherwise the stack
 * misrepresents how many distinct actors are involved).
 */
import { View } from "react-native";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { Text } from "@/components/ui/text";

export interface StackActor {
  type: "member" | "agent" | null | undefined;
  id: string | null | undefined;
}

interface Props {
  actors: StackActor[];
  /** Max distinct avatars rendered before collapsing to `+N`. Default 3. */
  max?: number;
  /** Avatar diameter in pt. Default 24 (tight enough for a header row). */
  size?: number;
}

export function AvatarStack({ actors, max = 3, size = 24 }: Props) {
  const deduped = dedupe(actors);
  const visible = deduped.slice(0, max);
  const overflow = deduped.length - visible.length;

  return (
    <View className="flex-row">
      {visible.map((actor, i) => (
        <Ring
          key={`${actor.type}:${actor.id}:${i}`}
          size={size}
          offset={i === 0 ? 0 : -size / 3}
        >
          <ActorAvatar type={actor.type} id={actor.id} size={size} />
        </Ring>
      ))}
      {overflow > 0 ? (
        <Ring size={size} offset={-size / 3}>
          <View
            style={{ width: size, height: size, borderRadius: size / 2 }}
            className="items-center justify-center bg-muted"
          >
            <Text className="text-[10px] font-medium text-muted-foreground">
              +{overflow}
            </Text>
          </View>
        </Ring>
      ) : null}
    </View>
  );
}

/** Wraps each avatar in a ring of `bg-background` so overlaps read clearly
 *  against the underlying surface. `marginLeft` does the overlap (web uses
 *  Tailwind's `-space-x-2`; RN doesn't compile that, so we set it inline). */
function Ring({
  size,
  offset,
  children,
}: {
  size: number;
  offset: number;
  children: React.ReactNode;
}) {
  return (
    <View
      style={{
        marginLeft: offset,
        width: size + 4,
        height: size + 4,
        borderRadius: (size + 4) / 2,
      }}
      className="bg-background items-center justify-center"
    >
      {children}
    </View>
  );
}

function dedupe(actors: StackActor[]): StackActor[] {
  const seen = new Set<string>();
  const out: StackActor[] = [];
  for (const a of actors) {
    const key = `${a.type ?? "none"}:${a.id ?? "none"}`;
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(a);
  }
  return out;
}
