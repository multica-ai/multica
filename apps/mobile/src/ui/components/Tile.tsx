import { StyleSheet, Text, View } from "react-native";
import { Icon, type IconName } from "../icons/Icon";
import { useTheme } from "../theme";
import { Avatar, type AvatarActor } from "./Avatar";

/** Rounded square holding an emoji or an icon: the mark of a project, squad or view. */
export function Tile({ emoji, icon, size = 40, tint }: { emoji?: string | null; icon?: IconName; size?: number; tint?: string }) {
  const palette = useTheme();
  return (
    <View style={[styles.center, { width: size, height: size, borderRadius: size * 0.3, backgroundColor: palette.fill }]}>
      {emoji ? <Text style={{ fontSize: size * 0.5 }}>{emoji}</Text> : icon ? <Icon name={icon} size={Math.round(size * 0.48)} color={tint ?? palette.label} /> : null}
    </View>
  );
}

/** Overlapping avatars, e.g. a squad's members. */
export function AvatarStack({ actors, size = 20 }: { actors: readonly AvatarActor[]; size?: number }) {
  const palette = useTheme();
  return (
    <View style={styles.stack}>
      {actors.map((actor, index) => (
        <View key={`${actor.name}-${index}`} style={[styles.item, { marginLeft: index ? -size * 0.3 : 0, borderRadius: size, borderColor: palette.background }]}>
          <Avatar actor={actor} size={size} />
        </View>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  center: { alignItems: "center", justifyContent: "center" },
  stack: { flexDirection: "row" },
  item: { borderWidth: 2 },
});
