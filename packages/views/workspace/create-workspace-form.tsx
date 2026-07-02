"use client";

import { useRef, useState } from "react";
import { toast } from "sonner";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { useCreateWorkspace } from "@multica/core/workspace/mutations";
import type { Workspace } from "@multica/core/types";
import { isImeComposing } from "@multica/core/utils";
import {
  WORKSPACE_SLUG_REGEX,
  isWorkspaceSlugConflict,
  nameToWorkspaceSlug,
} from "./slug";
import {
  TEAM_KEY_REGEX,
  defaultTeamKeyFromSlug,
  normalizeTeamKey,
} from "@multica/core/workspace";
import { useT } from "../i18n";
import { isReservedSlug } from "@multica/core/paths";
import { useConfigStore } from "@multica/core/config";
import { workspaceUrlHost } from "@multica/core/workspace/workspace-url";

export interface CreateWorkspaceFormProps {
  onSuccess: (workspace: Workspace) => void | Promise<void>;
}

export function CreateWorkspaceForm({ onSuccess }: CreateWorkspaceFormProps) {
  const { t } = useT("workspace");
  const createWorkspace = useCreateWorkspace();
  const urlHost = workspaceUrlHost(useConfigStore((s) => s.daemonAppUrl));
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [teamKey, setTeamKey] = useState("");
  const [slugServerError, setSlugServerError] = useState<string | null>(null);
  const slugTouched = useRef(false);
  const teamKeyTouched = useRef(false);

  const slugValidationError =
    slug.length > 0 && !WORKSPACE_SLUG_REGEX.test(slug)
      ? t(($) => $.create_form.errors.slug_format)
      : null;
  const slugReservedError =
    slug.length > 0 && isReservedSlug(slug)
      ? t(($) => $.create_form.errors.slug_reserved)
      : null;
  const slugError = slugValidationError ?? slugReservedError ?? slugServerError;
  const normalizedTeamKey = normalizeTeamKey(teamKey);
  const teamKeyError =
    normalizedTeamKey.length > 0 && !TEAM_KEY_REGEX.test(normalizedTeamKey)
      ? t(($) => $.create_form.errors.team_key_format)
      : null;
  const canSubmit =
    name.trim().length > 0 &&
    slug.trim().length > 0 &&
    normalizedTeamKey.length > 0 &&
    !slugError &&
    !teamKeyError;

  const handleNameChange = (value: string) => {
    setName(value);
    if (!slugTouched.current) {
      const nextSlug = nameToWorkspaceSlug(value);
      setSlug(nextSlug);
      setSlugServerError(null);
      if (!teamKeyTouched.current) {
        setTeamKey(defaultTeamKeyFromSlug(nextSlug));
      }
    }
  };

  const handleSlugChange = (value: string) => {
    slugTouched.current = true;
    setSlug(value);
    setSlugServerError(null);
    if (!teamKeyTouched.current) {
      setTeamKey(defaultTeamKeyFromSlug(value));
    }
  };

  const handleTeamKeyChange = (value: string) => {
    teamKeyTouched.current = true;
    setTeamKey(normalizeTeamKey(value));
  };

  const handleCreate = () => {
    if (!canSubmit) return;
    createWorkspace.mutate(
      {
        name: name.trim(),
        slug: slug.trim(),
        default_team_key: normalizedTeamKey,
      },
      {
        onSuccess,
        onError: (error) => {
          if (isWorkspaceSlugConflict(error)) {
            setSlugServerError(t(($) => $.create_form.errors.slug_taken));
            toast.error(t(($) => $.create_form.errors.slug_conflict_toast));
            return;
          }
          toast.error(
            error instanceof Error && error.message
              ? error.message
              : t(($) => $.create_form.errors.create_failed),
          );
        },
      },
    );
  };

  return (
    <Card className="w-full">
      <CardContent className="space-y-4 pt-6">
        <div className="space-y-1.5">
          <Label htmlFor="ws-name">{t(($) => $.create_form.name_label)}</Label>
          <Input
            id="ws-name"
            autoFocus
            type="text"
            value={name}
            onChange={(e) => handleNameChange(e.target.value)}
            placeholder={t(($) => $.create_form.name_placeholder)}
            onKeyDown={(e) => {
              if (isImeComposing(e)) return;
              if (e.key === "Enter") handleCreate();
            }}
          />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="ws-slug">{t(($) => $.create_form.url_label)}</Label>
          <div className="flex items-center gap-0 rounded-md border bg-background focus-within:ring-2 focus-within:ring-ring">
            <span className="pl-3 text-sm text-muted-foreground select-none">
              {`${urlHost}/`}
            </span>
            <Input
              id="ws-slug"
              type="text"
              value={slug}
              onChange={(e) => handleSlugChange(e.target.value)}
              placeholder={t(($) => $.create_form.url_placeholder)}
              className="border-0 shadow-none focus-visible:ring-0"
              onKeyDown={(e) => {
                if (isImeComposing(e)) return;
                if (e.key === "Enter") handleCreate();
              }}
            />
          </div>
          {slugError && (
            <p className="text-xs text-destructive">{slugError}</p>
          )}
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="ws-team-key">
            {t(($) => $.create_form.team_key_label)}
          </Label>
          <div className="flex items-center gap-0 rounded-md border bg-background focus-within:ring-2 focus-within:ring-ring">
            <Input
              id="ws-team-key"
              type="text"
              value={teamKey}
              onChange={(e) => handleTeamKeyChange(e.target.value)}
              placeholder="MUL"
              maxLength={7}
              className="border-0 font-mono shadow-none focus-visible:ring-0"
              onKeyDown={(e) => {
                if (isImeComposing(e)) return;
                if (e.key === "Enter") handleCreate();
              }}
            />
            <span className="pr-3 text-sm font-mono text-muted-foreground select-none">
              -123
            </span>
          </div>
          <p className="text-xs text-muted-foreground">
            {t(($) => $.create_form.team_key_hint)}
          </p>
          {teamKeyError && (
            <p className="text-xs text-destructive">{teamKeyError}</p>
          )}
        </div>
        <Button
          className="w-full"
          size="lg"
          onClick={handleCreate}
          disabled={createWorkspace.isPending || !canSubmit}
        >
          {createWorkspace.isPending
            ? t(($) => $.create_form.submitting)
            : t(($) => $.create_form.submit)}
        </Button>
      </CardContent>
    </Card>
  );
}
