import { useState } from "react";
import { ScrollView, StyleSheet, Text, TextInput, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import type { NativeStackScreenProps } from "@react-navigation/native-stack";
import { useCreateIssue } from "@multica/core/issues/mutations";
import { PRIORITY_CONFIG, PRIORITY_ORDER, STATUS_CONFIG, BOARD_STATUSES } from "@multica/core/issues/config";
import type { IssuePriority, IssueStatus } from "@multica/core/types";
import { Button, Field, Screen } from "../../components/ui/primitives";
import type { RootStackParamList } from "../../navigation/root-navigator";
import { colors, radii, spacing } from "../../theme/tokens";

type Props = NativeStackScreenProps<RootStackParamList, "CreateIssue">;

export function CreateIssueScreen({ navigation }: Props) {
  const insets = useSafeAreaInsets();
  const createIssue = useCreateIssue();
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [status, setStatus] = useState<IssueStatus>("todo");
  const [priority, setPriority] = useState<IssuePriority>("none");
  const [error, setError] = useState<string | null>(null);

  async function submit() {
    const trimmedTitle = title.trim();
    if (!trimmedTitle || createIssue.isPending) return;
    setError(null);
    try {
      const issue = await createIssue.mutateAsync({
        title: trimmedTitle,
        description: description.trim() || undefined,
        status,
        priority,
      });
      navigation.replace("IssueDetail", { issueId: issue.id });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to create issue");
    }
  }

  return (
    <Screen padded={false} safeArea={false}>
      <ScrollView
        contentContainerStyle={[
          styles.content,
          {
            paddingTop: Math.max(insets.top, spacing.lg),
            paddingBottom: Math.max(insets.bottom, spacing.lg),
          },
        ]}
      >
        <View style={styles.topBar}>
          <Button onPress={() => navigation.goBack()} variant="ghost">
            Back
          </Button>
        </View>
        <Text style={styles.title}>New issue</Text>
        <Field
          autoFocus
          onChangeText={setTitle}
          placeholder="Title"
          value={title}
        />
        <TextInput
          multiline
          onChangeText={setDescription}
          placeholder="Description"
          placeholderTextColor={colors.mutedForeground}
          style={styles.description}
          value={description}
        />
        <View style={styles.group}>
          <Text style={styles.label}>Status</Text>
          <View style={styles.optionRow}>
            {BOARD_STATUSES.map((item) => (
              <Option
                active={status === item}
                key={item}
                label={STATUS_CONFIG[item].label}
                onPress={() => setStatus(item)}
              />
            ))}
          </View>
        </View>
        <View style={styles.group}>
          <Text style={styles.label}>Priority</Text>
          <View style={styles.optionRow}>
            {PRIORITY_ORDER.map((item) => (
              <Option
                active={priority === item}
                key={item}
                label={PRIORITY_CONFIG[item].label}
                onPress={() => setPriority(item)}
              />
            ))}
          </View>
        </View>
        {error ? <Text style={styles.error}>{error}</Text> : null}
        <Button disabled={!title.trim() || createIssue.isPending} onPress={() => void submit()}>
          Create issue
        </Button>
      </ScrollView>
    </Screen>
  );
}

function Option({
  active,
  label,
  onPress,
}: {
  active: boolean;
  label: string;
  onPress: () => void;
}) {
  return (
    <Button onPress={onPress} variant={active ? "primary" : "secondary"}>
      {label}
    </Button>
  );
}

const styles = StyleSheet.create({
  content: {
    gap: spacing.md,
    padding: spacing.lg,
  },
  topBar: {
    alignItems: "flex-start",
  },
  title: {
    color: colors.foreground,
    fontSize: 20,
    fontWeight: "500",
  },
  description: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    color: colors.foreground,
    fontSize: 16,
    includeFontPadding: false,
    lineHeight: 22,
    minHeight: 128,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.md,
    textAlignVertical: "top",
  },
  group: {
    gap: spacing.sm,
  },
  label: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "500",
  },
  optionRow: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: spacing.sm,
  },
  error: {
    color: colors.destructive,
    fontSize: 14,
  },
});
