"use client";

import { ArrowDown, ArrowUp } from "lucide-react";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";

type IssueJumpFabProps = {
  isNearBottom: boolean;
  onClick: () => void;
  jumpToTopLabel: string;
  jumpToBottomLabel: string;
};

export function IssueJumpFab({
  isNearBottom,
  onClick,
  jumpToTopLabel,
  jumpToBottomLabel,
}: IssueJumpFabProps) {
  const label = isNearBottom ? jumpToTopLabel : jumpToBottomLabel;

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            onClick={onClick}
            aria-label={label}
            className="absolute bottom-14 right-2 z-50 flex size-10 cursor-pointer items-center justify-center rounded-full bg-card text-muted-foreground shadow-sm ring-1 ring-foreground/10 transition-transform hover:scale-110 hover:text-accent-foreground active:scale-95"
          >
            {isNearBottom ? <ArrowUp className="size-5" /> : <ArrowDown className="size-5" />}
          </button>
        }
      />
      <TooltipContent side="left" sideOffset={10}>{label}</TooltipContent>
    </Tooltip>
  );
}
