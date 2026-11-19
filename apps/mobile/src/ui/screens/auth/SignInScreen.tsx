import { useEffect, useState } from "react";
import { ApiError } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { endpoints } from "../../../platform/env";
import { useStrings, type Strings } from "../../strings";
import { CODE_LENGTH, SignInView } from "./SignInView";

const RESEND_SECONDS = 30;

function errorText(error: unknown, strings: Strings): string {
  if (error instanceof ApiError) {
    if (error.status === 429) return strings.auth.tooMany;
    if (error.status === 400 || error.status === 401) return strings.auth.invalidCode;
  }
  return strings.auth.failed;
}

export function SignInScreen() {
  const strings = useStrings();
  const sendCode = useAuthStore((state) => state.sendCode);
  const verifyCode = useAuthStore((state) => state.verifyCode);
  const [step, setStep] = useState<"email" | "code">("email");
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [resendIn, setResendIn] = useState(0);

  useEffect(() => {
    if (resendIn <= 0) return;
    const timer = setTimeout(() => setResendIn((value) => value - 1), 1000);
    return () => clearTimeout(timer);
  }, [resendIn]);

  const run = async (action: () => Promise<unknown>, after?: () => void) => {
    setBusy(true);
    setError(null);
    try {
      await action();
      after?.();
    } catch (cause) {
      setError(errorText(cause, strings));
    } finally {
      setBusy(false);
    }
  };

  const requestCode = () =>
    run(
      () => sendCode(email.trim()),
      () => {
        setStep("code");
        setCode("");
        setResendIn(RESEND_SECONDS);
      },
    );

  const submit = (value = code) => {
    if (busy) return;
    if (step === "email") void requestCode();
    else if (value.length === CODE_LENGTH) void run(() => verifyCode(email.trim(), value));
  };

  return (
    <SignInView
      step={step}
      email={email}
      code={code}
      onEmailChange={(value) => {
        setEmail(value);
        setError(null);
      }}
      onCodeChange={(value) => {
        setCode(value);
        setError(null);
        if (value.length === CODE_LENGTH) submit(value);
      }}
      onSubmit={() => submit()}
      onBack={() => {
        setStep("email");
        setError(null);
      }}
      onResend={() => void requestCode()}
      resendIn={resendIn}
      busy={busy}
      error={error}
      serverHost={new URL(endpoints.apiUrl).host}
    />
  );
}
