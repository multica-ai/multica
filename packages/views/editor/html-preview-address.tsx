"use client";

/**
 * HtmlPreviewAddress — an HTML file's name in the viewer's top bar, as the
 * address the document is at (MUL-7737), driven by useHtmlPreviewLocation.
 *
 * The file name is the fixed part; the query and fragment after it are the
 * editable part. A document at its bare address reads as just the file name,
 * so files that never use an address look as they always did; hovering or
 * focusing shows the field. The field follows the document until the reader
 * starts typing, and an edit that is not submitted is dropped on blur or
 * Escape, as in a browser's address bar.
 */

import {
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent,
} from "react";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { normalizeHtmlPreviewAddress } from "./utils/iframe-location-bridge";

interface HtmlPreviewAddressProps {
  filename: string;
  address: string;
  onNavigate: (address: string) => void;
  className?: string;
  style?: CSSProperties;
}

export function HtmlPreviewAddress({
  filename,
  address,
  onNavigate,
  className,
  style,
}: HtmlPreviewAddressProps) {
  const { t } = useT("editor");
  const inputRef = useRef<HTMLInputElement>(null);
  // null while the reader is not editing: the field follows the document.
  const [draft, setDraft] = useState<string | null>(null);
  const value = draft ?? address;
  const hint = t(($) => $.attachment.address_hint);

  const handleKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key !== "Escape" || draft === null || draft === address) return;
    // Revert the edit; the Escape does not reach the viewer, which would close.
    e.stopPropagation();
    setDraft(null);
  };

  return (
    <form
      className={cn(
        "group/address -mx-1.5 flex h-6 min-w-0 cursor-text items-center rounded-md px-1.5 text-body transition-colors hover:bg-secondary focus-within:bg-secondary focus-within:ring-2 focus-within:ring-ring/50",
        className,
      )}
      style={style}
      // Anywhere on the field edits the address, the file name included.
      onMouseDown={(e) => {
        if (e.target === inputRef.current) return;
        e.preventDefault();
        inputRef.current?.focus();
      }}
      onSubmit={(e) => {
        e.preventDefault();
        onNavigate(normalizeHtmlPreviewAddress(value, filename));
        setDraft(null);
      }}
    >
      <span className="min-w-0 truncate font-medium">{filename}</span>
      {/* Sized by an invisible copy of its text, so the address sits flush
          against the file name. Empty, it takes no room until the field is
          hovered or focused, when the hint shows where to type. */}
      <span
        data-empty={value === "" || undefined}
        className="inline-grid min-w-0 max-w-[70%] shrink-0 overflow-hidden data-empty:w-0 group-hover/address:data-empty:w-auto group-focus-within/address:data-empty:w-auto"
      >
        <span
          aria-hidden
          className="invisible col-start-1 row-start-1 whitespace-pre"
        >
          {value || hint}
        </span>
        <input
          ref={inputRef}
          // No intrinsic width of its own: the copy above sizes the field.
          size={1}
          value={value}
          placeholder={hint}
          aria-label={t(($) => $.attachment.address)}
          spellCheck={false}
          autoComplete="off"
          autoCapitalize="off"
          autoCorrect="off"
          enterKeyHint="go"
          className="col-start-1 row-start-1 w-full min-w-0 bg-transparent text-foreground outline-none placeholder:text-muted-foreground"
          onChange={(e) => setDraft(e.target.value)}
          onFocus={(e) => e.currentTarget.select()}
          onBlur={() => setDraft(null)}
          onKeyDown={handleKeyDown}
        />
      </span>
    </form>
  );
}
