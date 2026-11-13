import { ActivityIndicator, FlatList, Pressable, StyleSheet, View } from "react-native";
import { Avatar, type AvatarActor } from "../../components/Avatar";
import { Screen } from "../../components/Screen";
import { Empty, Failure, Loading } from "../../components/States";
import { Txt } from "../../components/Text";
import { useStrings } from "../../strings";
import { useTheme } from "../../theme";

export interface AgentChoice {
  id: string;
  actor: AvatarActor;
  description: string;
}

interface NewChatViewProps {
  state: "loading" | "error" | "ready";
  agents: readonly AgentChoice[];
  creatingId: string | null;
  onRetry: () => void;
  onPick: (agent: AgentChoice) => void;
}

/** Sheet that starts a conversation: pick who to talk to. */
export function NewChatView({ state, agents, creatingId, onRetry, onPick }: NewChatViewProps) {
  const strings = useStrings();
  const palette = useTheme();
  return (
    <Screen>
      <Txt variant="navTitle" style={styles.title}>
        {strings.agents.pickAgent}
      </Txt>
      {state === "loading" ? (
        <Loading />
      ) : state === "error" ? (
        <Failure title={strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={onRetry} />
      ) : (
        <FlatList
          data={agents}
          keyExtractor={(agent) => agent.id}
          contentContainerStyle={[styles.list, agents.length === 0 && styles.fill]}
          ListEmptyComponent={<Empty title={strings.agents.noAgents} />}
          renderItem={({ item }) => (
            <Pressable
              accessibilityRole="button"
              disabled={creatingId !== null}
              onPress={() => onPick(item)}
              style={({ pressed }) => [styles.row, pressed && { backgroundColor: palette.fill }]}
            >
              <Avatar actor={item.actor} size={36} />
              <View style={styles.body}>
                <Txt variant="row" numberOfLines={1}>
                  {item.actor.name}
                </Txt>
                {item.description ? (
                  <Txt variant="meta" tone="secondary" numberOfLines={1} style={styles.description}>
                    {item.description}
                  </Txt>
                ) : null}
              </View>
              {creatingId === item.id ? <ActivityIndicator color={palette.label3} /> : null}
            </Pressable>
          )}
        />
      )}
    </Screen>
  );
}

const styles = StyleSheet.create({
  title: { textAlign: "center", paddingTop: 22, paddingBottom: 10 },
  list: { paddingBottom: 32 },
  fill: { flexGrow: 1 },
  row: { flexDirection: "row", alignItems: "center", gap: 14, paddingHorizontal: 20, paddingVertical: 10 },
  body: { flex: 1, minWidth: 0 },
  description: { marginTop: 1 },
});
