"use client";

import { useState } from "react";
import {
  InputOTP,
  InputOTPGroup,
  InputOTPSeparator,
  InputOTPSlot,
} from "@multica/ui/components/ui/input-otp";
import { Button } from "@multica/ui/components/ui/button";
import { api, ApiError } from "@multica/core/api";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";

// Device authorization approval (`multica login --device`): the CLI on a
// remote machine shows an 8-character code; the signed-in user types it here
// to see which machine is asking, then explicitly approve handing it a login
// token. Mirrors the login page's OTP input.
// input-otp's pattern prop is a string (matched with RegExp constructor).
const DEVICE_CODE_ALPHANUMERIC = "^[a-zA-Z0-9]+$";

// The CLI displays the code as "XXXX-XXXX"; pasting it with the dash must
// still fill all 8 slots (input-otp would otherwise count the dash toward
// maxLength and fail the pattern). The server normalizes the same way.
const stripDeviceCodeSeparators = (pasted: string) =>
  pasted.replace(/[^a-zA-Z0-9]/g, "").toUpperCase();

export function DeviceCodeApproval({
  autoFocus = false,
  onApproved,
  className,
}: {
  autoFocus?: boolean;
  onApproved: () => void;
  className?: string;
}) {
  const { t } = useT("auth");
  const [code, setCode] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [machine, setMachine] = useState<{ client_name: string } | null>(null);

  async function submit() {
    if (code.length !== 8) return;
    setLoading(true);
    setError("");
    try {
      if (machine) {
        await api.approveDeviceAuthorization(code);
        onApproved();
      } else {
        setMachine(await api.inspectDeviceAuthorization(code));
      }
    } catch (err) {
      const body = err instanceof ApiError ? err.body : null;
      if (
        body &&
        typeof body === "object" &&
        "code" in body &&
        body.code === "invalid_device_code"
      ) {
        setError(t(($) => $.activate.errors.invalid_code));
        setCode("");
        setMachine(null);
      } else {
        setError(t(($) => $.activate.errors.approve_failed));
      }
    } finally {
      setLoading(false);
    }
  }

  function reset() {
    setMachine(null);
    setCode("");
    setError("");
  }

  return (
    <div className={cn("flex flex-col items-center gap-4", className)}>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          void submit();
        }}
        className="flex flex-col items-center gap-4"
      >
        {machine ? (
          <div className="text-center break-all">
            <p className="text-caption text-muted-foreground">
              {t(($) => $.activate.machine_label)}
            </p>
            <p className="text-body font-medium">
              {machine.client_name || t(($) => $.activate.unknown_machine)}
            </p>
            <p className="text-caption text-muted-foreground">
              {t(($) => $.activate.confirm_description)}
            </p>
          </div>
        ) : (
          <InputOTP
            aria-label={t(($) => $.activate.code_label)}
            autoFocus={autoFocus}
            maxLength={8}
            pattern={DEVICE_CODE_ALPHANUMERIC}
            pasteTransformer={stripDeviceCodeSeparators}
            inputMode="text"
            autoCapitalize="characters"
            value={code}
            onChange={(value) => {
              setCode(value.toUpperCase());
              setError("");
            }}
            disabled={loading}
          >
            <InputOTPGroup>
              <InputOTPSlot index={0} />
              <InputOTPSlot index={1} />
              <InputOTPSlot index={2} />
              <InputOTPSlot index={3} />
            </InputOTPGroup>
            <InputOTPSeparator />
            <InputOTPGroup>
              <InputOTPSlot index={4} />
              <InputOTPSlot index={5} />
              <InputOTPSlot index={6} />
              <InputOTPSlot index={7} />
            </InputOTPGroup>
          </InputOTP>
        )}
        <Button type="submit" disabled={loading || code.length !== 8} aria-busy={loading}>
          {machine ? t(($) => $.activate.confirm) : t(($) => $.activate.continue)}
        </Button>
        {machine && (
          <Button type="button" variant="ghost" disabled={loading} onClick={reset}>
            {t(($) => $.activate.back)}
          </Button>
        )}
      </form>
      {loading && (
        <p className="text-body text-muted-foreground">
          {t(($) => $.activate.approving)}
        </p>
      )}
      {error && <p className="text-body text-destructive">{error}</p>}
    </div>
  );
}
