"use client";

import { ChevronLeft } from "lucide-react";
import { Button } from "@/components/ui/button";

interface MobileListDetailProps {
  list: React.ReactNode;
  detail: React.ReactNode;
  showDetail: boolean;
  onBack: () => void;
  headerTitle?: string;
}

/**
 * Mobile list→detail navigation pattern.
 * Shows the list by default. When `showDetail` is true, shows the detail
 * view full-screen with a back button.
 */
export function MobileListDetail({
  list,
  detail,
  showDetail,
  onBack,
  headerTitle,
}: MobileListDetailProps) {
  if (showDetail) {
    return (
      <div className="flex flex-col h-full min-h-0">
        <div className="flex h-12 shrink-0 items-center gap-1 border-b px-2">
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={onBack}
          >
            <ChevronLeft className="h-5 w-5" />
          </Button>
          {headerTitle && (
            <span className="text-sm font-medium truncate">{headerTitle}</span>
          )}
        </div>
        <div className="flex-1 min-h-0 overflow-hidden">
          {detail}
        </div>
      </div>
    );
  }

  return <>{list}</>;
}
