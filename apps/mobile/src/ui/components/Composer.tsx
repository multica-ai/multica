import { useEffect, useRef, useState, type ReactNode } from "react";
import { Platform, Pressable, StyleSheet, TextInput, View } from "react-native";
import { Icon } from "../icons/Icon";
import { activeMention, insertMention, matchMentions, withMentionLinks, type MentionCandidate, type MentionTarget } from "../model/mentions";
import { floatShadow, useTheme } from "../theme";
import { Avatar } from "./Avatar";
import { Txt } from "./Text";

interface ComposerProps {
  placeholder: string;
  sendLabel: string;
  onSend: (text: string) => Promise<unknown> | void;
  /** Leading control, e.g. a round button that opens attachments. */
  leading?: ReactNode;
  disabled?: boolean;
  initialText?: string;
  /** Offered after an `@`; chosen ones are sent as mention links. `onQuery` hears what is typed after it. */
  mentions?: { candidates: readonly MentionCandidate[]; onQuery?: (query: string | null) => void };
  /** A line above the field, such as who is being replied to, with a way to drop it. */
  context?: { label: string; cancelLabel: string; onCancel: () => void };
  /** Text to start over from, such as a comment being edited; applied, and focused, when `key` changes. */
  draft?: { key: string; text: string; targets: readonly MentionTarget[] } | null;
  /** Focuses the field whenever it changes. */
  focusKey?: string | null;
}

/** Floating text input; the send button appears once there is text. */
export function Composer({ placeholder, sendLabel, onSend, leading, disabled = false, initialText = "", mentions, context, draft, focusKey }: ComposerProps) {
  const palette = useTheme();
  const input = useRef<TextInput>(null);
  const [text, setText] = useState(initialText);
  const [targets, setTargets] = useState<readonly MentionTarget[]>([]);
  const [cursor, setCursor] = useState(initialText.length);
  const [selection, setSelection] = useState<{ start: number; end: number } | undefined>();
  const [sending, setSending] = useState(false);
  const canSend = text.trim().length > 0 && !sending && !disabled;

  const draftKey = draft?.key ?? null;
  const [appliedDraft, setAppliedDraft] = useState<string | null>(null);
  if (draft && draftKey !== appliedDraft) {
    setAppliedDraft(draftKey);
    setText(draft.text);
    setTargets(draft.targets);
    setCursor(draft.text.length);
  } else if (!draft && appliedDraft !== null) {
    setAppliedDraft(null);
  }

  useEffect(() => {
    if (focusKey || draftKey) input.current?.focus();
  }, [focusKey, draftKey]);

  const active = mentions ? activeMention(text, cursor) : null;
  const query = active?.query ?? null;
  const onQuery = mentions?.onQuery;
  useEffect(() => {
    onQuery?.(query);
  }, [onQuery, query]);
  const suggestions = active && mentions ? matchMentions(mentions.candidates, active.query) : [];

  function choose(target: MentionCandidate) {
    if (!active) return;
    const next = insertMention(text, active.start, cursor, target);
    setText(next.text);
    setCursor(next.cursor);
    setSelection({ start: next.cursor, end: next.cursor });
    setTargets((current) => [...current, { type: target.type, id: target.id, label: target.label }]);
  }

  async function send() {
    if (!canSend) return;
    setSending(true);
    try {
      await onSend(withMentionLinks(text.trim(), targets));
      setText("");
      setTargets([]);
      setCursor(0);
    } finally {
      setSending(false);
    }
  }

  return (
    <View style={styles.column}>
      {suggestions.length ? (
        <View style={[styles.suggestions, { backgroundColor: palette.float }, floatShadow(palette)]}>
          {suggestions.map((candidate) => (
            <Pressable
              key={`${candidate.type}:${candidate.id}`}
              accessibilityRole="button"
              onPress={() => choose(candidate)}
              style={({ pressed }) => [styles.suggestion, pressed && { backgroundColor: palette.fill }]}
            >
              {candidate.actor ? <Avatar actor={candidate.actor} size={24} /> : <Icon name="Hash" size={18} color={palette.label2} />}
              <Txt variant="callout" numberOfLines={1} style={styles.flexShrink}>
                {candidate.label}
              </Txt>
              {candidate.detail ? (
                <Txt variant="meta" tone="tertiary" numberOfLines={1} style={styles.flex}>
                  {candidate.detail}
                </Txt>
              ) : null}
            </Pressable>
          ))}
        </View>
      ) : null}
      <View style={[styles.box, { backgroundColor: palette.float }, floatShadow(palette)]}>
        {context ? (
          <View style={[styles.context, { borderBottomColor: palette.separator }]}>
            <Txt variant="meta" tone="secondary" numberOfLines={1} style={styles.flex}>
              {context.label}
            </Txt>
            <Pressable accessibilityRole="button" accessibilityLabel={context.cancelLabel} onPress={context.onCancel} hitSlop={10}>
              <Icon name="X" size={16} color={palette.label2} />
            </Pressable>
          </View>
        ) : null}
        <View style={styles.row}>
          {leading}
          <TextInput
            ref={input}
            value={text}
            onChangeText={setText}
            selection={selection}
            onSelectionChange={(event) => {
              setCursor(event.nativeEvent.selection.end);
              if (selection) setSelection(undefined);
            }}
            placeholder={placeholder}
            placeholderTextColor={palette.label3}
            editable={!disabled}
            multiline
            // react-native-web otherwise renders a two-row textarea; on iOS the field grows from one line.
            numberOfLines={Platform.OS === "web" ? 1 : undefined}
            style={[styles.input, { color: palette.label }, !leading && styles.inputAlone]}
          />
          {canSend || sending ? (
            <Pressable
              accessibilityRole="button"
              accessibilityLabel={sendLabel}
              onPress={send}
              disabled={!canSend}
              style={[styles.send, { backgroundColor: palette.accent, opacity: sending ? 0.5 : 1 }]}
            >
              <Icon name="ArrowUp" size={18} color={palette.onAccent} strokeWidth={2.4} />
            </Pressable>
          ) : null}
        </View>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  column: { flex: 1, gap: 8 },
  suggestions: { borderRadius: 18, padding: 6 },
  suggestion: { minHeight: 44, flexDirection: "row", alignItems: "center", gap: 10, paddingHorizontal: 8, borderRadius: 12 },
  box: { minHeight: 48, borderRadius: 24 },
  context: { flexDirection: "row", alignItems: "center", gap: 8, marginHorizontal: 16, paddingTop: 10, paddingBottom: 8, borderBottomWidth: StyleSheet.hairlineWidth },
  row: { flexDirection: "row", alignItems: "flex-end", paddingHorizontal: 6, paddingVertical: 6, gap: 6 },
  input: { flex: 1, fontSize: 16, lineHeight: 21, paddingTop: 7, paddingBottom: 8, maxHeight: 120 },
  inputAlone: { paddingLeft: 10 },
  send: { width: 36, height: 36, borderRadius: 18, alignItems: "center", justifyContent: "center" },
  flex: { flex: 1 },
  flexShrink: { flexShrink: 1 },
});
