"use client";

import { useRef, useState } from "react";
import { toast } from "sonner";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { useCreateWorkspace } from "@multica/core/workspace/mutations";
import type { Workspace } from "@multica/core/types";
import {
  WORKSPACE_SLUG_CONFLICT_ERROR,
  WORKSPACE_SLUG_FORMAT_ERROR,
  WORKSPACE_SLUG_REGEX,
  isWorkspaceSlugConflict,
  nameToWorkspaceSlug,
} from "./slug";

export interface CreateWorkspaceFormProps {
  onSuccess: (workspace: Workspace) => void | Promise<void>;
}

export function CreateWorkspaceForm({ onSuccess }: CreateWorkspaceFormProps) {
  const createWorkspace = useCreateWorkspace();
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [useExistingFolder, setUseExistingFolder] = useState(false);
  const [localPath, setLocalPath] = useState("");
  const [slugServerError, setSlugServerError] = useState<string | null>(null);
  const slugTouched = useRef(false);

  const slugValidationError =
    slug.length > 0 && !WORKSPACE_SLUG_REGEX.test(slug)
      ? WORKSPACE_SLUG_FORMAT_ERROR
      : null;
  const slugError = slugValidationError ?? slugServerError;
  const canSubmit =
    name.trim().length > 0 &&
    slug.trim().length > 0 &&
    !slugError &&
    (!useExistingFolder || localPath.trim().length > 0);

  const handleNameChange = (value: string) => {
    setName(value);
    if (!slugTouched.current) {
      setSlug(nameToWorkspaceSlug(value));
      setSlugServerError(null);
    }
  };

  const handleSlugChange = (value: string) => {
    slugTouched.current = true;
    setSlug(value);
    setSlugServerError(null);
  };

  const handleCreate = () => {
    if (!canSubmit) return;
    const normalizedPath = localPath.trim();
    const localPathPayload = useExistingFolder && normalizedPath ? normalizedPath : undefined;
    createWorkspace.mutate(
      {
        name: name.trim(),
        slug: slug.trim(),
        local_path: localPathPayload,
      },
      {
        onSuccess,
        onError: (error) => {
          if (isWorkspaceSlugConflict(error)) {
            setSlugServerError(WORKSPACE_SLUG_CONFLICT_ERROR);
            toast.error("Choose a different workspace URL");
            return;
          }
          toast.error("Failed to create workspace");
        },
      },
    );
  };

  return (
    <Card className="w-full">
      <CardContent className="space-y-4 pt-6">
        <div className="space-y-1.5">
          <Label htmlFor="ws-name">Workspace Name</Label>
          <Input
            id="ws-name"
            autoFocus
            type="text"
            value={name}
            onChange={(e) => handleNameChange(e.target.value)}
            placeholder="My Workspace"
            onKeyDown={(e) => e.key === "Enter" && handleCreate()}
          />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="ws-slug">Workspace URL</Label>
          <div className="flex items-center gap-0 rounded-md border bg-background focus-within:ring-2 focus-within:ring-ring">
            <span className="pl-3 text-sm text-muted-foreground select-none">
              multica.ai/
            </span>
            <Input
              id="ws-slug"
              type="text"
              value={slug}
              onChange={(e) => handleSlugChange(e.target.value)}
              placeholder="my-workspace"
              className="border-0 shadow-none focus-visible:ring-0"
              onKeyDown={(e) => e.key === "Enter" && handleCreate()}
            />
          </div>
          {slugError && (
            <p className="text-xs text-destructive">{slugError}</p>
          )}
        </div>
        <div className="space-y-2">
          <label htmlFor="ws-existing-folder" className="flex items-center gap-2 text-sm">
            <Checkbox
              id="ws-existing-folder"
              checked={useExistingFolder}
              onCheckedChange={(checked) => setUseExistingFolder(Boolean(checked))}
            />
            Create from existing folder
          </label>
          {useExistingFolder && (
            <div className="space-y-1.5">
              <Label htmlFor="ws-local-path">Folder path</Label>
              <Input
                id="ws-local-path"
                type="text"
                value={localPath}
                onChange={(e) => setLocalPath(e.target.value)}
                placeholder="/home/user/projects/my-workspace"
                aria-describedby="ws-local-path-help"
                onKeyDown={(e) => e.key === "Enter" && handleCreate()}
              />
              <p id="ws-local-path-help" className="text-xs text-muted-foreground">
                Absolute path on the machine where daemon runs.
              </p>
            </div>
          )}
        </div>
        <Button
          className="w-full"
          size="lg"
          onClick={handleCreate}
          disabled={createWorkspace.isPending || !canSubmit}
        >
          {createWorkspace.isPending ? "Creating..." : "Create workspace"}
        </Button>
      </CardContent>
    </Card>
  );
}
