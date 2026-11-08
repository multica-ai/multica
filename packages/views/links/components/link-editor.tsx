"use client";

import { useState } from "react";
import { useCurrentWorkspace } from "@multica/core/paths";
import type { WorkspaceLink } from "@multica/core/types";
import { LINK_ICON_KEYS, expandLinkUrl, validateLinkUrl, useCreateLink, useUpdateLink } from "@multica/core/links";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Dialog,
  DialogTrigger,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogClose,
} from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";
import { LinkIcon } from "./link-icon";

export function LinkEditor({
  link,
  projectId,
  compact = false,
}: {
  link?: WorkspaceLink;
  projectId?: string;
  compact?: boolean;
}) {
  const { t } = useT("settings");
  const ws = useCurrentWorkspace();
  const create = useCreateLink();
  const update = useUpdateLink();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState(link?.name ?? "");
  const [icon, setIcon] = useState(link?.icon ?? "");
  const [url, setUrl] = useState(link?.urlTemplate ?? "");
  const [mode, setMode] = useState<"embed" | "new_tab">(link?.openMode ?? "embed");
  const [session, setSession] = useState(link?.allowSameOrigin ?? false);
  const [failed, setFailed] = useState(false);
  const invalid =
    validateLinkUrl(url, !!projectId) !== null ||
    [...name.trim()].length < 1 ||
    [...name.trim()].length > 40;
  const pending = create.isPending || update.isPending;
  let preview = "";
  if (ws && validateLinkUrl(url, !!projectId) === null) {
    preview = expandLinkUrl(url, {
      workspaceSlug: ws.slug,
      workspaceId: ws.id,
      projectId,
    });
  }

  const save = async () => {
    if (invalid) return;
    setFailed(false);
    const data = { name, icon, urlTemplate: url, openMode: mode, allowSameOrigin: session };
    try {
      if (link) {
        await update.mutateAsync({ id: link.id, ...data });
      } else {
        await create.mutateAsync({ ...data, projectId });
      }
      setOpen(false);
    } catch {
      setFailed(true);
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (pending) return;
        if (next) {
          setName(link?.name ?? "");
          setIcon(link?.icon ?? "");
          setUrl(link?.urlTemplate ?? "");
          setMode(link?.openMode ?? "embed");
          setSession(link?.allowSameOrigin ?? false);
          setFailed(false);
        }
        setOpen(next);
      }}
    >
      <DialogTrigger
        render={
          <Button
            variant="ghost"
            size="sm"
            aria-label={link ? t(($) => $.links.edit) : t(($) => $.links.add)}
          />
        }
      >
        {compact ? "+" : link ? t(($) => $.links.edit) : t(($) => $.links.add)}
      </DialogTrigger>
      <DialogContent className="max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{link ? t(($) => $.links.edit) : t(($) => $.links.add)}</DialogTitle>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          <label className="grid gap-1 text-caption">
            {t(($) => $.links.name)}
            <Input value={name} onChange={(e) => setName(e.target.value)} />
          </label>
          <div className="grid gap-2">
            <span className="text-caption">{t(($) => $.links.icon)}</span>
            <div className="flex flex-wrap gap-1">
              <Button
                type="button"
                variant={icon === "" ? "secondary" : "ghost"}
                size="icon-sm"
                aria-label={t(($) => $.links.default_icon)}
                onClick={() => setIcon("")}
              >
                <LinkIcon icon="" />
              </Button>
              {LINK_ICON_KEYS.map((key) => (
                <Button
                  key={key}
                  type="button"
                  variant={icon === key ? "secondary" : "ghost"}
                  size="icon-sm"
                  aria-label={key}
                  onClick={() => setIcon(key)}
                >
                  <LinkIcon icon={key} />
                </Button>
              ))}
            </div>
            <Input aria-label={t(($) => $.links.icon)} value={icon} onChange={(e) => setIcon(e.target.value)} />
          </div>
          <label className="grid gap-1 text-caption">
            {t(($) => $.links.url)}
            <Input value={url} onChange={(e) => setUrl(e.target.value)} aria-describedby="link-variables" />
          </label>
          <p id="link-variables" className="text-caption text-muted-foreground">
            {t(($) => $.links.variables)}
          </p>
          <label className="grid gap-1 text-caption">
            {t(($) => $.links.mode)}
            <select
              className="rounded-md border bg-background p-2"
              value={mode}
              onChange={(e) => setMode(e.target.value === "embed" ? "embed" : "new_tab")}
            >
              <option value="embed">{t(($) => $.links.embed)}</option>
              <option value="new_tab">{t(($) => $.links.new_tab)}</option>
            </select>
          </label>
          {mode === "embed" && (
            <div className="grid gap-1">
              <label className="flex gap-2 text-caption">
                <input type="checkbox" checked={session} onChange={(e) => setSession(e.target.checked)} />
                {t(($) => $.links.session)}
              </label>
              <p className="text-caption text-muted-foreground">{t(($) => $.links.warning)}</p>
            </div>
          )}
          {preview && (
            <div className="min-w-0 text-caption">
              <span>{t(($) => $.links.preview)}</span>
              <p className="break-all text-muted-foreground">{preview}</p>
            </div>
          )}
          {invalid && (url || name) && (
            <p className="text-caption text-destructive">{t(($) => $.links.invalid)}</p>
          )}
          {failed && (
            <p role="alert" className="text-caption text-destructive">{t(($) => $.links.failed)}</p>
          )}
          <DialogFooter>
            <DialogClose render={<Button variant="outline" disabled={pending} />}>
              {t(($) => $.links.cancel)}
            </DialogClose>
            <Button type="submit" disabled={invalid || pending} aria-busy={pending}>
              {t(($) => $.links.save)}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
