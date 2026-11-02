"use client";

import { useId } from "react";
import { FolderPlus } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";

export function NewFolderButton({
  label,
  disabled = false,
  disabledReason,
  onClick,
}: {
  label: string;
  disabled?: boolean;
  disabledReason?: string;
  onClick: () => void;
}) {
  const reasonId = useId();
  const showReason = disabled && !!disabledReason;
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-2">
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={disabled}
        aria-describedby={showReason ? reasonId : undefined}
        onClick={onClick}
      >
        <FolderPlus aria-hidden="true" className="size-3.5" />
        {label}
      </Button>
      {showReason && (
        <span id={reasonId} className="text-caption text-muted-foreground">
          {disabledReason}
        </span>
      )}
    </div>
  );
}
