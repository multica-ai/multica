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
import { api } from "@multica/core/api";
import { toast } from "sonner";
import { useT } from "../../i18n";

export function TOTPDisableDialog(props: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSuccess: () => void;
}) {
  const { t } = useT("settings");
  const [code, setCode] = useState("");
  const [loading, setLoading] = useState(false);

  // Clear the code field whenever the dialog closes so it's fresh next open.
  useEffect(() => {
    if (!props.open) setCode("");
  }, [props.open]);

  const handleDisable = async () => {
    if (code.length !== 6) return;
    setLoading(true);
    try {
      await api.totpDisable(code);
      toast.success(t(($) => $.security.two_factor.disable_success));
      props.onSuccess();
      props.onOpenChange(false);
    } catch {
      toast.error(t(($) => $.security.two_factor.disable_invalid));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {t(($) => $.security.two_factor.disable_title)}
          </DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          <p className="text-sm">
            {t(($) => $.security.two_factor.disable_prompt)}
          </p>
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
            onClick={handleDisable}
            disabled={code.length !== 6 || loading}
            variant="destructive"
            className="w-full"
          >
            {loading ? "..." : t(($) => $.security.two_factor.confirm)}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
