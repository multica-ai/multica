"use client";

import { useState, useRef } from "react";
import { toast } from "sonner";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { useCreateWorkspace } from "@multica/core/workspace/mutations";

const SLUG_REGEX = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

function nameToSlug(name: string): string {
  return (
    name
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-|-$/g, "") || "workspace"
  );
}

export function StepWorkspace({ onNext }: { onNext: () => void }) {
  const createWorkspace = useCreateWorkspace();
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  // Track whether the user has manually edited the slug field.
  const slugTouched = useRef(false);

  const slugError =
    slug.length > 0 && !SLUG_REGEX.test(slug)
      ? "Only lowercase letters, numbers, and hyphens"
      : null;

  const canSubmit =
    name.trim().length > 0 && slug.trim().length > 0 && !slugError;

  const handleNameChange = (value: string) => {
    setName(value);
    if (!slugTouched.current) {
      setSlug(nameToSlug(value));
    }
  };

  const handleSlugChange = (value: string) => {
    slugTouched.current = true;
    setSlug(value);
  };

  const handleCreate = () => {
    if (!canSubmit) return;
    createWorkspace.mutate(
      { name: name.trim(), slug: slug.trim() },
      {
        onSuccess: () => onNext(),
        onError: () => toast.error("Failed to create workspace"),
      },
    );
  };

  return (
    <div className="flex w-full max-w-md flex-col items-center gap-8">
      <div className="text-center">
        <h1 className="text-3xl font-semibold tracking-tight">
          Welcome to Multica
        </h1>
        <p className="mt-2 text-muted-foreground">
          Create your workspace to start building with AI agents.
        </p>
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
              placeholder="My Team"
              onKeyDown={(e) => e.key === "Enter" && handleCreate()}
            />
          </div>
          <div className="space-y-1.5">
            <Label>Workspace URL</Label>
            <div className="flex items-center gap-0 rounded-md border bg-background focus-within:ring-2 focus-within:ring-ring">
              <span className="pl-3 text-sm text-muted-foreground select-none">
                multica.ai/
              </span>
              <Input
                type="text"
                value={slug}
                onChange={(e) => handleSlugChange(e.target.value)}
                placeholder="my-team"
                className="border-0 shadow-none focus-visible:ring-0"
                onKeyDown={(e) => e.key === "Enter" && handleCreate()}
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
        disabled={createWorkspace.isPending || !canSubmit}
      >
        {createWorkspace.isPending ? "Creating..." : "Create Workspace"}
      </Button>
    </div>
  );
}
