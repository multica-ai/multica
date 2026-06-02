/**
 * Chat composer — thin wrapper around the shared `<MessageComposer>` with
 * chat-specific wiring:
 *
 *   - **Controlled text**: parent (chat.tsx) owns the draft via
 *     `useChatDraftsStore` so switching sessions rehydrates the right
 *     draft. Pass `value` + `onChangeText` through.
 *   - **Stop button**: while an agent task is running for the active
 *     session, `sending` flips true and we replace the Send button slot
 *     with a Stop affordance (filled foreground bg + stop glyph). Tap →
 *     `onStop()` cancels the in-flight task.
 *   - **Mention picker mode=chat**: chat is user ↔ single agent so
 *     @member / @agent / @squad / @all are noise + would notify the
 *     wrong people. Picker route honors `?mode=chat` and surfaces only
 *     Issues (useful for "reference this ticket for context").
 *   - **No reply target**: chat is a flat conversation; passes no
 *     reply chip.
 *   - **No upload context**: chat attachments are session-scoped; the
 *     server back-fills `chat_message_id` on each row when the message
 *     persists (server-side). `MessageComposer` calls `api.uploadFile`
 *     without `{ issueId, commentId }`.
 *   - **Parent owns keyboard**: chat.tsx wraps in KeyboardAvoidingView +
 *     SafeAreaView, so `manageKeyboard={false}` prevents the composer
 *     from double-stacking its own keyboard handling.
 *
 * Previously a hand-written 400-LOC twin of inline-comment-composer.tsx;
 * now ~50 LOC plus the StopButton subcomponent.
 */
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Alert, AppState, Pressable, View } from "react-native";
import Animated, { FadeIn, FadeOut } from "react-native-reanimated";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import {
  RecordingPresets,
  requestRecordingPermissionsAsync,
  setAudioModeAsync,
  useAudioPlayer,
  useAudioPlayerStatus,
  useAudioRecorder,
  useAudioRecorderState,
} from "expo-audio";
import type { ChatMessage } from "@multica/core/types";
import { MessageComposer } from "@/components/composer/message-composer";
import { api } from "@/data/api";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import { Text } from "@/components/ui/text";
import { IconButton } from "@/components/ui/icon-button";

interface Props {
  /** Current draft text (controlled). Empty string = no draft. */
  value: string;
  /** Fired on every keystroke. The caller writes to the drafts store. */
  onChangeText: (next: string) => void;
  /** Send the serialised markdown content + the completed attachments'
   *  server ids. Caller resets the input by setting `value=""` after a
   *  successful send. */
  onSend: (content: string, attachmentIds: string[]) => Promise<void> | void;
  /** Cancel the in-flight agent task. Only callable while `sending===true`. */
  onStop: () => void;
  latestAssistantMessage?: ChatMessage | null;
  /** True while an agent task is running for the active session. The
   *  composer swaps Send for Stop. */
  sending: boolean;
  /** Hard-disable typing + send. Used when there's no usable agent in the
   *  workspace or the session is archived (legacy). */
  disabled?: boolean;
  /** When `disabled`, replaces the pill label with the reason. */
  disabledReason?: string;
}

const IS_IOS = process.env.EXPO_OS === "ios";

export function ChatComposer({
  value,
  onChangeText,
  onSend,
  onStop,
  latestAssistantMessage,
  sending,
  disabled = false,
  disabledReason,
}: Props) {
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const [voiceState, setVoiceState] = useState<
    "idle" | "recording" | "transcribing" | "sending" | "synthesizing" | "playing" | "failed"
  >("idle");
  const [voiceError, setVoiceError] = useState<string | null>(null);
  const [lastPlayedAssistantId, setLastPlayedAssistantId] = useState<string | null>(null);
  const autoPlayPendingRef = useRef(false);
  const autoPlayAfterMessageIdRef = useRef<string | null>(null);
  const recorder = useAudioRecorder(RecordingPresets.LOW_QUALITY);
  const recorderState = useAudioRecorderState(recorder, 250);
  const player = useAudioPlayer(null, { updateInterval: 250 });
  const playerStatus = useAudioPlayerStatus(player);

  const statusLabel = useMemo(() => {
    switch (voiceState) {
      case "recording":
        return `Recording ${Math.round(recorderState.durationMillis / 1000)}s`;
      case "transcribing":
        return "Transcribing…";
      case "sending":
        return "Sending transcript…";
      case "synthesizing":
        return "Preparing audio…";
      case "playing":
        return "Playing reply…";
      case "failed":
        return voiceError ?? "Voice action failed";
      default:
        return null;
    }
  }, [recorderState.durationMillis, voiceError, voiceState]);

  const onSubmit = useCallback(
    async ({
      content,
      attachmentIds,
    }: {
      content: string;
      attachmentIds: string[];
    }) => {
      // `onSend` may be sync or async; await is safe in both cases. If it
      // throws, MessageComposer's catch restores text + chips.
      await onSend(content, attachmentIds);
    },
    [onSend],
  );

  const handleStop = useCallback(() => {
    if (IS_IOS) {
      void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    }
    onStop();
  }, [onStop]);

  const startRecording = useCallback(async () => {
    if (disabled || sending || voiceState !== "idle") return;
    setVoiceError(null);
    const permission = await requestRecordingPermissionsAsync();
    if (!permission.granted) {
      setVoiceState("failed");
      setVoiceError("Microphone permission is required.");
      Alert.alert(
        "Microphone access needed",
        "Enable microphone access in Settings to send voice messages.",
      );
      return;
    }
    await setAudioModeAsync({
      allowsRecording: true,
      playsInSilentMode: true,
    });
    await recorder.prepareToRecordAsync();
    recorder.record({ forDuration: 60 });
    setVoiceState("recording");
  }, [disabled, recorder, sending, voiceState]);

  const stopRecordingAndSend = useCallback(async () => {
    if (voiceState !== "recording") return;
    try {
      await recorder.stop();
      await setAudioModeAsync({
        allowsRecording: false,
        playsInSilentMode: true,
      });
      const uri = recorder.uri;
      if (!uri) {
        throw new Error("Recording did not produce an audio file.");
      }
      setVoiceState("transcribing");
      const { transcript } = await api.transcribeSpeech({
        uri,
        name: "voice-message.m4a",
        type: "audio/m4a",
      });
      const trimmed = transcript.trim();
      if (!trimmed) {
        throw new Error("No speech was detected.");
      }
      setVoiceState("sending");
      autoPlayPendingRef.current = true;
      autoPlayAfterMessageIdRef.current = latestAssistantMessage?.id ?? null;
      await onSend(trimmed, []);
      setVoiceState("idle");
    } catch (err) {
      autoPlayPendingRef.current = false;
      autoPlayAfterMessageIdRef.current = null;
      setVoiceState("failed");
      setVoiceError(err instanceof Error ? err.message : "Voice message failed.");
    }
  }, [latestAssistantMessage?.id, onSend, recorder, voiceState]);

  const toggleRecording = useCallback(() => {
    if (voiceState === "recording") {
      void stopRecordingAndSend();
      return;
    }
    void startRecording();
  }, [startRecording, stopRecordingAndSend, voiceState]);

  const playAssistant = useCallback(
    async (message?: ChatMessage | null) => {
      const target = message ?? latestAssistantMessage;
      if (!target?.content.trim()) return;
      try {
        setVoiceError(null);
        setVoiceState("synthesizing");
        const audio = await api.synthesizeSpeech(target.content);
        player.replace({
          uri: `data:${audio.content_type};base64,${audio.audio_base64}`,
        });
        await player.seekTo(0);
        player.play();
        setLastPlayedAssistantId(target.id);
      } catch (err) {
        setVoiceState("failed");
        setVoiceError(err instanceof Error ? err.message : "Playback failed.");
      }
    },
    [latestAssistantMessage, player],
  );

  const retryVoice = useCallback(() => {
    setVoiceError(null);
    setVoiceState("idle");
  }, []);

  useEffect(() => {
    if (playerStatus.playing) {
      setVoiceState((current) =>
        current === "synthesizing" || current === "idle" ? "playing" : current,
      );
      return;
    }
    if (playerStatus.didJustFinish) {
      setVoiceState("idle");
    }
  }, [playerStatus.didJustFinish, playerStatus.playing]);

  useEffect(() => {
    if (!autoPlayPendingRef.current) return;
    if (!latestAssistantMessage) return;
    if (latestAssistantMessage.id === autoPlayAfterMessageIdRef.current) return;
    if (latestAssistantMessage.id === lastPlayedAssistantId) return;
    autoPlayPendingRef.current = false;
    autoPlayAfterMessageIdRef.current = null;
    void playAssistant(latestAssistantMessage);
  }, [latestAssistantMessage, lastPlayedAssistantId, playAssistant]);

  useEffect(() => {
    if (voiceState !== "recording") return;
    const subscription = AppState.addEventListener("change", (nextState) => {
      if (nextState === "active") return;
      void recorder.stop().catch(() => {});
      void setAudioModeAsync({
        allowsRecording: false,
        playsInSilentMode: true,
      }).catch(() => {});
      setVoiceState("failed");
      setVoiceError("Recording stopped because Multica moved to the background.");
    });
    return () => subscription.remove();
  }, [recorder, voiceState]);

  return (
    <View>
      <VoiceControls
        statusLabel={statusLabel}
        failed={voiceState === "failed"}
        recording={voiceState === "recording"}
        busy={
          voiceState === "transcribing" ||
          voiceState === "sending" ||
          voiceState === "synthesizing"
        }
        playing={voiceState === "playing"}
        disabled={disabled || sending}
        canPlay={!!latestAssistantMessage?.content.trim()}
        onRecordPress={toggleRecording}
        onPlayPress={() => void playAssistant()}
        onRetryPress={retryVoice}
      />
      <MessageComposer
        value={value}
        onChangeText={onChangeText}
        onSubmit={onSubmit}
        mentionPickerPath={{
          pathname: "/[workspace]/mention-picker",
          params: { workspace: wsSlug ?? "", mode: "chat" },
        }}
        placeholder={sending ? "Agent is working…" : "Message…"}
        pillLabel={
          sending
            ? "Agent is working…"
            : disabled
              ? (disabledReason ?? "Chat unavailable")
              : "Message…"
        }
        pillIcon="chatbubble-ellipses-outline"
        disabled={disabled}
        disabledReason={disabledReason}
        isSending={sending}
        renderStop={() => <StopButton onPress={handleStop} />}
        manageKeyboard={false}
      />
    </View>
  );
}

function VoiceControls({
  statusLabel,
  failed,
  recording,
  busy,
  playing,
  disabled,
  canPlay,
  onRecordPress,
  onPlayPress,
  onRetryPress,
}: {
  statusLabel: string | null;
  failed: boolean;
  recording: boolean;
  busy: boolean;
  playing: boolean;
  disabled: boolean;
  canPlay: boolean;
  onRecordPress: () => void;
  onPlayPress: () => void;
  onRetryPress: () => void;
}) {
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  return (
    <View className="border-t border-border bg-background px-3 pt-2">
      <View className="flex-row items-center gap-2 rounded-2xl bg-secondary px-2 py-1.5">
        <IconButton
          name={recording ? "stop" : "mic-outline"}
          iconSize={20}
          color={recording ? theme.destructive : undefined}
          onPress={onRecordPress}
          disabled={disabled || busy || playing}
          className="h-9 w-9"
          accessibilityLabel={recording ? "Stop recording" : "Record voice message"}
          accessibilityState={{ disabled: disabled || busy || playing }}
        />
        <Text
          className={`flex-1 text-sm ${failed ? "text-destructive" : "text-muted-foreground"}`}
          numberOfLines={1}
        >
          {statusLabel ?? "Voice mode"}
        </Text>
        {failed ? (
          <IconButton
            name="refresh"
            iconSize={18}
            onPress={onRetryPress}
            className="h-9 w-9"
            accessibilityLabel="Retry voice action"
          />
        ) : null}
        <IconButton
          name={playing ? "volume-high" : "play-circle-outline"}
          iconSize={20}
          onPress={onPlayPress}
          disabled={!canPlay || busy || recording}
          className="h-9 w-9"
          accessibilityLabel="Play latest assistant reply"
          accessibilityState={{ disabled: !canPlay || busy || recording }}
        />
      </View>
    </View>
  );
}

function StopButton({ onPress }: { onPress: () => void }) {
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  return (
    <Animated.View
      key="stop"
      entering={FadeIn.duration(120)}
      exiting={FadeOut.duration(120)}
    >
      <Pressable
        onPress={onPress}
        className="h-8 w-8 items-center justify-center rounded-full bg-foreground active:opacity-80"
        hitSlop={12}
        accessibilityRole="button"
        accessibilityLabel="Stop agent"
      >
        <View
          style={{
            width: 10,
            height: 10,
            backgroundColor: theme.background,
            borderRadius: 1.5,
          }}
        />
      </Pressable>
    </Animated.View>
  );
}
