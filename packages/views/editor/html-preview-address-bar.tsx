"use client";

/**
 * HtmlPreviewAddressBar — reload and an address field for an HTML preview
 * (MUL-7737), driven by useHtmlPreviewLocation.
 *
 * The file name is the fixed part of the address; the reader edits the
 * query and fragment after it. The field shows where the document is until
 * the reader starts typing, and an edit that is not submitted is dropped on
 * blur or Escape, as in a browser's address bar.
 */

import { useState, type KeyboardEvent } from "react";
import { RotateCw } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  InputGroupText,
} from "@multica/ui/components/ui/input-group";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { normalizeHtmlPreviewAddress } from "./utils/iframe-location-bridge";

interface HtmlPreviewAddressBarProps {
  filename: string;
  address: string;
  onNavigate: (address: string) => void;
  onReload: () => void;
  className?: string;
}

export function HtmlPreviewAddressBar({
  filename,
  address,
  onNavigate,
  onReload,
  className,
}: HtmlPreviewAddressBarProps) {
  const { t } = useT("editor");
  // null while the reader is not editing: the field follows the document.
  const [draft, setDraft] = useState<string | null>(null);

  const handleKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key !== "Escape" || draft === null || draft === address) return;
    // Revert the edit; the Escape does not reach the viewer, which would close.
    e.stopPropagation();
    setDraft(null);
  };

  return (
    <div className={cn("flex items-center gap-1", className)}>
      <Button
        variant="ghost"
        size="icon-sm"
        className="text-muted-foreground"
        title={t(($) => $.attachment.reload)}
        aria-label={t(($) => $.attachment.reload)}
        onClick={onReload}
      >
        <RotateCw />
      </Button>
      <form
        className="min-w-0 flex-1"
        onSubmit={(e) => {
          e.preventDefault();
          onNavigate(normalizeHtmlPreviewAddress(draft ?? address, filename));
          setDraft(null);
        }}
      >
        <InputGroup>
          {filename && (
            <InputGroupAddon className="min-w-0 max-w-[50%]">
              <InputGroupText className="block truncate font-normal">{filename}</InputGroupText>
            </InputGroupAddon>
          )}
          <InputGroupInput
            // Flush against the file name: the group's gap would split the
            // address in two.
            className={cn(filename && "!pl-0")}
            value={draft ?? address}
            aria-label={t(($) => $.attachment.address)}
            spellCheck={false}
            autoComplete="off"
            autoCapitalize="off"
            autoCorrect="off"
            enterKeyHint="go"
            onChange={(e) => setDraft(e.target.value)}
            onFocus={(e) => e.currentTarget.select()}
            onBlur={() => setDraft(null)}
            onKeyDown={handleKeyDown}
          />
        </InputGroup>
      </form>
    </div>
  );
}
