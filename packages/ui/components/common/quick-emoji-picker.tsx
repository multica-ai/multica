"use client";

import { useState, lazy, Suspense } from "react";
import { SmilePlus } from "lucide-react";
import { Popover, PopoverTrigger, PopoverContent } from "@multica/ui/components/ui/popover";

const EmojiPicker = lazy(() =>
  import("./emoji-picker").then((m) => ({ default: m.EmojiPicker })),
);

const QUICK_EMOJIS = ["👍", "👌", "❤️", "😄", "🎉", "😕", "🚀", "👀"];

interface QuickEmojiPickerProps {
  onSelect: (emoji: string) => void;
  align?: "start" | "end";
  className?: string;
}

function QuickEmojiPicker({ onSelect, align = "start", className }: QuickEmojiPickerProps) {
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
            className={`inline-flex items-center justify-center h-6 w-6 rounded-full text-muted-foreground hover:bg-accent hover:text-foreground transition-colors ${className ?? ""}`}
          >
            <SmilePlus className="h-3.5 w-3.5" />
          </button>
        }
      />
      <PopoverContent align={align} className="w-auto p-0">
        {showFull ? (
          <Suspense fallback={<div className="p-4 text-sm text-muted-foreground">Loading...</div>}>
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
                  className="h-8 w-8 flex items-center justify-center rounded hover:bg-accent text-base transition-colors"
                >
                  {emoji}
                </button>
              ))}
            </div>
            <button
              type="button"
              onClick={() => setShowFull(true)}
              className="mt-1.5 w-full text-xs text-muted-foreground hover:text-foreground text-center py-1 rounded hover:bg-accent transition-colors"
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
