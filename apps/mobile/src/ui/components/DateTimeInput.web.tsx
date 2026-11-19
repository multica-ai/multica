import { Txt } from "./Text";

// The system picker has no browser build; the preview shows the value instead.
export function DateTimeInput({ value, mode }: { value: Date; mode: "date" | "time"; onChange: (value: Date) => void }) {
  return (
    <Txt variant="body" tone="secondary" style={{ padding: 16 }}>
      {mode === "date" ? value.toDateString() : value.toTimeString().slice(0, 5)}
    </Txt>
  );
}
