"use client";

import { useState, useTransition } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useCreateChannel } from "@multica/core/channels";
import { useNavigation } from "../../navigation";
import { useRequiredWorkspaceSlug, paths } from "@multica/core/paths";
import { useT } from "../../i18n";

interface ChannelCreateDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/**
 * ChannelCreateDialog — modal for creating a new public/private channel.
 *
 * Validation matches the server: lowercase, no whitespace, 1-80 chars.
 * We sanitize the name client-side (lowercasing, replacing spaces with
 * dashes) to remove a small UX paper-cut where the server would reject
 * "General Chat" as having uppercase + whitespace.
 */
export function ChannelCreateDialog({ open, onOpenChange }: ChannelCreateDialogProps) {
  const { t } = useT("channels");
  const slug = useRequiredWorkspaceSlug();
  const navigation = useNavigation();
  const [name, setName] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [description, setDescription] = useState("");
  const [visibility, setVisibility] = useState<"public" | "private">("public");
  const [error, setError] = useState<string | null>(null);
  const [isPending, startTransition] = useTransition();
  const createMut = useCreateChannel();

  const sanitizedName = name.toLowerCase().replace(/\s+/g, "-").replace(/[^a-z0-9_-]/g, "");

  const handleClose = () => {
    if (createMut.isPending) return;
    setName("");
    setDisplayName("");
    setDescription("");
    setVisibility("public");
    setError(null);
    onOpenChange(false);
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (!sanitizedName) {
      setError(t(($) => $.create_dialog.name_required));
      return;
    }
    startTransition(() => {
      createMut.mutate(
        {
          name: sanitizedName,
          display_name: displayName.trim() || sanitizedName,
          description: description.trim(),
          visibility,
        },
        {
          onSuccess: (channel) => {
            handleClose();
            navigation.push(paths.workspace(slug).channelDetail(channel.id));
          },
          onError: (err: unknown) => {
            const msg = err instanceof Error ? err.message : t(($) => $.create_dialog.submit_failed);
            setError(msg);
          },
        },
      );
    });
  };

  return (
    <Dialog open={open} onOpenChange={(v) => (v ? onOpenChange(true) : handleClose())}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.create_dialog.title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.create_dialog.description)}
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="channel-name">{t(($) => $.create_dialog.name_label)}</Label>
            <Input
              id="channel-name"
              placeholder={t(($) => $.create_dialog.name_placeholder)}
              value={name}
              onChange={(e) => setName(e.target.value)}
              autoFocus
              maxLength={80}
            />
            {sanitizedName && sanitizedName !== name && (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.create_dialog.saved_as)}{" "}
                <code className="font-mono">{sanitizedName}</code>
              </p>
            )}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="channel-display">{t(($) => $.create_dialog.display_label)}</Label>
            <Input
              id="channel-display"
              placeholder={t(($) => $.create_dialog.display_placeholder)}
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              maxLength={120}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="channel-desc">{t(($) => $.create_dialog.description_label)}</Label>
            <Textarea
              id="channel-desc"
              placeholder={t(($) => $.create_dialog.description_placeholder)}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={2}
            />
          </div>
          <fieldset className="space-y-2">
            <legend className="text-sm font-medium">{t(($) => $.create_dialog.visibility_label)}</legend>
            <label className="flex items-start gap-2 text-sm">
              <input
                type="radio"
                name="visibility"
                value="public"
                checked={visibility === "public"}
                onChange={() => setVisibility("public")}
                className="mt-0.5"
              />
              <span>
                <span className="font-medium">{t(($) => $.create_dialog.public_label)}</span>
                <span className="block text-muted-foreground">
                  {t(($) => $.create_dialog.public_hint)}
                </span>
              </span>
            </label>
            <label className="flex items-start gap-2 text-sm">
              <input
                type="radio"
                name="visibility"
                value="private"
                checked={visibility === "private"}
                onChange={() => setVisibility("private")}
                className="mt-0.5"
              />
              <span>
                <span className="font-medium">{t(($) => $.create_dialog.private_label)}</span>
                <span className="block text-muted-foreground">
                  {t(($) => $.create_dialog.private_hint)}
                </span>
              </span>
            </label>
          </fieldset>
          {error && (
            <p className="text-sm text-destructive" role="alert">
              {error}
            </p>
          )}
          <div className="flex justify-end gap-2 pt-2">
            <Button type="button" variant="outline" onClick={handleClose} disabled={createMut.isPending}>
              {t(($) => $.create_dialog.cancel)}
            </Button>
            <Button type="submit" disabled={!sanitizedName || createMut.isPending || isPending}>
              {createMut.isPending ? t(($) => $.create_dialog.creating) : t(($) => $.create_dialog.create)}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
