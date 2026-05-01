"use client";

import { useState, useRef } from "react";
import { cn } from "@/lib/utils";

// Preset color palette for labels
export const LABEL_PRESET_COLORS = [
  { hex: "#ef4444", label: "Red" },
  { hex: "#f97316", label: "Orange" },
  { hex: "#eab308", label: "Yellow" },
  { hex: "#22c55e", label: "Green" },
  { hex: "#06b6d4", label: "Cyan" },
  { hex: "#3b82f6", label: "Blue" },
  { hex: "#8b5cf6", label: "Purple" },
  { hex: "#ec4899", label: "Pink" },
  { hex: "#14b8a6", label: "Teal" },
  { hex: "#f59e0b", label: "Amber" },
  { hex: "#6366f1", label: "Indigo" },
  { hex: "#6b7280", label: "Gray" },
];

interface LabelColorPickerProps {
  value: string;
  onChange: (color: string) => void;
  className?: string;
}

export function LabelColorPicker({ value, onChange, className }: LabelColorPickerProps) {
  const [customHex, setCustomHex] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  function handleCustomInput(raw: string) {
    setCustomHex(raw);
    const hex = raw.startsWith("#") ? raw : `#${raw}`;
    if (/^#[0-9a-fA-F]{6}$/.test(hex)) {
      onChange(hex);
    }
  }

  return (
    <div className={cn("space-y-2", className)}>
      {/* Preset swatches */}
      <div className="flex flex-wrap gap-1.5">
        {LABEL_PRESET_COLORS.map(({ hex, label }) => (
          <button
            key={hex}
            type="button"
            title={label}
            onClick={() => {
              onChange(hex);
              setCustomHex("");
            }}
            className={cn(
              "h-5 w-5 rounded-full border-2 transition-transform hover:scale-110",
              value === hex ? "border-foreground scale-110" : "border-transparent",
            )}
            style={{ backgroundColor: hex }}
          />
        ))}
      </div>

      {/* Custom hex input */}
      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={() => inputRef.current?.click()}
          className="h-5 w-5 shrink-0 rounded-full border border-border"
          style={{ backgroundColor: value }}
          title="Pick custom color"
        />
        <input
          ref={inputRef}
          type="color"
          value={value}
          onChange={(e) => {
            onChange(e.target.value);
            setCustomHex(e.target.value);
          }}
          className="sr-only"
        />
        <input
          type="text"
          value={customHex || value}
          onChange={(e) => handleCustomInput(e.target.value)}
          placeholder="#6b7280"
          maxLength={7}
          className="h-7 w-24 rounded border border-input bg-background px-2 text-xs font-mono focus:outline-none focus:ring-1 focus:ring-ring"
        />
        <span className="text-xs text-muted-foreground">Custom hex</span>
      </div>
    </div>
  );
}
