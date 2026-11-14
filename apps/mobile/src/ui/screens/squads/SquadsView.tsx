import { FlatList, Pressable, RefreshControl, StyleSheet, View } from "react-native";
import type { AvatarActor } from "../../components/Avatar";
import { TitleHeader } from "../../components/Header";
import { BOTTOM_CLEARANCE, Screen } from "../../components/Screen";
import { Empty, Failure, Loading } from "../../components/States";
import { Txt } from "../../components/Text";
import { AvatarStack, Tile } from "../../components/Tile";
import { useStrings } from "../../strings";
import { useTheme } from "../../theme";

export interface SquadRowModel {
  id: string;
  name: string;
  description: string;
  leader: AvatarActor;
  members: readonly AvatarActor[];
  memberCount: number;
}

function SquadRow({ squad, onPress }: { squad: SquadRowModel; onPress: () => void }) {
  const palette = useTheme();
  const strings = useStrings();
  return (
    <Pressable accessibilityRole="button" onPress={onPress} style={({ pressed }) => [styles.row, pressed && { backgroundColor: palette.fill }]}>
      <Tile icon="Users" />
      <View style={styles.flex}>
        <Txt variant="row" numberOfLines={1}>
          {squad.name}
        </Txt>
        {squad.description ? (
          <Txt variant="meta" tone="secondary" numberOfLines={1} style={styles.meta}>
            {squad.description}
          </Txt>
        ) : null}
        <View style={styles.foot}>
          <AvatarStack actors={squad.members} size={18} />
          <Txt variant="meta" tone="tertiary" numberOfLines={1} style={styles.flex}>
            {strings.squad.memberCount(squad.memberCount)} · {strings.squad.leader} {squad.leader.name}
          </Txt>
        </View>
      </View>
    </Pressable>
  );
}

interface SquadsViewProps {
  state: "loading" | "error" | "ready";
  squads: readonly SquadRowModel[];
  refreshing?: boolean;
  onRefresh?: () => void;
  onRetry: () => void;
  onOpen: (squadId: string) => void;
  onCreate?: () => void;
}

export function SquadsView({ state, squads, refreshing = false, onRefresh, onRetry, onOpen, onCreate }: SquadsViewProps) {
  const strings = useStrings();
  const palette = useTheme();
  return (
    <Screen>
      <TitleHeader title={strings.squad.title} actions={onCreate ? [{ icon: "Plus", label: strings.squad.new, onPress: onCreate }] : []} />
      {state === "loading" ? (
        <Loading />
      ) : state === "error" ? (
        <Failure title={strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={onRetry} />
      ) : (
        <FlatList contentInsetAdjustmentBehavior="automatic"
          data={squads}
          keyExtractor={(squad) => squad.id}
          renderItem={({ item }) => <SquadRow squad={item} onPress={() => onOpen(item.id)} />}
          contentContainerStyle={[styles.list, squads.length === 0 && styles.fill]}
          ListEmptyComponent={<Empty title={strings.squad.empty} detail={strings.squad.emptyDetail} />}
          refreshControl={onRefresh ? <RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor={palette.label3} /> : undefined}
        />
      )}
    </Screen>
  );
}

const styles = StyleSheet.create({
  flex: { flex: 1, minWidth: 0 },
  list: { paddingTop: 4, paddingBottom: BOTTOM_CLEARANCE },
  fill: { flexGrow: 1 },
  row: { flexDirection: "row", alignItems: "flex-start", gap: 12, paddingHorizontal: 20, paddingVertical: 11 },
  meta: { marginTop: 2 },
  foot: { flexDirection: "row", alignItems: "center", gap: 8, marginTop: 8 },
});
