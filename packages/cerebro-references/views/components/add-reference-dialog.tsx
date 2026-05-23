"use client";

import { useMemo, useState } from "react";
import { toast } from "sonner";
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
import { Label } from "@multica/ui/components/ui/label";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";
import {
  listRegisteredObjects,
  parseGithubPr,
  useAddReference,
  type CreateReferenceInput,
  type ReferenceObject,
} from "@multica/cerebro-references/core";

// Human labels for the object dropdown. Anything registered but unmapped
// falls through to its raw wire string, so a newly-registered object kind
// is selectable the moment its renderer loads — no edit here required.
const OBJECT_LABELS: Record<string, string> = {
  github_pr: "GitHub PR",
};

function objectLabel(object: ReferenceObject): string {
  return OBJECT_LABELS[object] ?? object;
}

// Placeholder hint per object, falling back to a generic one.
function objectPlaceholder(object: ReferenceObject): string {
  if (object === "github_pr") return "firtal-group/firtal-cerebro#42";
  return "Identifier";
}

// Builds the CreateReferenceInput for the chosen object. For github_pr we
// parse the entered identifier so we can store a normalized ref_id + the
// resolved github.com URL. Returns an error string when input is invalid.
function buildInput(
  object: ReferenceObject,
  rawRefId: string,
): { input: CreateReferenceInput } | { error: string } {
  const refId = rawRefId.trim();
  if (!refId) return { error: "Enter a reference identifier." };

  if (object === "github_pr") {
    const parsed = parseGithubPr(refId);
    if (!parsed) {
      return {
        error: "Use org/repo#123 or a github.com pull-request URL.",
      };
    }
    return {
      input: {
        object,
        ref_id: `${parsed.org}/${parsed.repo}#${parsed.number}`,
        url: parsed.url,
        label: `${parsed.org}/${parsed.repo}#${parsed.number}`,
      },
    };
  }

  // Generic / unknown object kind: store the identifier as-is. The fallback
  // renderer handles display, so this still produces a usable reference.
  return { input: { object, ref_id: refId } };
}

export function AddReferenceDialog({
  issueId,
  open,
  onOpenChange,
}: {
  issueId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const objects = useMemo(() => {
    const registered = listRegisteredObjects();
    // Guarantee github_pr is offered even if registration order shifts.
    return registered.includes("github_pr")
      ? registered
      : ["github_pr", ...registered];
  }, []);

  const [object, setObject] = useState<ReferenceObject>(objects[0] ?? "github_pr");
  const [refId, setRefId] = useState("");
  const [error, setError] = useState<string | null>(null);

  const addReference = useAddReference(issueId);

  const reset = () => {
    setObject(objects[0] ?? "github_pr");
    setRefId("");
    setError(null);
  };

  const handleOpenChange = (next: boolean) => {
    if (!next) reset();
    onOpenChange(next);
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    const built = buildInput(object, refId);
    if ("error" in built) {
      setError(built.error);
      return;
    }
    // UPSERT — the backend de-dupes on (issue, object, type, ref_id), so
    // re-adding the same identifier updates rather than duplicates. The
    // mutation invalidates referenceKeys.byIssue on settle.
    addReference.mutate(built.input, {
      onSuccess: () => {
        toast.success("Reference added");
        handleOpenChange(false);
      },
      onError: () => {
        setError("Could not add the reference. Try again.");
      },
    });
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>Add reference</DialogTitle>
            <DialogDescription>
              Link an external object — like a GitHub pull request — to this issue.
            </DialogDescription>
          </DialogHeader>

          <div className="mt-4 flex flex-col gap-4">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="reference-object">Type</Label>
              <NativeSelect
                id="reference-object"
                className="w-full"
                value={object}
                onChange={(e) => setObject(e.target.value)}
              >
                {objects.map((o) => (
                  <NativeSelectOption key={o} value={o}>
                    {objectLabel(o)}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </div>

            <div className="flex flex-col gap-1.5">
              <Label htmlFor="reference-ref-id">Reference</Label>
              <Input
                id="reference-ref-id"
                autoFocus
                value={refId}
                onChange={(e) => setRefId(e.target.value)}
                placeholder={objectPlaceholder(object)}
              />
              {error && (
                <p className="text-xs text-destructive" role="alert">
                  {error}
                </p>
              )}
            </div>
          </div>

          <DialogFooter className="mt-6">
            <Button
              type="button"
              variant="ghost"
              onClick={() => handleOpenChange(false)}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={addReference.isPending}>
              {addReference.isPending ? "Adding…" : "Add reference"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
