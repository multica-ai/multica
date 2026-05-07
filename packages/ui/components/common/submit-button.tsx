"use client";

// CEREBRO-PATCH(ui-submit-button): cerebro modification of upstream file

import { ArrowUp, Loader2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";

interface SubmitButtonProps {
  onClick: () => void;
  disabled?: boolean;
  loading?: boolean;
}

function SubmitButton({ onClick, disabled, loading }: SubmitButtonProps) {
  return (
    <Button size="icon-sm" disabled={disabled || loading} onClick={onClick}>
      {loading ? <Loader2 className="animate-spin" /> : <ArrowUp />}
    </Button>
  );
}

export { SubmitButton, type SubmitButtonProps };
