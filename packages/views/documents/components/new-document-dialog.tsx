"use client";

import { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogClose,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

const PATH_REGEX = /^[a-z0-9][a-z0-9/_-]*\.md$/;

interface NewDocumentDialogProps {
  /** Prefilled path prefix from the currently selected folder */
  pathPrefix?: string;
  onClose: () => void;
  onCreated: (path: string, title?: string, description?: string) => void;
  isPending?: boolean;
}

export function NewDocumentDialog({
  pathPrefix,
  onClose,
  onCreated,
  isPending,
}: NewDocumentDialogProps) {
  const { t } = useT("documents");
  const [path, setPath] = useState(pathPrefix ? `${pathPrefix}/` : "");
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [error, setError] = useState<string | null>(null);

  const validate = (value: string): string | null => {
    if (!value.trim()) return t(($) => $.create_dialog.errors.empty_path);
    if (value.startsWith("/")) return t(($) => $.create_dialog.errors.absolute_path);
    if (value.includes("..")) return t(($) => $.create_dialog.errors.double_dot);
    if (!PATH_REGEX.test(value)) return t(($) => $.create_dialog.errors.invalid_path);
    return null;
  };

  const handleSubmit = () => {
    const err = validate(path);
    if (err) {
      setError(err);
      return;
    }
    onCreated(path, title || undefined, description || undefined);
  };

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose(); }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.create_dialog.title)}</DialogTitle>
        </DialogHeader>

        <div className="space-y-3 py-2">
          <div>
            <Label className="text-xs text-muted-foreground">
              {t(($) => $.create_dialog.path_label)}
            </Label>
            <Input
              value={path}
              onChange={(e) => {
                setPath(e.target.value);
                setError(null);
              }}
              placeholder={t(($) => $.create_dialog.path_placeholder)}
              className="mt-1 font-mono text-sm"
              autoFocus
            />
            {error ? (
              <p className="mt-1 text-xs text-destructive">{error}</p>
            ) : (
              <p className="mt-1 text-[10px] text-muted-foreground">
                {t(($) => $.create_dialog.path_hint)}
              </p>
            )}
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">
              {t(($) => $.create_dialog.title_label)}
            </Label>
            <Input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder={t(($) => $.create_dialog.title_placeholder)}
              className="mt-1"
            />
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">
              {t(($) => $.create_dialog.description_label)}
            </Label>
            <Textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t(($) => $.create_dialog.description_placeholder)}
              rows={2}
              className="mt-1 resize-none"
            />
          </div>
        </div>

        <DialogFooter>
          <DialogClose render={<Button variant="outline" size="sm" />}>
            {t(($) => $.create_dialog.cancel)}
          </DialogClose>
          <Button
            size="sm"
            onClick={handleSubmit}
            disabled={isPending || !path.trim()}
          >
            {isPending
              ? t(($) => $.create_dialog.creating)
              : t(($) => $.create_dialog.create)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
