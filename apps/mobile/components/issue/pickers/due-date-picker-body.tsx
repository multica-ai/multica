/**
 * Pure picker body for due-date. Wraps the native UIDatePicker. The caller
 * (a formSheet route) renders the Done / Clear actions in its own header
 * area — this body only handles the picker spinner + the local draft state.
 *
 * due_date is a calendar day (date-only "YYYY-MM-DD", no time/timezone — see
 * @multica/core/issues/date and GH #3618). Mirrors web's
 * packages/views/issues/components/pickers/due-date-picker.tsx: read the stored
 * day into a local-midnight Date for the spinner, write back the picked local
 * day as a date-only string.
 */
import { useState, useEffect, useImperativeHandle, forwardRef } from "react";
import { Platform, View } from "react-native";
import DateTimePicker from "@react-native-community/datetimepicker";
import { toDateOnly, dateOnlyToLocalDate } from "@multica/core/issues/date";
import { i18n } from "@/lib/i18n";
import { Button } from "@/components/ui/button";
import { Text } from "@/components/ui/text";

interface Props {
  value: string | null;
}

export interface DueDatePickerBodyHandle {
  /** Returns the currently-displayed day as a date-only "YYYY-MM-DD" string. */
  getIso: () => string;
}

function toLocalDay(value: string | null): Date {
  return dateOnlyToLocalDate(value) ?? new Date();
}

export const DueDatePickerBody = forwardRef<DueDatePickerBodyHandle, Props>(
  function DueDatePickerBody({ value }, ref) {
    const [draft, setDraft] = useState<Date>(() => toLocalDay(value));
    const [showPicker, setShowPicker] = useState(Platform.OS === "ios");

    useEffect(() => {
      setDraft(toLocalDay(value));
    }, [value]);

    useImperativeHandle(ref, () => ({
      getIso: () => toDateOnly(draft),
    }));

    return (
      <View className="flex-1 items-center pt-2">
        {Platform.OS === "android" ? (
          <Button variant="outline" onPress={() => setShowPicker(true)}>
            <Text>{draft.toLocaleDateString(i18n.resolvedLanguage ?? i18n.language)}</Text>
          </Button>
        ) : null}
        {showPicker ? (
          <DateTimePicker
            value={draft}
            mode="date"
            display={Platform.OS === "ios" ? "inline" : "default"}
            locale={i18n.resolvedLanguage ?? i18n.language}
            onChange={(event, selected) => {
              if (Platform.OS === "android") setShowPicker(false);
              if (event.type === "set" && selected) setDraft(selected);
            }}
          />
        ) : null}
      </View>
    );
  },
);
