"use client";

import { useState, useMemo } from "react";
import type { RuntimeDevice, MemberWithUser } from "@multica/core/types";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { toast } from "sonner";
import { ProviderLogo } from "../runtimes/components/provider-logo";

export function CreateRuntimeGroupDialog({
  runtimes,
  members: _members,
  currentUserId: _currentUserId,
  onClose,
  onCreate,
}: {
  runtimes: RuntimeDevice[];
  members: MemberWithUser[];
  currentUserId: string | null;
  onClose: () => void;
  onCreate: (req: { name: string; description: string; runtime_ids: string[] }) => Promise<void>;
}) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [selectedIds, setSelectedIds] = useState<string[]>([]);
  const [addOpen, setAddOpen] = useState(false);
  const [saving, setSaving] = useState(false);

  const selected = useMemo(
    () =>
      selectedIds
        .map((id) => runtimes.find((r) => r.id === id))
        .filter((r): r is RuntimeDevice => Boolean(r)),
    [selectedIds, runtimes],
  );
  const candidates = useMemo(
    () => runtimes.filter((r) => !selectedIds.includes(r.id)),
    [runtimes, selectedIds],
  );

  const canSave = !saving && name.trim().length > 0 && selectedIds.length > 0;

  const handleSubmit = async () => {
    if (!canSave) return;
    setSaving(true);
    try {
      await onCreate({
        name: name.trim(),
        description: description.trim(),
        runtime_ids: selectedIds,
      });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to create group");
      setSaving(false);
    }
  };

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>New Runtime Group</DialogTitle>
          <DialogDescription>
            A named group of runtimes you can assign to agents.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div>
            <Label className="text-xs text-muted-foreground">Name</Label>
            <Input
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Backend Team"
              className="mt-1"
            />
          </div>
          <div>
            <Label className="text-xs text-muted-foreground">Description</Label>
            <Input
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Optional"
              className="mt-1"
            />
          </div>
          <div>
            <Label className="text-xs text-muted-foreground">Runtimes</Label>
            <div className="mt-1.5 flex flex-wrap gap-2">
              {selected.map((d) => (
                <div
                  key={d.id}
                  className="flex items-center gap-2 rounded-lg border border-border bg-background px-3 py-2 text-sm"
                >
                  <ProviderLogo provider={d.provider} className="h-4 w-4 shrink-0" />
                  <span className="truncate font-medium">{d.name}</span>
                  <button
                    type="button"
                    aria-label={`Remove ${d.name}`}
                    onClick={() => setSelectedIds((ids) => ids.filter((id) => id !== d.id))}
                    className="ml-1 text-muted-foreground hover:text-foreground"
                  >
                    ×
                  </button>
                </div>
              ))}
              <Popover open={addOpen} onOpenChange={setAddOpen}>
                <PopoverTrigger
                  disabled={candidates.length === 0}
                  className="rounded-lg border border-dashed border-border bg-background px-3 py-2 text-sm text-muted-foreground transition-colors hover:bg-muted disabled:pointer-events-none disabled:opacity-50"
                >
                  + Add runtime
                </PopoverTrigger>
                <PopoverContent align="start" className="w-72 p-1 max-h-60 overflow-y-auto">
                  {candidates.map((d) => (
                    <button
                      key={d.id}
                      role="menuitem"
                      onClick={() => {
                        setSelectedIds((ids) => [...ids, d.id]);
                        setAddOpen(false);
                      }}
                      className="flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-left text-sm hover:bg-accent/50"
                    >
                      <ProviderLogo provider={d.provider} className="h-4 w-4 shrink-0" />
                      <span className="truncate font-medium">{d.name}</span>
                    </button>
                  ))}
                </PopoverContent>
              </Popover>
            </div>
            {selectedIds.length === 0 && (
              <p className="mt-2 text-xs text-destructive">At least one runtime is required.</p>
            )}
          </div>
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={!canSave}>
            {saving ? "Creating..." : "Create"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
