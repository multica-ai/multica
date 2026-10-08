"use client";

import { useState, lazy, Suspense } from "react";
import { SmilePlus } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Popover, PopoverTrigger, PopoverContent } from "@multica/ui/components/ui/popover";

const EmojiPicker = lazy(() =>
  import("./emoji-picker").then((m) => ({ default: m.EmojiPicker })),
);

const QUICK_EMOJIS = ["👍", "👌", "❤️", "✅", "🎉", "😕", "🚀", "👀"];

interface QuickEmojiPickerProps {
  onSelect: (emoji: string) => void;
  align?: "start" | "end";
  className?: string;
  ariaLabel?: string;
}

function QuickEmojiPicker({ onSelect, align = "start", className, ariaLabel = "Add reaction" }: QuickEmojiPickerProps) {
  const [open, setOpen] = useState(false);
  const [showFull, setShowFull] = useState(false);

  const handleOpenChange = (v: boolean) => {
    setOpen(v);
    if (!v) setShowFull(false);
  };

  const handleSelect = (emoji: string) => {
    onSelect(emoji);
    setOpen(false);
    setShowFull(false);
  };

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger
        render={
          <button
            type="button"
            aria-label={ariaLabel}
            title={ariaLabel}
            className={cn("inline-flex shrink-0 items-center justify-center h-6 w-6 rounded-full text-muted-foreground hover:bg-accent hover:text-foreground transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring", className)}
          >
            <SmilePlus className="h-3.5 w-3.5" aria-hidden />
          </button>
        }
      />
      <PopoverContent align={align} className="w-auto p-0">
        {showFull ? (
          <Suspense fallback={<div className="p-4 text-body text-muted-foreground">Loading...</div>}>
            <EmojiPicker onSelect={handleSelect} />
          </Suspense>
        ) : (
          <div className="p-2">
            <div className="flex gap-1">
              {QUICK_EMOJIS.map((emoji) => (
                <button
                  key={emoji}
                  type="button"
                  onClick={() => handleSelect(emoji)}
                  className="h-8 w-8 flex items-center justify-center rounded-xs hover:bg-accent text-title-sm transition-colors"
                >
                  {emoji}
                </button>
              ))}
            </div>
            <button
              type="button"
              onClick={() => setShowFull(true)}
              className="mt-1.5 w-full text-caption text-muted-foreground hover:text-foreground text-center py-1 rounded-xs hover:bg-accent transition-colors"
            >
              More emojis...
            </button>
          </div>
        )}
      </PopoverContent>
    </Popover>
  );
}

export { QuickEmojiPicker };
