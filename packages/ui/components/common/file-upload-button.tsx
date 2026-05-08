"use client";

// CEREBRO-PATCH(file-upload-button-cerebro): cerebro modification of upstream file
// Adds multi-select picker + per-batch popup for "embed image inline vs attach
// as file card", on top of upstream's single-file paperclip button.

import { useRef, useState } from "react";
import { Paperclip } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";

interface FileUploadButtonProps {
  /** Default attach handler — called for any picked files that are not images,
   *  or for images the user chooses to attach. */
  onAttach: (files: File[]) => void;
  /** Optional embed-as-image handler. When provided, picking image files
   *  triggers a popup that lets the user choose embed inline vs attach. */
  onEmbed?: (files: File[]) => void;
  disabled?: boolean;
  className?: string;
  size?: "sm" | "default";
}

function FileUploadButton({
  onAttach,
  onEmbed,
  disabled,
  className,
  size = "default",
}: FileUploadButtonProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [pendingImages, setPendingImages] = useState<File[]>([]);
  const dialogOpen = pendingImages.length > 0;

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files;
    if (!files?.length) return;
    e.target.value = "";

    const arr = Array.from(files);
    if (!onEmbed) {
      onAttach(arr);
      return;
    }

    // Split images from other files. Non-images skip the popup since "embed
    // inline" only applies to images.
    const images: File[] = [];
    const nonImages: File[] = [];
    for (const f of arr) {
      (f.type.startsWith("image/") ? images : nonImages).push(f);
    }
    if (nonImages.length > 0) onAttach(nonImages);
    if (images.length > 0) setPendingImages(images);
  };

  const handleChoice = (mode: "embed" | "attach") => {
    if (mode === "embed") onEmbed?.(pendingImages);
    else onAttach(pendingImages);
    setPendingImages([]);
  };

  const iconSize = size === "sm" ? "h-3.5 w-3.5" : "h-4 w-4";
  // Mobile gets a 36×36 hit target (≥ recommended 44pt with default padding
  // around the icon) so users can tap upload without accidentally hitting the
  // adjacent send button. Desktop keeps the compact size.
  const btnSize =
    size === "sm" ? "h-9 w-9 sm:h-6 sm:w-6" : "h-9 w-9 sm:h-7 sm:w-7";
  const btnClass = cn(
    "inline-flex items-center justify-center rounded-full text-muted-foreground hover:bg-accent hover:text-foreground transition-colors disabled:opacity-50 disabled:pointer-events-none",
    btnSize,
    className,
  );

  return (
    <>
      <button
        type="button"
        onClick={() => inputRef.current?.click()}
        disabled={disabled}
        title="Add file"
        className={btnClass}
      >
        <Paperclip className={iconSize} />
      </button>
      <input
        ref={inputRef}
        type="file"
        multiple
        className="hidden"
        onChange={handleChange}
      />
      <Dialog
        open={dialogOpen}
        onOpenChange={(open) => {
          if (!open) setPendingImages([]);
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>
              {pendingImages.length === 1
                ? "Insert image as…"
                : `Insert ${pendingImages.length} images as…`}
            </DialogTitle>
            <DialogDescription>
              You can change this per image afterwards from the hover toolbar.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter className="gap-2 sm:gap-2">
            <Button variant="outline" onClick={() => handleChoice("attach")}>
              Attach as file
            </Button>
            <Button onClick={() => handleChoice("embed")}>Embed inline</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

export { FileUploadButton, type FileUploadButtonProps };
