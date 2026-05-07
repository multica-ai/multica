"use client";

import { useRef } from "react";
import { ImagePlus, Paperclip } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";

interface FileUploadButtonProps {
  /** Default attach handler — called with any file the user picks. */
  onSelect: (file: File) => void;
  /** Optional embed-as-image handler. When provided, a second button is
   *  rendered that opens an image-only picker and routes to this callback. */
  onEmbedImage?: (file: File) => void;
  disabled?: boolean;
  className?: string;
  size?: "sm" | "default";
}

function FileUploadButton({
  onSelect,
  onEmbedImage,
  disabled,
  className,
  size = "default",
}: FileUploadButtonProps) {
  const attachInputRef = useRef<HTMLInputElement>(null);
  const embedInputRef = useRef<HTMLInputElement>(null);

  const handleAttach = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    e.target.value = "";
    onSelect(file);
  };

  const handleEmbed = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    e.target.value = "";
    onEmbedImage?.(file);
  };

  const iconSize = size === "sm" ? "h-3.5 w-3.5" : "h-4 w-4";
  const btnSize = size === "sm" ? "h-6 w-6" : "h-7 w-7";
  const btnClass = cn(
    "inline-flex items-center justify-center rounded-full text-muted-foreground hover:bg-accent hover:text-foreground transition-colors disabled:opacity-50 disabled:pointer-events-none",
    btnSize,
  );

  return (
    <>
      <button
        type="button"
        onClick={() => attachInputRef.current?.click()}
        disabled={disabled}
        aria-label="Attach file"
        title="Attach file"
        className={cn(btnClass, className)}
      >
        <Paperclip className={iconSize} />
      </button>
      <input
        ref={attachInputRef}
        type="file"
        className="hidden"
        onChange={handleAttach}
      />
      {onEmbedImage && (
        <>
          <button
            type="button"
            onClick={() => embedInputRef.current?.click()}
            disabled={disabled}
            title="Embed image"
            className={btnClass}
          >
            <ImagePlus className={iconSize} />
          </button>
          <input
            ref={embedInputRef}
            type="file"
            accept="image/*"
            className="hidden"
            onChange={handleEmbed}
          />
        </>
      )}
    </>
  );
}

export { FileUploadButton, type FileUploadButtonProps };
