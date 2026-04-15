"use client";

import { useRef, useState } from "react";
import { useNavigation } from "../navigation";
import { useImmersiveMode } from "../platform";
import { toast } from "sonner";
import { ArrowLeft } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
} from "@multica/ui/components/ui/dialog";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { useCreateWorkspace } from "@multica/core/workspace/mutations";
import {
  WORKSPACE_SLUG_CONFLICT_ERROR,
  WORKSPACE_SLUG_FORMAT_ERROR,
  WORKSPACE_SLUG_REGEX,
  isWorkspaceSlugConflict,
  nameToWorkspaceSlug,
} from "../workspace/slug";

export function CreateWorkspaceModal({ onClose }: { onClose: () => void }) {
  // This modal is full-screen, so it covers the app titlebar. On macOS desktop
  // we hide the traffic lights for its lifetime so the Back button in the top-
  // left corner isn't stolen by the native controls' hit-test. No-op elsewhere.
  useImmersiveMode();

  const router = useNavigation();
  const createWorkspace = useCreateWorkspace();
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [slugServerError, setSlugServerError] = useState<string | null>(null);
  const slugTouched = useRef(false);

  const slugValidationError =
    slug.length > 0 && !WORKSPACE_SLUG_REGEX.test(slug)
      ? WORKSPACE_SLUG_FORMAT_ERROR
      : null;
  const slugError = slugValidationError ?? slugServerError;

  const canSubmit =
    name.trim().length > 0 && slug.trim().length > 0 && !slugError;

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
    createWorkspace.mutate(
      { name: name.trim(), slug: slug.trim() },
      {
        onSuccess: () => {
          onClose();
          router.push("/onboarding");
        },
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
    <Dialog
      open
      onOpenChange={(v) => {
        if (!v) onClose();
      }}
    >
      <DialogContent
        finalFocus={false}
        showCloseButton={false}
        className="inset-0 flex h-full w-full max-w-none sm:max-w-none translate-0 flex-col items-center justify-center rounded-none bg-background ring-0 shadow-none"
      >
        {/* Top drag region — restores window-drag ability that the full-screen
            modal would otherwise swallow. Transparent; web browsers ignore the
            -webkit-app-region property, so this is safe cross-platform. */}
        <div
          aria-hidden
          className="absolute inset-x-0 top-0 h-10"
          style={{ WebkitAppRegion: "drag" } as React.CSSProperties}
        />

        <Button
          variant="ghost"
          size="sm"
          className="absolute top-12 left-12 text-muted-foreground"
          style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
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
                    multica.ai/
                  </span>
                  <Input
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
            </CardContent>
          </Card>

          <Button
            className="w-full"
            size="lg"
            onClick={handleCreate}
            disabled={createWorkspace.isPending || !canSubmit}
          >
            {createWorkspace.isPending ? "Creating..." : "Create workspace"}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
