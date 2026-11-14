import { useMemo, useState, type ReactNode } from "react";
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, TextInput, View } from "react-native";
import { Icon } from "../icons/Icon";
import { radius, type as typeRamp, useTheme } from "../theme";
import { Txt } from "./Text";

interface SheetHeaderProps {
  title: string;
  /** Left action: Cancel on a form, Back on a choice page. */
  leading: { label: string; onPress: () => void; back?: boolean };
  trailing?: { label: string; onPress: () => void; disabled?: boolean; busy?: boolean };
}

/** Header of a modal sheet: text actions either side of a centered title. */
export function SheetHeader({ title, leading, trailing }: SheetHeaderProps) {
  const palette = useTheme();
  return (
    <View style={styles.header}>
      <Pressable accessibilityRole="button" onPress={leading.onPress} hitSlop={8} style={[styles.side, styles.leading]}>
        {leading.back ? <Icon name="ChevronLeft" size={20} color={palette.label} /> : null}
        <Txt variant="callout" numberOfLines={1}>
          {leading.label}
        </Txt>
      </Pressable>
      <Txt variant="navTitle" numberOfLines={1} style={styles.title}>
        {title}
      </Txt>
      <View style={[styles.side, styles.trailing]}>
        {trailing ? (
          trailing.busy ? (
            <ActivityIndicator color={palette.label2} />
          ) : (
            <Pressable accessibilityRole="button" onPress={trailing.onPress} disabled={trailing.disabled} hitSlop={8}>
              <Txt variant="calloutStrong" style={{ opacity: trailing.disabled ? 0.35 : 1 }}>
                {trailing.label}
              </Txt>
            </Pressable>
          )
        ) : null}
      </View>
    </View>
  );
}

/** Scrolling body of a form sheet. */
export function FormBody({ children }: { children: ReactNode }) {
  return (
    <ScrollView contentContainerStyle={styles.body} keyboardShouldPersistTaps="handled" keyboardDismissMode="interactive">
      {children}
    </ScrollView>
  );
}

interface FieldProps {
  value: string;
  onChangeText: (text: string) => void;
  placeholder: string;
  multiline?: boolean;
  autoFocus?: boolean;
  maxLength?: number;
  large?: boolean;
}

/** A text input in its own grouped card. */
export function Field({ value, onChangeText, placeholder, multiline = false, autoFocus = false, maxLength, large = false }: FieldProps) {
  const palette = useTheme();
  return (
    <View style={[styles.field, { backgroundColor: palette.grouped }]}>
      <TextInput
        value={value}
        onChangeText={onChangeText}
        placeholder={placeholder}
        placeholderTextColor={palette.label3}
        multiline={multiline}
        autoFocus={autoFocus}
        maxLength={maxLength}
        style={[large ? typeRamp.bodyStrong : typeRamp.body, styles.input, multiline && styles.multiline, { color: palette.label }]}
      />
    </View>
  );
}

export interface Choice<T extends string> {
  value: T;
  label: string;
  detail?: string;
  leading?: ReactNode;
}

interface ChoiceListProps<T extends string> {
  choices: readonly Choice<T>[];
  selected: readonly T[];
  onToggle: (value: T) => void;
  searchPlaceholder?: string;
  emptyLabel?: string;
  /** Searching elsewhere, e.g. on the server: the field always shows and the choices arrive filtered. */
  search?: { query: string; onQueryChange: (query: string) => void };
}

/** Options with a check on the chosen ones; a search field once the list is long. */
export function ChoiceList<T extends string>({ choices, selected, onToggle, searchPlaceholder, emptyLabel, search }: ChoiceListProps<T>) {
  const palette = useTheme();
  const [localQuery, setLocalQuery] = useState("");
  const query = search?.query ?? localQuery;
  const setQuery = search?.onQueryChange ?? setLocalQuery;
  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (search || !q) return choices;
    return choices.filter((choice) => choice.label.toLowerCase().includes(q) || choice.detail?.toLowerCase().includes(q));
  }, [choices, query, search]);
  return (
    <ScrollView contentContainerStyle={styles.body} keyboardShouldPersistTaps="handled" keyboardDismissMode="on-drag">
      {searchPlaceholder && (search || choices.length > 8) ? (
        <View style={[styles.search, { backgroundColor: palette.fill }]}>
          <Icon name="Search" size={16} color={palette.label2} />
          <TextInput
            value={query}
            onChangeText={setQuery}
            placeholder={searchPlaceholder}
            placeholderTextColor={palette.label3}
            autoCorrect={false}
            autoCapitalize="none"
            style={[typeRamp.callout, styles.searchInput, { color: palette.label }]}
          />
        </View>
      ) : null}
      <View style={[styles.list, { backgroundColor: palette.grouped }]}>
        {shown.length === 0 && emptyLabel ? (
          <Txt variant="callout" tone="tertiary" style={styles.empty}>
            {emptyLabel}
          </Txt>
        ) : null}
        {shown.map((choice, index) => (
          <Pressable
            key={choice.value}
            accessibilityRole="button"
            accessibilityState={{ selected: selected.includes(choice.value) }}
            onPress={() => onToggle(choice.value)}
            style={({ pressed }) => [
              styles.choice,
              index < shown.length - 1 && { borderBottomColor: palette.separator, borderBottomWidth: StyleSheet.hairlineWidth },
              pressed && { opacity: 0.6 },
            ]}
          >
            {choice.leading}
            <View style={styles.flex}>
              <Txt variant="rowRegular" numberOfLines={1}>
                {choice.label}
              </Txt>
              {choice.detail ? (
                <Txt variant="meta" tone="secondary" numberOfLines={1}>
                  {choice.detail}
                </Txt>
              ) : null}
            </View>
            {selected.includes(choice.value) ? <Icon name="Check" size={18} color={palette.accent} /> : null}
          </Pressable>
        ))}
      </View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  header: { height: 56, flexDirection: "row", alignItems: "center", paddingHorizontal: 16 },
  side: { width: 88, flexDirection: "row", alignItems: "center" },
  leading: { gap: 2 },
  trailing: { justifyContent: "flex-end" },
  title: { flex: 1, textAlign: "center" },
  body: { paddingHorizontal: 16, paddingTop: 8, paddingBottom: 48, gap: 12 },
  field: { borderRadius: radius.group, paddingHorizontal: 16 },
  input: { paddingVertical: 14 },
  multiline: { minHeight: 120, textAlignVertical: "top" },
  search: { height: 38, borderRadius: 12, flexDirection: "row", alignItems: "center", gap: 8, paddingHorizontal: 12 },
  searchInput: { flex: 1, paddingVertical: 0 },
  list: { borderRadius: radius.group, paddingHorizontal: 16 },
  choice: { minHeight: 50, paddingVertical: 10, flexDirection: "row", alignItems: "center", gap: 12 },
  flex: { flex: 1, minWidth: 0 },
  empty: { paddingVertical: 16 },
});
