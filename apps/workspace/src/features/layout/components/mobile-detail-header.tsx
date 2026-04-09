"use client";

import { ChevronLeft } from "lucide-react";
import { Button } from "@/components/ui/button";

interface MobileDetailHeaderProps {
  title: string;
  subtitle?: string;
  onBack: () => void;
}

export function MobileDetailHeader({
  title,
  subtitle,
  onBack,
}: MobileDetailHeaderProps) {
  return (
    <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4 md:hidden">
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label={`Back to ${title}`}
        onClick={onBack}
      >
        <ChevronLeft className="size-4" />
      </Button>
      <div className="min-w-0 flex-1">
        <div className="text-sm font-semibold">{title}</div>
        {subtitle ? (
          <div className="truncate text-xs text-muted-foreground">
            {subtitle}
          </div>
        ) : null}
      </div>
    </div>
  );
}
