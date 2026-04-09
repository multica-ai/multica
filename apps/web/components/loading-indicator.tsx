"use client";

import { Spinner } from "@/components/spinner";
import { cn } from "@multica/ui/lib/utils";

export type LoadingVariant = "generating" | "streaming";

interface LoadingIndicatorProps {
  variant: LoadingVariant;
  className?: string;
}

const VARIANT_TEXT: Record<LoadingVariant, string> = {
  generating: "Generating...",
  streaming: "Streaming...",
};

/**
 * Unified loading indicator for chat.
 * Use "generating" when waiting for AI response (no content yet).
 * Use "streaming" when content is actively being received.
 */
export function LoadingIndicator({ variant, className }: LoadingIndicatorProps) {
  return (
    <div className={cn("flex items-center gap-2 py-1 text-muted-foreground", className)}>
      <Spinner className="text-xs" />
      <span className="text-xs">{VARIANT_TEXT[variant]}</span>
    </div>
  );
}
