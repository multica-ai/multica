import { FlatList, RefreshControl, StyleSheet } from "react-native";
import { TitleHeader } from "../../components/Header";
import { IssueRow, type IssueRowModel } from "../../components/IssueRow";
import { BOTTOM_CLEARANCE, Screen } from "../../components/Screen";
import { Segments } from "../../components/Segments";
import { Empty, Failure, Loading } from "../../components/States";
import { useStrings } from "../../strings";
import { useTheme } from "../../theme";

export type MyIssuesRelation = "assigned" | "created" | "subscribed";

interface MyIssuesViewProps {
  relation: MyIssuesRelation;
  onRelationChange: (relation: MyIssuesRelation) => void;
  state: "loading" | "error" | "ready";
  issues: readonly IssueRowModel[];
  refreshing?: boolean;
  onRefresh?: () => void;
  onRetry: () => void;
  onOpen: (issue: IssueRowModel) => void;
}

export function MyIssuesView({ relation, onRelationChange, state, issues, refreshing = false, onRefresh, onRetry, onOpen }: MyIssuesViewProps) {
  const strings = useStrings();
  const palette = useTheme();
  return (
    <Screen>
      <TitleHeader title={strings.myIssues.title} />
      <Segments
        value={relation}
        onChange={onRelationChange}
        items={[
          { key: "assigned", label: strings.myIssues.assigned },
          { key: "created", label: strings.myIssues.created },
          { key: "subscribed", label: strings.myIssues.subscribed },
        ]}
      />
      {state === "loading" ? (
        <Loading />
      ) : state === "error" ? (
        <Failure title={strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={onRetry} />
      ) : (
        <FlatList
          data={issues}
          keyExtractor={(issue) => issue.id}
          renderItem={({ item }) => <IssueRow issue={item} onPress={onOpen} />}
          contentContainerStyle={[styles.list, issues.length === 0 && styles.fill]}
          ListEmptyComponent={<Empty title={strings.myIssues.empty} />}
          refreshControl={onRefresh ? <RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor={palette.label3} /> : undefined}
        />
      )}
    </Screen>
  );
}

const styles = StyleSheet.create({
  list: { paddingTop: 4, paddingBottom: BOTTOM_CLEARANCE },
  fill: { flexGrow: 1 },
});
