import { ActivityIndicator, FlatList, StyleSheet, TextInput, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { FloatingButton, FloatingSurface } from "../../components/Floating";
import { IssueRow, type IssueRowModel } from "../../components/IssueRow";
import { Screen } from "../../components/Screen";
import { Empty, Failure } from "../../components/States";
import { Icon } from "../../icons/Icon";
import { useStrings } from "../../strings";
import { useTheme } from "../../theme";

interface SearchViewProps {
  query: string;
  onQueryChange: (query: string) => void;
  state: "idle" | "loading" | "error" | "ready";
  results: readonly IssueRowModel[];
  onRetry: () => void;
  onOpen: (issue: IssueRowModel) => void;
  onClose: () => void;
}

export function SearchView({ query, onQueryChange, state, results, onRetry, onOpen, onClose }: SearchViewProps) {
  const strings = useStrings();
  const palette = useTheme();
  const insets = useSafeAreaInsets();
  return (
    <Screen>
      <View style={[styles.bar, { paddingTop: insets.top + 6 }]}>
        <FloatingSurface style={styles.field}>
          <Icon name="Search" size={18} color={palette.label2} />
          <TextInput
            value={query}
            onChangeText={onQueryChange}
            placeholder={strings.search.placeholder}
            placeholderTextColor={palette.label3}
            autoFocus
            autoCorrect={false}
            returnKeyType="search"
            clearButtonMode="while-editing"
            style={[styles.input, { color: palette.label }]}
          />
          {state === "loading" ? <ActivityIndicator size="small" color={palette.label3} /> : null}
        </FloatingSurface>
        <FloatingButton icon="X" label={strings.common.close} onPress={onClose} />
      </View>
      {state === "error" ? (
        <Failure title={strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={onRetry} />
      ) : (
        <FlatList
          data={results}
          keyExtractor={(issue) => issue.id}
          renderItem={({ item }) => <IssueRow issue={item} onPress={onOpen} />}
          keyboardShouldPersistTaps="handled"
          keyboardDismissMode="on-drag"
          contentContainerStyle={[styles.list, results.length === 0 && styles.fill]}
          ListEmptyComponent={state === "ready" ? <Empty title={strings.search.empty} /> : state === "idle" ? <Empty title={strings.search.hint} /> : null}
        />
      )}
    </Screen>
  );
}

const styles = StyleSheet.create({
  bar: { flexDirection: "row", alignItems: "center", gap: 10, paddingHorizontal: 16, paddingBottom: 10 },
  field: { flex: 1, height: 44, borderRadius: 22, flexDirection: "row", alignItems: "center", gap: 8, paddingHorizontal: 14 },
  input: { flex: 1, fontSize: 16, height: 44 },
  list: { paddingBottom: 40 },
  fill: { flexGrow: 1 },
});
