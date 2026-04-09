"use client";

import { useEffect, useRef, useCallback } from "react";
import data from "@emoji-mart/data";
import { Picker } from "emoji-mart";

interface EmojiPickerProps {
  onSelect: (emoji: string) => void;
}

export function EmojiPicker({ onSelect }: EmojiPickerProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const onSelectRef = useRef(onSelect);
  onSelectRef.current = onSelect;

  const handleSelect = useCallback((emoji: { native: string }) => {
    onSelectRef.current(emoji.native);
  }, []);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const picker = new Picker({
      data,
      onEmojiSelect: handleSelect,
      theme: "auto",
      set: "native",
      previewPosition: "none",
      skinTonePosition: "search",
      maxFrequentRows: 2,
    });

    container.appendChild(picker as unknown as Node);

    return () => {
      container.replaceChildren();
    };
  }, [handleSelect]);

  return <div ref={containerRef} />;
}
