/**
 * Description input block shared by `new-issue.tsx` and `issue/[id]/edit.tsx`.
 *
 * Focus-tinted `rounded-2xl` container wrapping the `AutosizeTextArea` —
 * matches the "write markdown body" treatment used by the comment composer
 * so all three surfaces feel like the same control.
 *
 * Pure UI shell. The mention pipeline lives in the caller's `useMentionInput`
 * instance, passed in as `description`. Callers also own the floating
 * `MentionSuggestionBar` (it has to sit above the keyboard, outside the
 * scroll view).
 */
import { useState } from "react";
import { View } from "react-native";
import { AutosizeTextArea } from "@/components/ui/autosize-textarea";
import { MIN_BODY_INPUT_HEIGHT_PX } from "@/components/ui/input-tokens";
import { cn } from "@/lib/utils";
import type { UseMentionInputReturn } from "@/lib/use-mention-input";

export function DescriptionField({
  description,
  disabled,
  placeholder = "Description… (type @ to mention)",
}: {
  description: UseMentionInputReturn;
  disabled: boolean;
  placeholder?: string;
}) {
  const [focused, setFocused] = useState(false);
  return (
    <View
      className={cn(
        "rounded-2xl border px-3",
        focused
          ? "border-primary/30 bg-secondary"
          : "border-transparent bg-secondary/40",
      )}
    >
      <AutosizeTextArea
        value={description.text}
        onChangeText={description.handlers.onChangeText}
        selection={description.selection}
        onSelectionChange={description.handlers.onSelectionChange}
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
        placeholder={placeholder}
        className="py-2"
        minHeight={MIN_BODY_INPUT_HEIGHT_PX}
        editable={!disabled}
      />
    </View>
  );
}
