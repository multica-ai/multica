import { RefreshControl, ScrollView, StyleSheet, View } from "react-native";
import { Chip } from "../../components/Chip";
import { PushHeader } from "../../components/Header";
import { IssueGroups, type IssueGroupModel } from "../../components/IssueGroups";
import type { IssueRowModel } from "../../components/IssueRow";
import { Screen } from "../../components/Screen";
import { Empty, Failure, Loading } from "../../components/States";
import { Txt } from "../../components/Text";
import { Icon, type IconName } from "../../icons/Icon";
import { useStrings } from "../../strings";
import { useTheme } from "../../theme";

export interface FilterChipModel {
  icon: IconName;
  label: string;
}

interface SavedViewViewProps {
  state: "loading" | "error" | "missing" | "ready";
  title: string;
  subtitle: string;
  filters: readonly FilterChipModel[];
  groups: readonly IssueGroupModel[];
  starred: boolean;
  refreshing?: boolean;
  onRefresh?: () => void;
  onToggleStar: () => void;
  onBack: () => void;
  onRetry: () => void;
  onOpen: (issue: IssueRowModel) => void;
}

export function SavedViewView({ state, title, subtitle, filters, groups, starred, refreshing = false, onRefresh, onToggleStar, onBack, onRetry, onOpen }: SavedViewViewProps) {
  const strings = useStrings();
  const palette = useTheme();
  return (
    <Screen>
      <PushHeader
        onBack={onBack}
        backLabel={strings.common.back}
        actions={state === "missing" ? [] : [{ icon: "Star", label: starred ? strings.views.unstar : strings.views.star, onPress: onToggleStar, filled: starred }]}
      />
      {state === "loading" ? (
        <Loading />
      ) : state === "error" ? (
        <Failure title={strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={onRetry} />
      ) : state === "missing" ? (
        <Empty title={strings.views.missing} />
      ) : (
        <ScrollView contentInsetAdjustmentBehavior="automatic"
          contentContainerStyle={styles.content}
          refreshControl={onRefresh ? <RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor={palette.label3} /> : undefined}
        >
          <View style={styles.head}>
            <View style={styles.titleRow}>
              <Txt variant="title" numberOfLines={2} style={styles.flex}>
                {title}
              </Txt>
              {starred ? <Icon name="Star" size={18} color={palette.warning} /> : null}
            </View>
            <Txt variant="meta" tone="secondary" style={styles.subtitle}>
              {subtitle}
            </Txt>
          </View>
          {filters.length > 0 ? (
            <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.filterBar} contentContainerStyle={styles.filters}>
              {filters.map((filter) => (
                <Chip key={`${filter.icon}-${filter.label}`} icon={<Icon name={filter.icon} size={14} color={palette.label2} />} label={filter.label} />
              ))}
            </ScrollView>
          ) : (
            <View style={styles.gap} />
          )}
          {groups.length === 0 ? <Empty title={strings.views.empty} /> : <IssueGroups groups={groups} finishedLabel={strings.project.finished} onOpen={onOpen} />}
        </ScrollView>
      )}
    </Screen>
  );
}

const styles = StyleSheet.create({
  flex: { flex: 1 },
  content: { paddingBottom: 60, flexGrow: 1 },
  head: { paddingHorizontal: 20, paddingTop: 4 },
  titleRow: { flexDirection: "row", alignItems: "center", gap: 8 },
  subtitle: { marginTop: 2 },
  filterBar: { flexGrow: 0 },
  filters: { paddingHorizontal: 20, paddingVertical: 14, gap: 6 },
  gap: { height: 8 },
});
