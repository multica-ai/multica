import { useRef, type ReactNode } from "react";
import { ActivityIndicator, FlatList, KeyboardAvoidingView, Platform, StyleSheet, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { BottomFade } from "../../components/BottomFade";
import { Composer } from "../../components/Composer";
import { PushHeader } from "../../components/Header";
import { RunSteps } from "../../components/RunSteps";
import { Screen } from "../../components/Screen";
import { Empty, Failure, Loading } from "../../components/States";
import { Txt } from "../../components/Text";
import type { RunStep } from "../../model/steps";
import { Markdown } from "../../markdown/Markdown";
import { useStrings } from "../../strings";
import { useTheme } from "../../theme";

export type ChatItem =
  | { kind: "user"; id: string; text: string }
  | { kind: "assistant"; id: string; text: string; taskId: string | null; failed: boolean };

export interface PendingRun {
  steps: readonly RunStep[];
  hiddenSteps: number;
}

interface ChatViewProps {
  title: string;
  state: "loading" | "error" | "ready";
  items: readonly ChatItem[];
  pending: PendingRun | null;
  onBack: () => void;
  onRetry: () => void;
  onSend: (text: string) => Promise<unknown>;
  onOpenIssue: (issueId: string) => void;
  /** The finished run behind an assistant reply, collapsed until asked for. */
  renderSteps?: (taskId: string) => ReactNode;
}

function Message({ item, onOpenIssue, renderSteps }: { item: ChatItem; onOpenIssue: (id: string) => void; renderSteps?: (taskId: string) => ReactNode }) {
  const palette = useTheme();
  const strings = useStrings();
  if (item.kind === "user") {
    return (
      <View style={[styles.user, { backgroundColor: palette.fill }]}>
        <Txt variant="callout" selectable>
          {item.text}
        </Txt>
      </View>
    );
  }
  return (
    <View style={styles.assistant}>
      {item.taskId && renderSteps ? renderSteps(item.taskId) : null}
      {item.failed ? (
        <Txt variant="callout" tone="destructive">
          {strings.chat.failed}
          {item.text ? `: ${item.text}` : ""}
        </Txt>
      ) : (
        <Markdown source={item.text} size="callout" onOpenIssue={onOpenIssue} />
      )}
    </View>
  );
}

function Working({ pending }: { pending: PendingRun }) {
  const palette = useTheme();
  const strings = useStrings();
  return (
    <View style={styles.assistant}>
      {pending.steps.length ? <RunSteps steps={pending.steps} more={pending.hiddenSteps > 0 ? strings.chat.steps(pending.hiddenSteps) : undefined} /> : null}
      <View style={styles.working}>
        <ActivityIndicator size="small" color={palette.label3} />
        <Txt variant="meta" tone="secondary">
          {strings.chat.working}
        </Txt>
      </View>
    </View>
  );
}

export function ChatView({ title, state, items, pending, onBack, onRetry, onSend, onOpenIssue, renderSteps }: ChatViewProps) {
  const strings = useStrings();
  const insets = useSafeAreaInsets();
  const list = useRef<FlatList<ChatItem>>(null);

  return (
    <Screen>
      <KeyboardAvoidingView style={styles.flex} behavior={Platform.OS === "ios" ? "padding" : undefined}>
        <PushHeader title={title} onBack={onBack} backLabel={strings.common.back} />
        <View style={styles.flex}>
          {state === "loading" ? (
            <Loading />
          ) : state === "error" ? (
            <Failure title={strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={onRetry} />
          ) : (
            <FlatList contentInsetAdjustmentBehavior="automatic"
              ref={list}
              data={items}
              keyExtractor={(item) => item.id}
              renderItem={({ item }) => <Message item={item} onOpenIssue={onOpenIssue} renderSteps={renderSteps} />}
              ListFooterComponent={pending ? <Working pending={pending} /> : null}
              ListEmptyComponent={pending ? null : <Empty title={strings.agents.noMessages} />}
              contentContainerStyle={[styles.content, items.length === 0 && !pending && styles.fill]}
              onContentSizeChange={() => list.current?.scrollToEnd({ animated: false })}
              keyboardDismissMode="interactive"
            />
          )}
          <BottomFade height={insets.bottom + 80} />
          <View style={[styles.composer, { paddingBottom: Math.max(insets.bottom - 8, 12) }]}>
            <Composer placeholder={items.length ? strings.chat.followUp : strings.chat.message} sendLabel={strings.common.send} onSend={onSend} />
          </View>
        </View>
      </KeyboardAvoidingView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  flex: { flex: 1 },
  fill: { flexGrow: 1 },
  content: { paddingHorizontal: 20, paddingTop: 8, paddingBottom: 110, gap: 18 },
  user: { alignSelf: "flex-end", maxWidth: "84%", borderRadius: 18, paddingHorizontal: 14, paddingVertical: 10 },
  assistant: { gap: 10 },
  working: { flexDirection: "row", alignItems: "center", gap: 8 },
  composer: { position: "absolute", left: 16, right: 16, bottom: 0, flexDirection: "row" },
});
