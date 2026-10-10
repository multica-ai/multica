/**
 * Overlapping avatar stack — HarmonyOS port of
 * apps/mobile/components/ui/avatar-stack.tsx, itself the mobile equivalent
 * of web's `packages/ui/components/ui/avatar.tsx` `AvatarGroup`. A native
 * re-implementation built on top of the ported ActorAvatar (without the
 * presence overlay, which is not wired on this platform yet).
 *
 * Dedupes input by `${type}:${id}` before slicing — multiple active tasks
 * from the same agent collapse to a single avatar (otherwise the stack
 * misrepresents how many distinct actors are involved).
 */
import { StyleSheet, View } from "react-native";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { Text } from "@/components/ui/text";
import { useThemeColors } from "@/lib/use-theme-colors";

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
  const c = useThemeColors();
  const deduped = dedupe(actors);
  const visible = deduped.slice(0, max);
  const overflow = deduped.length - visible.length;

  return (
    <View style={styles.row}>
      {visible.map((actor, i) => (
        <Ring key={`${actor.type}:${actor.id}:${i}`} size={size} offset={i === 0 ? 0 : -size / 3}>
          <ActorAvatar type={actor.type} id={actor.id} size={size} />
        </Ring>
      ))}
      {overflow > 0 ? (
        <Ring size={size} offset={-size / 3}>
          <View
            style={[
              styles.overflow,
              { width: size, height: size, borderRadius: size / 2, backgroundColor: c.muted },
            ]}
          >
            <Text style={[styles.overflowText, { color: c.mutedForeground }]}>
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
  const c = useThemeColors();
  return (
    <View
      style={[
        styles.ring,
        {
          marginLeft: offset,
          width: size + 4,
          height: size + 4,
          borderRadius: (size + 4) / 2,
          backgroundColor: c.background,
        },
      ]}
    >
      {children}
    </View>
  );
}

// text-[10px] font-medium
const styles = StyleSheet.create({
  row: { flexDirection: "row" },
  ring: { alignItems: "center", justifyContent: "center" },
  overflow: { alignItems: "center", justifyContent: "center" },
  overflowText: { fontSize: 10, fontWeight: "500" },
});

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
