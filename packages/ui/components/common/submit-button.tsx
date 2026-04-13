"use client";

import { ArrowUp, Loader2, Square } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";

interface SubmitButtonProps {
  onClick: () => void;
  disabled?: boolean;
  loading?: boolean;
  running?: boolean;
  onStop?: () => void;
}

function SubmitButton({ onClick, disabled, loading, running, onStop }: SubmitButtonProps) {
  if (running) {
    return (
      <Button size="icon-sm" onClick={onStop}>
        <Square className="fill-current" />
      </Button>
    );
  }

  return (
    <Button size="icon-sm" disabled={disabled || loading} onClick={onClick}>
      {loading ? (
        <Loader2 className="animate-spin" />
      ) : (
        <ArrowUp />
      )}
    </Button>
  );
}

export { SubmitButton, type SubmitButtonProps };
