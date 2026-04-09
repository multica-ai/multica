"use client";

import { useState } from "react";
import { toast } from "sonner";
import { ArrowLeft } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { Card, CardContent } from "@/components/ui/card";
import { useWorkspaceStore } from "@/features/workspace";

const SLUG_REGEX = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

export function CreateWorkspaceModal({ onClose }: { onClose: () => void }) {
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [creating, setCreating] = useState(false);

  const slugError =
    slug.length > 0 && !SLUG_REGEX.test(slug)
      ? "Only lowercase letters, numbers, and hyphens"
      : null;

  const canSubmit = name.trim().length > 0 && slug.trim().length > 0 && !slugError;

  const handleNameChange = (value: string) => {
    setName(value);
    setSlug(
      value
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, "-")
        .replace(/^-|-$/g, ""),
    );
  };

  const handleCreate = async () => {
    if (!canSubmit) return;
    setCreating(true);
    try {
      const { createWorkspace, switchWorkspace } =
        useWorkspaceStore.getState();
      const ws = await createWorkspace({
        name: name.trim(),
        slug: slug.trim(),
      });
      onClose();
      await switchWorkspace(ws.id);
    } catch {
      toast.error("Failed to create workspace");
    } finally {
      setCreating(false);
    }
  };

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent
        showCloseButton={false}
        className="inset-0 flex h-full w-full max-w-none sm:max-w-none translate-0 flex-col items-center justify-center rounded-none bg-background ring-0 shadow-none"
      >
        <Button
          variant="ghost"
          size="sm"
          className="absolute top-6 left-6 text-muted-foreground"
          onClick={onClose}
        >
          <ArrowLeft className="h-4 w-4" />
          Back
        </Button>

        <div className="flex w-full max-w-md flex-col items-center gap-6">
          <div className="text-center">
            <DialogTitle className="text-2xl font-semibold">
              Create a new workspace
            </DialogTitle>
            <DialogDescription className="mt-2">
              Workspaces are shared environments where teams can work on
              projects and issues.
            </DialogDescription>
          </div>

          <Card className="w-full">
            <CardContent className="space-y-4 pt-6">
              <div className="space-y-1.5">
                <Label>Workspace Name</Label>
                <Input
                  autoFocus
                  type="text"
                  value={name}
                  onChange={(e) => handleNameChange(e.target.value)}
                  placeholder="My Workspace"
                />
              </div>
              <div className="space-y-1.5">
                <Label>Workspace URL</Label>
                <div className="flex items-center gap-0 rounded-md border bg-background focus-within:ring-2 focus-within:ring-ring">
                  <span className="pl-3 text-sm text-muted-foreground select-none">
                    multica.app/
                  </span>
                  <Input
                    type="text"
                    value={slug}
                    onChange={(e) => setSlug(e.target.value)}
                    placeholder="my-workspace"
                    className="border-0 shadow-none focus-visible:ring-0"
                  />
                </div>
                {slugError && (
                  <p className="text-xs text-destructive">{slugError}</p>
                )}
              </div>
            </CardContent>
          </Card>

          <Button
            className="w-full"
            size="lg"
            onClick={handleCreate}
            disabled={creating || !canSubmit}
          >
            {creating ? "Creating..." : "Create workspace"}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
