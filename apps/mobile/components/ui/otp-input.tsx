/**
 * Six-slot OTP input. Wraps `input-otp-native`'s headless OTPInput with
 * mobile design tokens (bg-secondary/50 box, primary border on active slot,
 * 2px caret). Paste-to-fill, autofocus, and iOS/Android one-time-code
 * autofill are handled by the underlying library (textContentType + autoComplete
 * are set inside its TextInput).
 *
 * Numeric input enforced via `inputMode="numeric"` (library default).
 */
import { forwardRef } from "react";
import { Text, View } from "react-native";
import {
  OTPInput,
  type OTPInputProps,
  type OTPInputRef,
  type SlotProps,
} from "input-otp-native";
import { cn } from "@/lib/utils";

export type { OTPInputRef as OtpInputRef };

export interface OtpInputProps
  extends Omit<OTPInputProps, "render" | "maxLength"> {
  numberOfDigits?: number;
}

export const OtpInput = forwardRef<OTPInputRef, OtpInputProps>(
  ({ numberOfDigits = 6, ...rest }, ref) => {
    return (
      <OTPInput
        ref={ref}
        maxLength={numberOfDigits}
        containerStyle={{ flexDirection: "row", gap: 8 }}
        render={({ slots }) => (
          <>
            {slots.map((slot, idx) => (
              <Slot key={idx} slot={slot} />
            ))}
          </>
        )}
        {...rest}
      />
    );
  },
);
OtpInput.displayName = "OtpInput";

function Slot({ slot }: { slot: SlotProps }) {
  return (
    <View
      className={cn(
        "w-12 h-14 rounded-md items-center justify-center bg-secondary/50",
        slot.isActive && "border-2 border-primary",
      )}
    >
      {slot.char ? (
        <Text
          className="text-foreground font-semibold"
          style={{ fontSize: 22, includeFontPadding: false }}
        >
          {slot.char}
        </Text>
      ) : slot.hasFakeCaret ? (
        <View className="w-0.5 h-6 bg-foreground" />
      ) : null}
    </View>
  );
}
