/**
 * Pure picker body for due-date. Wraps the native UIDatePicker. The caller
 * (a formSheet route) renders the Done / Clear actions in its own header
 * area — this body only handles the picker spinner + the local draft state.
 *
 * Backend (server/internal/handler/issue.go CreateIssue / UpdateIssue) parses
 * with time.Parse(time.RFC3339, ...) — strict. Mirrors web's
 * packages/views/issues/components/pickers/due-date-picker.tsx which sends
 * d.toISOString().
 */
import { useState, useEffect, useImperativeHandle, forwardRef } from "react";
import { View } from "react-native";
import DateTimePicker from "@react-native-community/datetimepicker";

interface Props {
  value: string | null;
}

export interface DueDatePickerBodyHandle {
  /** Returns the currently-displayed date as an ISO 8601 string. */
  getIso: () => string;
}

function isoToDate(iso: string | null): Date {
  if (!iso) return new Date();
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? new Date() : d;
}

export const DueDatePickerBody = forwardRef<DueDatePickerBodyHandle, Props>(
  function DueDatePickerBody({ value }, ref) {
    const [draft, setDraft] = useState<Date>(() => isoToDate(value));

    useEffect(() => {
      setDraft(isoToDate(value));
    }, [value]);

    useImperativeHandle(ref, () => ({
      getIso: () => draft.toISOString(),
    }));

    return (
      <View className="flex-1 items-center pt-2">
        <DateTimePicker
          value={draft}
          mode="date"
          display="inline"
          onChange={(_event, selected) => {
            if (selected) setDraft(selected);
          }}
        />
      </View>
    );
  },
);
