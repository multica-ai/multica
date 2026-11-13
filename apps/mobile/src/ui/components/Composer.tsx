import { useState, type ReactNode } from "react";
import { Platform, Pressable, StyleSheet, TextInput, View } from "react-native";
import { Icon } from "../icons/Icon";
import { floatShadow, useTheme } from "../theme";

interface ComposerProps {
  placeholder: string;
  sendLabel: string;
  onSend: (text: string) => Promise<unknown> | void;
  /** Leading control, e.g. a round button that opens attachments. */
  leading?: ReactNode;
  disabled?: boolean;
  initialText?: string;
}

/** Floating text input; the send button appears once there is text. */
export function Composer({ placeholder, sendLabel, onSend, leading, disabled = false, initialText = "" }: ComposerProps) {
  const palette = useTheme();
  const [text, setText] = useState(initialText);
  const [sending, setSending] = useState(false);
  const canSend = text.trim().length > 0 && !sending && !disabled;

  async function send() {
    if (!canSend) return;
    setSending(true);
    try {
      await onSend(text.trim());
      setText("");
    } finally {
      setSending(false);
    }
  }

  return (
    <View style={[styles.box, { backgroundColor: palette.float }, floatShadow(palette)]}>
      {leading}
      <TextInput
        value={text}
        onChangeText={setText}
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
  );
}

const styles = StyleSheet.create({
  box: { flex: 1, minHeight: 48, borderRadius: 24, flexDirection: "row", alignItems: "flex-end", paddingHorizontal: 6, paddingVertical: 6, gap: 6 },
  input: { flex: 1, fontSize: 16, lineHeight: 21, paddingTop: 7, paddingBottom: 8, maxHeight: 120 },
  inputAlone: { paddingLeft: 10 },
  send: { width: 36, height: 36, borderRadius: 18, alignItems: "center", justifyContent: "center" },
});
