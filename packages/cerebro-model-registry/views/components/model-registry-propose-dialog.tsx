"use client";

import { useEffect, useState } from "react";
import { Check, Loader2, Pencil, Plus, Send } from "lucide-react";
import { toast } from "sonner";
import type { ModelRegistryEntry } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import {
  useCreateModelRegistryChangeRequest,
  useReviewModelRegistryChangeRequest,
} from "../../core/mutations";

const EMPTY_ENTRY: ModelRegistryEntry = {
  label: "",
  provider: "",
  context_window: 0,
  input_usd_per_mtok: 0,
  output_usd_per_mtok: 0,
  cache_read_usd_per_mtok: 0,
  cache_write_usd_per_mtok: 0,
};

// bumpPatch returns the next strict-semver patch of a valid X.Y.Z, else 1.0.1.
function bumpPatch(v: string): string {
  const m = /^(\d+)\.(\d+)\.(\d+)$/.exec(v);
  if (!m) return "1.0.1";
  return `${m[1]}.${m[2]}.${Number(m[3]) + 1}`;
}

interface Props {
  currentVersion: string;
  /** Set when editing an existing row; undefined when adding a new model. */
  modelId?: string;
  entry?: ModelRegistryEntry;
  /** True when the current user can approve (owner/approver/admin). Unlocks
   *  the one-step "Save & approve" action. */
  canReview?: boolean;
}

export function ModelRegistryProposeDialog({
  currentVersion,
  modelId,
  entry,
  canReview = false,
}: Props) {
  const isEdit = !!modelId;
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [id, setId] = useState(modelId ?? "");
  const [fields, setFields] = useState<ModelRegistryEntry>(entry ?? EMPTY_ENTRY);

  const proposedVersion = bumpPatch(currentVersion);
  const mutation = useCreateModelRegistryChangeRequest();
  const reviewMutation = useReviewModelRegistryChangeRequest();
  const busy = mutation.isPending || reviewMutation.isPending;

  useEffect(() => {
    if (open) {
      setId(modelId ?? "");
      setFields(entry ?? EMPTY_ENTRY);
      setTitle("");
      setDescription("");
    }
    // Only reset when the dialog opens, not on every keystroke.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const setField = (key: keyof ModelRegistryEntry, value: string) => {
    if (key === "label" || key === "provider") {
      setFields((f) => ({ ...f, [key]: value }));
      return;
    }
    const n = Number(value);
    setFields((f) => ({ ...f, [key]: Number.isFinite(n) ? n : 0 }));
  };

  const normalizedId = id.trim().toLowerCase();
  const canSubmit = normalizedId.length > 0 && fields.label.trim().length > 0 && title.trim().length > 0;

  const submit = async (approve: boolean) => {
    if (!canSubmit) return;
    try {
      const cr = await mutation.mutateAsync({
        title: title.trim(),
        description: description.trim() || undefined,
        proposed_version: proposedVersion,
        set_models: { [normalizedId]: fields },
      });
      if (approve) {
        await reviewMutation.mutateAsync({
          crId: cr.id,
          data: { action: "approve" },
        });
        toast.success(`Change approved — now live as ${proposedVersion}.`);
      } else {
        toast.success("Change proposed for review.");
      }
      setOpen(false);
    } catch {
      toast.error(approve ? "Failed to save & approve" : "Failed to submit proposal");
    }
  };

  return (
    <>
      {isEdit ? (
        <Button
          type="button"
          size="xs"
          variant="ghost"
          className="h-6 gap-1 px-2"
          onClick={() => setOpen(true)}
        >
          <Pencil className="h-3 w-3" />
          Edit
        </Button>
      ) : (
        <Button
          type="button"
          size="xs"
          className="gap-1 bg-blue-600 text-white hover:bg-blue-700"
          onClick={() => setOpen(true)}
        >
          <Plus className="h-3 w-3" />
          Add model
        </Button>
      )}

      <Dialog open={open} onOpenChange={(v) => !busy && setOpen(v)}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>
              {isEdit ? `Propose a change to ${modelId}` : "Propose a new model"}
            </DialogTitle>
            <DialogDescription>
              Submits a versioned change request ({currentVersion} →{" "}
              {proposedVersion}). The owner/approvers review the diff before it
              applies to the live registry that both the backend and every
              cost estimate in the app read from.
            </DialogDescription>
          </DialogHeader>

          <div className="max-h-[65vh] space-y-4 overflow-y-auto pr-1">
            <div className="space-y-1.5">
              <Label htmlFor="mr-title" className="text-xs">
                Title <span className="text-destructive">*</span>
              </Label>
              <Input
                id="mr-title"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder="e.g. Add gpt-5.6 pricing"
                className="h-8 text-sm"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="mr-desc" className="text-xs">
                Rationale <span className="text-muted-foreground">(optional)</span>
              </Label>
              <Textarea
                id="mr-desc"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="Why this change is correct…"
                rows={2}
                className="resize-none text-sm"
              />
            </div>

            <div className="h-px bg-border" />

            <div className="grid grid-cols-2 gap-3">
              <div className="col-span-2 space-y-1.5">
                <Label htmlFor="mr-id" className="text-xs">
                  Model id <span className="text-destructive">*</span>
                </Label>
                <Input
                  id="mr-id"
                  value={id}
                  onChange={(e) => setId(e.target.value)}
                  disabled={isEdit}
                  placeholder="e.g. claude-opus-4-9"
                  className="h-8 font-mono text-sm"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="mr-label" className="text-xs">
                  Display label <span className="text-destructive">*</span>
                </Label>
                <Input
                  id="mr-label"
                  value={fields.label}
                  onChange={(e) => setField("label", e.target.value)}
                  className="h-8 text-sm"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="mr-provider" className="text-xs">
                  Provider
                </Label>
                <Input
                  id="mr-provider"
                  value={fields.provider}
                  onChange={(e) => setField("provider", e.target.value)}
                  placeholder="anthropic / openai / google …"
                  className="h-8 text-sm"
                />
              </div>
              <div className="col-span-2 space-y-1.5">
                <Label htmlFor="mr-context" className="text-xs">
                  Context window (tokens, 0 = not curated)
                </Label>
                <Input
                  id="mr-context"
                  type="number"
                  min={0}
                  value={fields.context_window}
                  onChange={(e) => setField("context_window", e.target.value)}
                  className="h-8 text-sm"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="mr-input" className="text-xs">
                  Input $/Mtok
                </Label>
                <Input
                  id="mr-input"
                  type="number"
                  min={0}
                  step="any"
                  value={fields.input_usd_per_mtok}
                  onChange={(e) => setField("input_usd_per_mtok", e.target.value)}
                  className="h-8 text-sm"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="mr-output" className="text-xs">
                  Output $/Mtok
                </Label>
                <Input
                  id="mr-output"
                  type="number"
                  min={0}
                  step="any"
                  value={fields.output_usd_per_mtok}
                  onChange={(e) => setField("output_usd_per_mtok", e.target.value)}
                  className="h-8 text-sm"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="mr-cache-read" className="text-xs">
                  Cache read $/Mtok
                </Label>
                <Input
                  id="mr-cache-read"
                  type="number"
                  min={0}
                  step="any"
                  value={fields.cache_read_usd_per_mtok}
                  onChange={(e) => setField("cache_read_usd_per_mtok", e.target.value)}
                  className="h-8 text-sm"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="mr-cache-write" className="text-xs">
                  Cache write $/Mtok
                </Label>
                <Input
                  id="mr-cache-write"
                  type="number"
                  min={0}
                  step="any"
                  value={fields.cache_write_usd_per_mtok}
                  onChange={(e) => setField("cache_write_usd_per_mtok", e.target.value)}
                  className="h-8 text-sm"
                />
              </div>
            </div>
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => setOpen(false)}
              disabled={busy}
            >
              Cancel
            </Button>
            <Button
              type="button"
              size="sm"
              variant={canReview ? "outline" : "default"}
              onClick={() => submit(false)}
              disabled={busy || !canSubmit}
            >
              {mutation.isPending && !reviewMutation.isPending ? (
                <>
                  <Loader2 className="h-3 w-3 animate-spin" />
                  Submitting…
                </>
              ) : (
                <>
                  <Send className="h-3 w-3" />
                  {canReview ? "Propose only" : "Submit proposal"}
                </>
              )}
            </Button>
            {canReview && (
              <Button
                type="button"
                size="sm"
                onClick={() => submit(true)}
                disabled={busy || !canSubmit}
              >
                {reviewMutation.isPending ? (
                  <>
                    <Loader2 className="h-3 w-3 animate-spin" />
                    Saving…
                  </>
                ) : (
                  <>
                    <Check className="h-3 w-3" />
                    Save &amp; approve
                  </>
                )}
              </Button>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
