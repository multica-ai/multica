"use client";

import { useState, useEffect } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import {
  InputOTP,
  InputOTPGroup,
  InputOTPSlot,
} from "@multica/ui/components/ui/input-otp";
// Named import, NOT default: react-qr-code is CJS, and electron-vite's
// dep-optimizer default-import interop handed back the module namespace
// object instead of the component. The named export maps straight to
// `exports.QRCode` and resolves correctly under both bundlers.
// See lark-tab.tsx for the full comment on this interop issue.
import { QRCode } from "react-qr-code";
import { api } from "@multica/core/api";
import { toast } from "sonner";
import { useT } from "../../i18n";

export function TOTPSetupDialog(props: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSuccess: () => void;
}) {
  const { t } = useT("settings");
  const [step, setStep] = useState<"qr" | "verify">("qr");
  const [otpauth, setOtpauth] = useState("");
  const [secret, setSecret] = useState("");
  const [code, setCode] = useState("");
  const [loading, setLoading] = useState(false);

  // Fetch setup-init data when dialog opens and we're on the QR step without
  // a URI yet. We guard on !otpauth so a re-render while the dialog is open
  // doesn't fire a second request.
  useEffect(() => {
    if (props.open && step === "qr" && !otpauth) {
      api
        .totpSetupInit()
        .then(({ secret: s, otpauth_url }) => {
          setSecret(s);
          setOtpauth(otpauth_url);
        })
        .catch(() => {
          toast.error(t(($) => $.security.two_factor.setup_init_error));
          props.onOpenChange(false);
        });
    }
  }, [props.open, step, otpauth]);

  // Reset all state when the dialog closes so the next open starts fresh.
  useEffect(() => {
    if (!props.open) {
      setStep("qr");
      setOtpauth("");
      setSecret("");
      setCode("");
    }
  }, [props.open]);

  const handleVerify = async () => {
    if (code.length !== 6) return;
    setLoading(true);
    try {
      await api.totpSetupVerify(code);
      toast.success(t(($) => $.security.two_factor.setup_success));
      props.onSuccess();
      props.onOpenChange(false);
    } catch {
      toast.error(t(($) => $.security.two_factor.setup_invalid));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {t(($) => $.security.two_factor.setup_title)}
          </DialogTitle>
        </DialogHeader>

        {step === "qr" && otpauth && (
          <div className="space-y-4">
            <p className="text-sm">{t(($) => $.security.two_factor.setup_scan_qr)}</p>
            <div className="flex justify-center rounded bg-white p-4">
              <QRCode value={otpauth} size={200} />
            </div>
            <p className="text-xs text-muted-foreground">
              {t(($) => $.security.two_factor.setup_manual)}
              <code className="mt-1 block select-all break-all font-mono">
                {secret}
              </code>
            </p>
            <Button onClick={() => setStep("verify")} className="w-full">
              {t(($) => $.security.two_factor.setup_next)}
            </Button>
          </div>
        )}

        {step === "qr" && !otpauth && (
          <p className="py-4 text-center text-sm text-muted-foreground">
            Loading...
          </p>
        )}

        {step === "verify" && (
          <div className="space-y-4">
            <p className="text-sm">{t(($) => $.security.two_factor.setup_verify)}</p>
            <div className="flex justify-center">
              <InputOTP maxLength={6} value={code} onChange={setCode}>
                <InputOTPGroup>
                  {[0, 1, 2, 3, 4, 5].map((i) => (
                    <InputOTPSlot key={i} index={i} />
                  ))}
                </InputOTPGroup>
              </InputOTP>
            </div>
            <Button
              onClick={handleVerify}
              disabled={code.length !== 6 || loading}
              className="w-full"
            >
              {loading ? "..." : t(($) => $.security.two_factor.confirm)}
            </Button>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
