/**
 * Comment-reminder picker route (FIR-2644 · Del 3 — mobile).
 *
 * A formSheet presented from the comment context menu ("Remind me…"). It lets
 * the user:
 *   - edit the reminder text, pre-filled with the same snippet the server would
 *     auto-suggest (lib/suggest-reminder-text mirrors the Go suggestReminderText),
 *   - tap a quick preset (in 1 hour / tomorrow 9:00), or
 *   - pick a specific date + time on the native wheel.
 *
 * On commit it creates a personal reminder pinned to the comment via the
 * cerebro backend (POST /api/inbox/reminders with comment_id, FIR-2641). When
 * that reminder later fires, tapping it in the inbox deep-links straight to the
 * comment — that flow already exists (the inbox row carries details.comment_id,
 * see (tabs)/inbox.tsx → highlight param, and components/issue/timeline-list.tsx
 * scrolls + flashes the matching comment). This route only creates the reminder.
 *
 * iOS-native-first (apps/mobile/CLAUDE.md UI waterfall): the date+time control
 * is @react-native-community/datetimepicker — the same native picker the
 * due-date sheet uses, here in `datetime` mode so the user spins date and time
 * together. Text/presets/commit are inline-composed (AutosizeTextArea +
 * Pressable + Button); no new generic primitive for a single caller.
 *
 * Self-contained per the formSheet rule: it derives everything from route
 * params (commentId, issueId, the suggested text) and calls its own mutation —
 * no callbacks back up to the issue screen.
 */
import { useMemo, useRef, useState } from "react";
import { Alert, Pressable, ScrollView, View } from "react-native";
import { useLocalSearchParams, router } from "expo-router";
import DateTimePicker from "@react-native-community/datetimepicker";
import * as Haptics from "expo-haptics";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { AutosizeTextArea } from "@/components/ui/autosize-textarea";
import { useCreateCommentReminder } from "@/data/mutations/reminders";
import { ApiError } from "@/data/api";

function addMinutes(base: Date, minutes: number): Date {
  return new Date(base.getTime() + minutes * 60_000);
}

/** Tomorrow at the given local hour, minutes/seconds zeroed. */
function tomorrowAt(hour: number): Date {
  const d = new Date();
  d.setDate(d.getDate() + 1);
  d.setHours(hour, 0, 0, 0);
  return d;
}

export default function CommentReminderRoute() {
  const { id, commentId, suggested } = useLocalSearchParams<{
    workspace: string;
    id: string;
    commentId: string;
    suggested?: string;
  }>();

  const createReminder = useCreateCommentReminder();
  const [text, setText] = useState(suggested ?? "");
  // Custom date+time draft seeds at +1h so the wheel opens on a sensible,
  // already-valid future moment.
  const [customDate, setCustomDate] = useState<Date>(() =>
    addMinutes(new Date(), 60),
  );
  // Guards against a double-tap firing two reminders during the in-flight POST.
  const submittingRef = useRef(false);
  const [submitting, setSubmitting] = useState(false);

  const presets = useMemo(
    () => [
      { label: "In 1 hour", date: () => addMinutes(new Date(), 60) },
      { label: "Tomorrow", hint: "9:00", date: () => tomorrowAt(9) },
    ],
    [],
  );

  const submit = (when: Date) => {
    if (submittingRef.current) return;
    if (when.getTime() <= Date.now()) {
      Alert.alert(
        "Pick a future time",
        "The reminder time has to be in the future.",
      );
      return;
    }
    submittingRef.current = true;
    setSubmitting(true);
    createReminder.mutate(
      { commentId, issueId: id, text: text.trim(), plannedAt: when },
      {
        onSuccess: () => {
          Haptics.notificationAsync(
            Haptics.NotificationFeedbackType.Success,
          ).catch(() => {});
          router.back();
        },
        onError: (err) => {
          submittingRef.current = false;
          setSubmitting(false);
          // The server returns 403 when an owner has turned the
          // cerebro_comment_reminders flag off after this sheet opened.
          const message =
            err instanceof ApiError && err.status === 403
              ? "Comment reminders are turned off for this workspace."
              : "Could not set the reminder. Please try again.";
          Alert.alert("Reminder not set", message);
        },
      },
    );
  };

  return (
    <View className="flex-1">
      <View className="flex-row items-center justify-between px-4 pt-4 pb-2">
        <Text className="text-base font-semibold text-foreground">
          Remind me
        </Text>
        <Pressable
          onPress={() => router.back()}
          hitSlop={6}
          className="px-2 py-1 rounded-md active:bg-secondary"
        >
          <Text className="text-sm text-muted-foreground">Cancel</Text>
        </Pressable>
      </View>

      <ScrollView
        className="flex-1"
        contentContainerClassName="px-4 pb-8"
        keyboardShouldPersistTaps="handled"
      >
        {/* Editable reminder text — pre-filled with the server-mirrored
            suggestion. Leaving it blank lets the server fill the snippet. */}
        <Text className="text-xs font-medium text-muted-foreground mb-1.5">
          Reminder
        </Text>
        <AutosizeTextArea
          value={text}
          onChangeText={setText}
          placeholder="What should this remind you about?"
          minHeight={44}
          maxHeight={120}
          className="border border-border rounded-md px-3 py-2 bg-secondary/50"
        />

        {/* Quick presets — each fires immediately with the current text. */}
        <View className="mt-5 border-t border-border">
          {presets.map((preset) => (
            <Pressable
              key={preset.label}
              onPress={() => submit(preset.date())}
              disabled={submitting}
              className="flex-row items-center justify-between py-3.5 border-b border-border active:opacity-60"
            >
              <Text className="text-base text-foreground">{preset.label}</Text>
              {preset.hint ? (
                <Text className="text-sm text-muted-foreground">
                  {preset.hint}
                </Text>
              ) : null}
            </Pressable>
          ))}
        </View>

        {/* Custom date + time on the native wheel, then one commit button. */}
        <Text className="text-sm font-medium text-muted-foreground mt-5 mb-1">
          Pick date & time
        </Text>
        <View className="items-center">
          <DateTimePicker
            value={customDate}
            mode="datetime"
            display="spinner"
            minimumDate={new Date()}
            onChange={(_event, selected) => {
              if (selected) setCustomDate(selected);
            }}
          />
        </View>
        <Button
          className="mt-2"
          disabled={submitting}
          onPress={() => submit(customDate)}
        >
          <Text>Set reminder</Text>
        </Button>
      </ScrollView>
    </View>
  );
}
