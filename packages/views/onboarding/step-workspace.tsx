"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { useCreateWorkspace } from "@multica/core/workspace/mutations";

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

  const canSubmit = name.trim().length > 0;

  const handleCreate = () => {
    if (!canSubmit) return;
    createWorkspace.mutate(
      { name: name.trim(), slug: nameToSlug(name.trim()) },
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
              onChange={(e) => setName(e.target.value)}
              placeholder="My Team"
              onKeyDown={(e) => e.key === "Enter" && handleCreate()}
            />
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
