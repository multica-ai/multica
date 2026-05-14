/**
 * Due-date picker. Wraps `@react-native-community/datetimepicker` (native
 * UIDatePicker on iOS, Material spinner on Android). Two affordances:
 *   - "Done" — sends the currently displayed date as ISO 8601 / RFC 3339
 *   - "Clear due date" — sends null (only shown when value is set)
 *
 * Backend (`server/internal/handler/issue.go` CreateIssue / UpdateIssue)
 * parses with `time.Parse(time.RFC3339, ...)` — strict. Mirrors web's
 * `packages/views/issues/components/pickers/due-date-picker.tsx:57` which
 * sends `d.toISOString()`.
 *
 * Note: full ISO means UTC. Users in negative or large positive offsets
 * may see a one-day shift after round-trip (e.g. local "May 14" stored as
 * "2026-05-13T16:00:00Z" for UTC+8 if backend truncates day). This
 * matches web's behavior — diverging here would break parity.
 */
import { useState, useEffect } from "react";
import { Modal, Pressable, View } from "react-native";
import DateTimePicker from "@react-native-community/datetimepicker";
import { Text } from "@/components/ui/text";

interface Props {
  visible: boolean;
  value: string | null;
  onChange: (next: string | null) => void;
  onClose: () => void;
}

function isoToDate(iso: string | null): Date {
  if (!iso) return new Date();
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? new Date() : d;
}

function dateToIso(d: Date): string {
  return d.toISOString();
}

export function DueDatePickerSheet({
  visible,
  value,
  onChange,
  onClose,
}: Props) {
  const [draft, setDraft] = useState<Date>(() => isoToDate(value));

  // Reset draft to incoming value when sheet (re)opens.
  useEffect(() => {
    if (visible) setDraft(isoToDate(value));
  }, [visible, value]);

  const submit = () => {
    onChange(dateToIso(draft));
    onClose();
  };
  const clear = () => {
    onChange(null);
    onClose();
  };

  return (
    <Modal
      visible={visible}
      transparent
      animationType="fade"
      onRequestClose={onClose}
    >
      <Pressable className="flex-1 bg-black/40" onPress={onClose}>
        <View className="flex-1 items-center justify-center px-6">
          <Pressable onPress={() => {}} className="w-full max-w-sm">
            <View className="bg-popover rounded-2xl p-4 gap-3">
              <DateTimePicker
                value={draft}
                mode="date"
                display="inline"
                onChange={(_event, selected) => {
                  if (selected) setDraft(selected);
                }}
              />
              <View className="flex-row gap-2 justify-end">
                {value ? (
                  <Pressable
                    onPress={clear}
                    className="px-3 py-2 rounded-md active:bg-secondary"
                  >
                    <Text className="text-sm text-destructive">Clear</Text>
                  </Pressable>
                ) : null}
                <Pressable
                  onPress={onClose}
                  className="px-3 py-2 rounded-md active:bg-secondary"
                >
                  <Text className="text-sm text-muted-foreground">Cancel</Text>
                </Pressable>
                <Pressable
                  onPress={submit}
                  className="px-3 py-2 rounded-md bg-primary active:opacity-80"
                >
                  <Text className="text-sm text-primary-foreground">Done</Text>
                </Pressable>
              </View>
            </View>
          </Pressable>
        </View>
      </Pressable>
    </Modal>
  );
}
