"use client";

import { useCallback, useState } from "react";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@multica/ui/components/ui/card";
import {
  InputOTP,
  InputOTPGroup,
  InputOTPSeparator,
  InputOTPSlot,
} from "@multica/ui/components/ui/input-otp";
import { api } from "@multica/core/api";
import { useT } from "../i18n";

// Device authorization approval (`multica login --device`): the CLI on a
// remote machine shows an 8-character code; the signed-in user types it here
// to hand that CLI a login token. Mirrors the login page's OTP input.
const DEVICE_CODE_ALPHANUMERIC = /^[a-zA-Z0-9]+$/;

export function ActivatePage() {
  const { t } = useT("auth");
  const [code, setCode] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);

  const handleApprove = useCallback(async (value: string) => {
    if (value.length !== 8) return;
    setLoading(true);
    setError("");
    try {
      await api.approveDeviceAuthorization(value);
      setDone(true);
    } catch (err) {
      setError(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.activate.errors.approve_failed),
      );
    } finally {
      setLoading(false);
    }
  }, [t]);

  if (done) {
    return (
      <Card className="w-full max-w-sm">
        <CardHeader className="text-center">
          <CardTitle className="text-display-sm">
            {t(($) => $.activate.approved_title)}
          </CardTitle>
          <CardDescription>
            {t(($) => $.activate.approved_description)}
          </CardDescription>
        </CardHeader>
      </Card>
    );
  }

  return (
    <Card className="w-full max-w-sm">
      <CardHeader className="text-center">
        <CardTitle className="text-display-sm">
          {t(($) => $.activate.title)}
        </CardTitle>
        <CardDescription>{t(($) => $.activate.description)}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col items-center gap-4">
        <InputOTP
          autoFocus
          maxLength={8}
          pattern={DEVICE_CODE_ALPHANUMERIC}
          inputMode="text"
          autoCapitalize="characters"
          value={code}
          onChange={(value) => {
            const upper = value.toUpperCase();
            setCode(upper);
            setError("");
            if (upper.length === 8) handleApprove(upper);
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
        {loading && (
          <p className="text-body text-muted-foreground">
            {t(($) => $.activate.approving)}
          </p>
        )}
        {error && <p className="text-body text-destructive">{error}</p>}
      </CardContent>
    </Card>
  );
}
