"use client";

import { useRef, useState } from "react";
import { toast } from "sonner";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { useCreateWorkspace } from "@multica/core/workspace/mutations";
import type { Workspace } from "@multica/core/types";
import { useI18n } from "../i18n";
import {
  WORKSPACE_SLUG_REGEX,
  isWorkspaceSlugConflict,
  nameToWorkspaceSlug,
} from "./slug";

export interface CreateWorkspaceFormProps {
  onSuccess: (workspace: Workspace) => void | Promise<void>;
}

export function CreateWorkspaceForm({ onSuccess }: CreateWorkspaceFormProps) {
  const { t } = useI18n();
  const createWorkspace = useCreateWorkspace();
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [slugServerError, setSlugServerError] = useState<string | null>(null);
  const slugTouched = useRef(false);

  const slugValidationError =
    slug.length > 0 && !WORKSPACE_SLUG_REGEX.test(slug)
      ? t("workspace.form.slugFormatError")
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
        onSuccess,
        onError: (error) => {
          if (isWorkspaceSlugConflict(error)) {
            setSlugServerError(t("workspace.form.slugConflictError"));
            toast.error(t("workspace.form.slugConflictToast"));
            return;
          }
          toast.error(t("workspace.form.createFailed"));
        },
      },
    );
  };

  return (
    <Card className="w-full">
      <CardContent className="space-y-4 pt-6">
        <div className="space-y-1.5">
          <Label htmlFor="ws-name">{t("workspace.form.nameLabel")}</Label>
          <Input
            id="ws-name"
            autoFocus
            type="text"
            value={name}
            onChange={(e) => handleNameChange(e.target.value)}
            placeholder={t("workspace.form.namePlaceholder")}
            onKeyDown={(e) => e.key === "Enter" && handleCreate()}
          />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="ws-slug">{t("workspace.form.slugLabel")}</Label>
          <div className="flex items-center gap-0 rounded-md border bg-background focus-within:ring-2 focus-within:ring-ring">
            <span className="pl-3 text-sm text-muted-foreground select-none">
              multica.ai/
            </span>
            <Input
              id="ws-slug"
              type="text"
              value={slug}
              onChange={(e) => handleSlugChange(e.target.value)}
              placeholder={t("workspace.form.slugPlaceholder")}
              className="border-0 shadow-none focus-visible:ring-0"
              onKeyDown={(e) => e.key === "Enter" && handleCreate()}
            />
          </div>
          {slugError && (
            <p className="text-xs text-destructive">{slugError}</p>
          )}
        </div>
        <Button
          className="w-full"
          size="lg"
          onClick={handleCreate}
          disabled={createWorkspace.isPending || !canSubmit}
        >
          {createWorkspace.isPending ? t("workspace.form.creating") : t("workspace.form.create")}
        </Button>
      </CardContent>
    </Card>
  );
}
