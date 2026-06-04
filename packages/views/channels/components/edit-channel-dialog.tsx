// CEREBRO-PATCH(channels-edit-channel-dialog): edit channel name + description in one modal (FIR-2660)
"use client";

// Opens from the channel-detail header (pencil button). The dialog wraps the
// already-existing useUpdateChannel mutation and lets the caller change name
// and description in a single round-trip — inline rename only changes title,
// and there was no UI for description at all. DMs can never reach this dialog
// because the entry button is gated on display.isChannel in channel-detail.

import { useEffect, useState } from "react";
import { Hash } from "lucide-react";
import { toast } from "sonner";
import { useUpdateChannel } from "@multica/core/channels";
import type { Channel } from "@multica/core/types";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Button } from "@multica/ui/components/ui/button";

export interface EditChannelDialogProps {
  channel: Channel;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function EditChannelDialog({ channel, open, onOpenChange }: EditChannelDialogProps) {
  const [name, setName] = useState(channel.title);
  const [description, setDescription] = useState(channel.description ?? "");
  const update = useUpdateChannel();

  // Re-seed the form whenever the channel changes or the dialog re-opens, so
  // stale values from a prior edit attempt don't leak in.
  useEffect(() => {
    if (open) {
      setName(channel.title);
      setDescription(channel.description ?? "");
    }
  }, [open, channel.id, channel.title, channel.description]);

  const trimmedName = name.trim();
  const trimmedDescription = description.trim();
  const isDirty =
    trimmedName !== channel.title.trim() ||
    trimmedDescription !== (channel.description ?? "").trim();
  const canSave = trimmedName.length > 0 && isDirty && !update.isPending;

  const handleSave = () => {
    if (!canSave) return;
    const payload: { id: string; title?: string; description?: string } = { id: channel.id };
    if (trimmedName !== channel.title.trim()) payload.title = trimmedName;
    // Send description on every save so clearing it persists ('' wipes the
    // server-side value via useUpdateChannel's optimistic merge).
    if (trimmedDescription !== (channel.description ?? "").trim()) {
      payload.description = trimmedDescription;
    }
    update.mutate(payload, {
      onSuccess: () => {
        toast.success("Channel updated");
        onOpenChange(false);
      },
      onError: (err) => {
        toast.error(err instanceof Error ? err.message : "Failed to update channel");
      },
    });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-1.5">
            <Hash className="size-4 text-muted-foreground" />
            Edit channel
          </DialogTitle>
          <DialogDescription>
            Change the channel name and description. Participants are managed from the participants panel.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-2">
          <div className="space-y-1.5">
            <Label htmlFor="edit-channel-name">Name</Label>
            <Input
              id="edit-channel-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="channel-name"
              aria-label="Channel name"
              autoFocus
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="edit-channel-description">Description</Label>
            <Textarea
              id="edit-channel-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="What is this channel for?"
              aria-label="Channel description"
              rows={3}
            />
          </div>
        </div>

        <DialogFooter>
          <Button variant="ghost" type="button" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button type="button" onClick={handleSave} disabled={!canSave}>
            {update.isPending ? "Saving…" : "Save"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
