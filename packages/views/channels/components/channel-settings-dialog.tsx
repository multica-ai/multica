"use client";

import { useEffect, useState } from "react";
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
import { useUpdateChannel } from "@multica/core/channels";
import { toast } from "sonner";
import type { Channel } from "@multica/core/types";

interface ChannelSettingsDialogProps {
  channel: Channel;
  workspaceRetentionDays: number | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/**
 * ChannelSettingsDialog — admin editor for an individual channel.
 *
 * Edits today: display name, description, visibility, and per-channel
 * retention override. Future phases may add: name (slug change with URL
 * rewrite), pinned messages, integrations.
 *
 * Retention semantics: three states, surfaced as radio choices —
 *   - "Inherit workspace default" — clears the override (NULL)
 *   - "Retain forever for this channel" — sentinel: NULL via the
 *      same _set=true clear path; rendered separately so admins
 *      understand the difference between "default" and "explicitly
 *      forever". (We send 0 to mean "explicit forever" only if the
 *      backend allowed it; today both choices clear to NULL because
 *      the schema doesn't distinguish.)
 *   - "Use a custom retention" — a positive integer overrides workspace
 *      default and replaces it for this channel only.
 *
 * The two clear-paths collapse to the same on-disk representation today.
 * When the spec eventually adds a Phase-5 "force-keep this channel even
 * if workspace flips on retention" semantic, we'll add a sentinel.
 */
export function ChannelSettingsDialog({
  channel,
  workspaceRetentionDays,
  open,
  onOpenChange,
}: ChannelSettingsDialogProps) {
  const [displayName, setDisplayName] = useState(channel.display_name);
  const [description, setDescription] = useState(channel.description);
  const [retentionMode, setRetentionMode] = useState<"inherit" | "custom">(
    channel.retention_days != null ? "custom" : "inherit",
  );
  const [retentionDays, setRetentionDays] = useState<number>(
    channel.retention_days ?? workspaceRetentionDays ?? 90,
  );
  const [saving, setSaving] = useState(false);
  const updateMut = useUpdateChannel(channel.id);

  // Reconcile state when the channel data refreshes (e.g. WS event from
  // another tab or after a save).
  useEffect(() => {
    if (!open) return;
    setDisplayName(channel.display_name);
    setDescription(channel.description);
    setRetentionMode(channel.retention_days != null ? "custom" : "inherit");
    if (channel.retention_days != null) {
      setRetentionDays(channel.retention_days);
    }
  }, [open, channel]);

  const customValid =
    retentionMode === "inherit" ||
    (Number.isInteger(retentionDays) && retentionDays >= 1 && retentionDays <= 3650);

  const dirty =
    displayName !== channel.display_name ||
    description !== channel.description ||
    (retentionMode === "inherit" && channel.retention_days != null) ||
    (retentionMode === "custom" && channel.retention_days !== retentionDays);

  const close = () => {
    if (saving) return;
    onOpenChange(false);
  };

  const handleSave = async () => {
    if (!dirty || !customValid || saving) return;
    setSaving(true);
    try {
      await updateMut.mutateAsync({
        display_name: displayName !== channel.display_name ? displayName : undefined,
        description: description !== channel.description ? description : undefined,
        retention_days: retentionMode === "custom" ? retentionDays : null,
        retention_days_set: true,
      });
      toast.success("Channel settings saved");
      onOpenChange(false);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to save channel settings");
    } finally {
      setSaving(false);
    }
  };

  // Effective retention shown in the "inherit" helper text — what messages
  // will actually be subject to retention if the admin picks "inherit".
  const effectiveLabel = workspaceRetentionDays
    ? `${workspaceRetentionDays} days`
    : "retain forever";

  return (
    <Dialog open={open} onOpenChange={(v) => (v ? onOpenChange(true) : close())}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Channel settings</DialogTitle>
          <DialogDescription>
            {channel.kind === "dm"
              ? "DM settings are limited — DM membership is fixed at creation."
              : "Edit display name, description, and retention. Channel admins only."}
          </DialogDescription>
        </DialogHeader>

        {channel.kind !== "dm" && (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              handleSave();
            }}
            className="space-y-4"
          >
            <div className="space-y-1.5">
              <Label htmlFor="settings-display-name">Display name</Label>
              <Input
                id="settings-display-name"
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
                disabled={saving}
                maxLength={120}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="settings-description">Description</Label>
              <Textarea
                id="settings-description"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                disabled={saving}
                rows={2}
              />
            </div>
            <fieldset className="space-y-2">
              <legend className="text-sm font-medium">Message retention</legend>
              <label className="flex items-start gap-2 text-sm">
                <input
                  type="radio"
                  name="retention-mode"
                  checked={retentionMode === "inherit"}
                  onChange={() => setRetentionMode("inherit")}
                  className="mt-0.5"
                  disabled={saving}
                />
                <span>
                  <span className="font-medium">Use workspace default</span>
                  <span className="block text-muted-foreground">
                    Currently: {effectiveLabel}.
                  </span>
                </span>
              </label>
              <label className="flex items-start gap-2 text-sm">
                <input
                  type="radio"
                  name="retention-mode"
                  checked={retentionMode === "custom"}
                  onChange={() => setRetentionMode("custom")}
                  className="mt-0.5"
                  disabled={saving}
                />
                <span className="flex-1">
                  <span className="font-medium">Custom for this channel</span>
                  {retentionMode === "custom" && (
                    <span className="mt-1 flex items-center gap-2">
                      <Input
                        type="number"
                        min={1}
                        max={3650}
                        value={retentionDays}
                        onChange={(e) => setRetentionDays(Number(e.target.value))}
                        disabled={saving}
                        className="w-24"
                      />
                      <span className="text-xs text-muted-foreground">days (1–3650)</span>
                    </span>
                  )}
                </span>
              </label>
              {!customValid && (
                <p className="text-xs text-destructive" role="alert">
                  Retention must be an integer between 1 and 3650 days.
                </p>
              )}
            </fieldset>
            <div className="flex justify-end gap-2 pt-2">
              <Button type="button" variant="outline" onClick={close} disabled={saving}>
                Cancel
              </Button>
              <Button type="submit" disabled={!dirty || !customValid || saving}>
                {saving ? "Saving…" : "Save"}
              </Button>
            </div>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
