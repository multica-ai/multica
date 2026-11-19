import { useMemo } from "react";
import { StyleSheet, View } from "react-native";
import { Avatar, type AvatarActor } from "../../components/Avatar";
import { DateTimeInput } from "../../components/DateTimeInput";
import { ChoiceList, SheetHeader, type Choice } from "../../components/Form";
import { Screen } from "../../components/Screen";
import { Loading } from "../../components/States";

export interface FieldChoice {
  value: string;
  label: string;
  detail?: string;
  actor?: AvatarActor;
  /** A label's color. */
  color?: string;
}

interface IssueFieldViewProps {
  title: string;
  /** Dismisses the sheet: Cancel for a single choice or a date, Done for a set like labels. */
  close: { label: string; onPress: () => void };
  /** Clears the value, for dates. */
  clear?: { label: string; onPress: () => void };
  loading?: boolean;
  list?: {
    choices: readonly FieldChoice[];
    selected: readonly string[];
    onToggle: (value: string) => void;
    searchPlaceholder?: string;
    emptyLabel?: string;
    search?: { query: string; onQueryChange: (query: string) => void };
  };
  date?: { value: Date; onChange: (date: Date) => void };
}

/** One issue property as a sheet: a list to pick from, or a calendar. */
export function IssueFieldView({ title, close, clear, loading = false, list, date }: IssueFieldViewProps) {
  const choices = useMemo<Choice<string>[]>(
    () =>
      (list?.choices ?? []).map((choice) => ({
        value: choice.value,
        label: choice.label,
        detail: choice.detail,
        leading: choice.actor ? (
          <Avatar actor={choice.actor} size={24} />
        ) : choice.color ? (
          <View style={[styles.dot, { backgroundColor: choice.color }]} />
        ) : undefined,
      })),
    [list?.choices],
  );
  return (
    <Screen>
      <SheetHeader title={title} leading={close} trailing={clear} />
      {loading ? (
        <Loading />
      ) : list ? (
        <ChoiceList
          choices={choices}
          selected={list.selected}
          onToggle={list.onToggle}
          searchPlaceholder={list.searchPlaceholder}
          emptyLabel={list.emptyLabel}
          search={list.search}
        />
      ) : date ? (
        <View style={styles.calendar}>
          <DateTimeInput mode="date" value={date.value} onChange={date.onChange} />
        </View>
      ) : null}
    </Screen>
  );
}

const styles = StyleSheet.create({
  calendar: { paddingHorizontal: 12 },
  dot: { width: 10, height: 10, borderRadius: 5, marginHorizontal: 7 },
});
