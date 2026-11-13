import DateTimePicker from "@react-native-community/datetimepicker";
import { useTheme } from "../theme";

interface DateTimeInputProps {
  value: Date;
  mode: "date" | "time";
  onChange: (value: Date) => void;
}

/** The system picker, inline: a calendar for dates, wheels for times. */
export function DateTimeInput({ value, mode, onChange }: DateTimeInputProps) {
  const palette = useTheme();
  return (
    <DateTimePicker
      value={value}
      mode={mode}
      display={mode === "date" ? "inline" : "spinner"}
      themeVariant={palette.scheme}
      accentColor={palette.accent}
      onChange={(_event, next) => {
        if (next) onChange(next);
      }}
    />
  );
}
