"use client";

import { useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { useCreateWorkspace } from "@multica/core/workspace/mutations";
import { knownUserListOptions } from "@multica/core/workspace/queries";
import type { Workspace } from "@multica/core/types";
import { isImeComposing } from "@multica/core/utils";
import {
  WORKSPACE_SLUG_REGEX,
  isWorkspaceSlugConflict,
  nameToWorkspaceSlug,
} from "./slug";
import { useT } from "../i18n";
import { isReservedSlug } from "@multica/core/paths";
import { useConfigStore } from "@multica/core/config";
import { workspaceUrlHost } from "@multica/core/workspace/workspace-url";
import { User } from "lucide-react";
import { matchesPinyin } from "../editor/extensions/pinyin-match";

export interface CreateWorkspaceFormProps {
  onSuccess: (workspace: Workspace) => void | Promise<void>;
}

export function CreateWorkspaceForm({ onSuccess }: CreateWorkspaceFormProps) {
  const { t } = useT("workspace");
  const createWorkspace = useCreateWorkspace();
  const urlHost = workspaceUrlHost(useConfigStore((s) => s.daemonAppUrl));
  const { data: knownUsers = [] } = useQuery(knownUserListOptions());
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [slugServerError, setSlugServerError] = useState<string | null>(null);
  const [memberFilter, setMemberFilter] = useState("");
  const [selectedMemberIds, setSelectedMemberIds] = useState<Set<string>>(
    () => new Set(),
  );
  const slugTouched = useRef(false);

  const slugValidationError =
    slug.length > 0 && !WORKSPACE_SLUG_REGEX.test(slug)
      ? t(($) => $.create_form.errors.slug_format)
      : null;
  const slugReservedError =
    slug.length > 0 && isReservedSlug(slug)
      ? t(($) => $.create_form.errors.slug_reserved)
      : null;
  const slugError = slugValidationError ?? slugReservedError ?? slugServerError;
  const canSubmit =
    name.trim().length > 0 && slug.trim().length > 0 && !slugError;

  const filteredKnownUsers = useMemo(() => {
    const q = memberFilter.trim().toLowerCase();
    if (!q) return knownUsers;
    return knownUsers.filter(
      (u) =>
        u.name.toLowerCase().includes(q) ||
        u.email.toLowerCase().includes(q) ||
        matchesPinyin(u.name, q),
    );
  }, [knownUsers, memberFilter]);

  const toggleMember = (userId: string, checked: boolean) => {
    setSelectedMemberIds((prev) => {
      const next = new Set(prev);
      if (checked) next.add(userId);
      else next.delete(userId);
      return next;
    });
  };

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
      {
        name: name.trim(),
        slug: slug.trim(),
        member_user_ids:
          selectedMemberIds.size > 0
            ? [...selectedMemberIds]
            : undefined,
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

        {knownUsers.length > 0 && (
          <div className="space-y-2">
            <div>
              <Label htmlFor="ws-members">
                {t(($) => $.create_form.members_label)}
              </Label>
              <p className="text-xs text-muted-foreground">
                {t(($) => $.create_form.members_hint)}
              </p>
            </div>
            <Input
              id="ws-members"
              type="search"
              value={memberFilter}
              onChange={(e) => setMemberFilter(e.target.value)}
              placeholder={t(($) => $.create_form.members_search_placeholder)}
            />
            <div className="max-h-48 space-y-1 overflow-y-auto rounded-md border p-2">
              {filteredKnownUsers.length === 0 ? (
                <p className="px-2 py-3 text-sm text-muted-foreground">
                  {t(($) => $.create_form.members_empty)}
                </p>
              ) : (
                filteredKnownUsers.map((user) => {
                  const checked = selectedMemberIds.has(user.id);
                  return (
                    <label
                      key={user.id}
                      className="flex cursor-pointer items-center gap-3 rounded-md px-2 py-2 hover:bg-muted/60"
                    >
                      <Checkbox
                        checked={checked}
                        onCheckedChange={(v) =>
                          toggleMember(user.id, v === true)
                        }
                      />
                      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-muted">
                        <User className="h-4 w-4 text-muted-foreground" />
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-sm font-medium">
                          {user.name}
                        </div>
                        <div className="truncate text-xs text-muted-foreground">
                          {user.email}
                        </div>
                      </div>
                    </label>
                  );
                })
              )}
            </div>
          </div>
        )}

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