import type { ReactNode } from "react";
import { Image, StyleSheet, Text, View } from "react-native";
import { Icon } from "../icons/Icon";
import { useTheme } from "../theme";

export interface AvatarActor {
  kind: "member" | "agent" | "system";
  name: string;
  avatarUrl?: string | null;
}

function initials(name: string) {
  const words = name.trim().split(/\s+/).filter(Boolean);
  if (words.length === 0) return "?";
  const first = words[0]!;
  if (/[\u3400-\u9fff]/.test(first)) return first.slice(0, 1);
  return words.length > 1 ? (first[0]! + words[1]![0]!).toUpperCase() : first.slice(0, 2).toUpperCase();
}

function hue(name: string, count: number) {
  let hash = 0;
  for (const ch of name) hash = (hash * 31 + ch.charCodeAt(0)) | 0;
  return Math.abs(hash) % count;
}

interface AvatarProps {
  actor: AvatarActor;
  size?: number;
  /** Small glyph at the bottom right: what happened, in an inbox row. */
  badge?: ReactNode;
  running?: boolean;
}

export function Avatar({ actor, size = 36, badge, running = false }: AvatarProps) {
  const palette = useTheme();
  const round = { width: size, height: size, borderRadius: size / 2 };
  let face: ReactNode;
  if (actor.avatarUrl) {
    face = <Image source={{ uri: actor.avatarUrl }} style={[round, { backgroundColor: palette.fill }]} />;
  } else if (actor.kind === "member") {
    face = (
      <View style={[round, styles.center, { backgroundColor: palette.avatars[hue(actor.name, palette.avatars.length)] }]}>
        <Text style={[styles.initials, { fontSize: Math.round(size * 0.36) }]}>{initials(actor.name)}</Text>
      </View>
    );
  } else {
    face = (
      <View style={[round, styles.center, { backgroundColor: palette.fillStrong }]}>
        <Icon name="Bot" size={Math.round(size * 0.5)} color={palette.label} />
      </View>
    );
  }
  return (
    <View style={round} accessibilityLabel={actor.name}>
      {face}
      {badge ? <View style={[styles.badge, { backgroundColor: palette.background }]}>{badge}</View> : null}
      {running ? (
        <View
          style={[
            styles.run,
            { width: Math.max(8, size * 0.28), height: Math.max(8, size * 0.28), backgroundColor: palette.warning, borderColor: palette.background },
          ]}
        />
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  center: { alignItems: "center", justifyContent: "center" },
  initials: { color: "#ffffff", fontWeight: "600" },
  badge: { position: "absolute", right: -3, bottom: -3, width: 18, height: 18, borderRadius: 9, alignItems: "center", justifyContent: "center" },
  run: { position: "absolute", right: -1, bottom: -1, borderRadius: 6, borderWidth: 2 },
});
