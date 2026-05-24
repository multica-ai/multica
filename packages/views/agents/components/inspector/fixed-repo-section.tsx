"use client";

import { useState } from "react";
import { Pencil, Plus, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useT } from "../../i18n";

interface FixedRepoSectionProps {
  enabled: boolean;
  paths: string[];
  vcsType: string;
  initScript: string;
  cleanupScript: string;
  canEdit: boolean;
  onUpdate: (data: Record<string, unknown>) => Promise<void>;
}

const VCS_OPTIONS = ["git", "p4", "svn", "none"] as const;

export function FixedRepoSection({
  enabled,
  paths,
  vcsType,
  initScript,
  cleanupScript,
  canEdit,
  onUpdate,
}: FixedRepoSectionProps) {
  const { t } = useT("agents");

  const handleToggle = (checked: boolean) => {
    void onUpdate({ fixed_repo_enabled: checked });
  };

  if (!canEdit) {
    return (
      <div className="border-b px-5 py-4">
        <div className="mb-1 -mx-2 px-2 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
          {t(($) => $.fixed_repo.section_label)}
        </div>
        <div className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5">
          <span className="text-sm text-muted-foreground">
            {enabled ? t(($) => $.fixed_repo.enabled) : t(($) => $.fixed_repo.disabled)}
          </span>
        </div>
      </div>
    );
  }

  return (
    <div className="border-b px-5 py-4">
      <div className="mb-1 -mx-2 px-2 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
        {t(($) => $.fixed_repo.section_label)}
      </div>
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Switch
            checked={enabled}
            onCheckedChange={handleToggle}
            aria-label={t(($) => $.fixed_repo.section_label)}
          />
          <span className="text-sm text-muted-foreground">
            {enabled ? t(($) => $.fixed_repo.enabled) : t(($) => $.fixed_repo.disabled)}
          </span>
        </div>
        {enabled && (
          <FixedRepoDialog
            paths={paths}
            vcsType={vcsType}
            initScript={initScript}
            cleanupScript={cleanupScript}
            onUpdate={onUpdate}
          />
        )}
      </div>
    </div>
  );
}

function FixedRepoDialog({
  paths,
  vcsType,
  initScript,
  cleanupScript,
  onUpdate,
}: Omit<FixedRepoSectionProps, "enabled" | "canEdit">) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);

  return (
    <>
      <Button
        variant="ghost"
        size="icon"
        className="h-6 w-6"
        onClick={() => setOpen(true)}
        aria-label={t(($) => $.fixed_repo.edit_aria)}
      >
        <Pencil className="h-3 w-3" />
      </Button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-lg">
          {open && (
            <FixedRepoDialogBody
              initialPaths={paths}
              initialVcsType={vcsType}
              initialInitScript={initScript}
              initialCleanupScript={cleanupScript}
              onUpdate={onUpdate}
              onClose={() => setOpen(false)}
            />
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}

function FixedRepoDialogBody({
  initialPaths,
  initialVcsType,
  initialInitScript,
  initialCleanupScript,
  onUpdate,
  onClose,
}: {
  initialPaths: string[];
  initialVcsType: string;
  initialInitScript: string;
  initialCleanupScript: string;
  onUpdate: (data: Record<string, unknown>) => Promise<void>;
  onClose: () => void;
}) {
  const { t } = useT("agents");
  const [vcsType, setVcsType] = useState(initialVcsType);
  const [paths, setPaths] = useState<string[]>(initialPaths);
  const [newPath, setNewPath] = useState("");
  const [initScript, setInitScript] = useState(initialInitScript);
  const [cleanupScript, setCleanupScript] = useState(initialCleanupScript);
  const [saving, setSaving] = useState(false);

  const addPath = () => {
    const trimmed = newPath.trim();
    if (trimmed && !paths.includes(trimmed)) {
      setPaths([...paths, trimmed]);
      setNewPath("");
    }
  };

  const removePath = (index: number) => {
    setPaths(paths.filter((_, i) => i !== index));
  };

  const dirty =
    vcsType !== initialVcsType ||
    JSON.stringify(paths) !== JSON.stringify(initialPaths) ||
    initScript !== initialInitScript ||
    cleanupScript !== initialCleanupScript;

  const save = async () => {
    if (!dirty) {
      onClose();
      return;
    }
    setSaving(true);
    try {
      await onUpdate({
        vcs_type: vcsType,
        fixed_repo_paths: paths,
        init_script: initScript,
        cleanup_script: cleanupScript,
      });
      onClose();
    } catch {
      // toast handled by parent's onUpdate
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle>{t(($) => $.fixed_repo.dialog_title)}</DialogTitle>
      </DialogHeader>

      <div className="flex flex-col gap-4">
        <div>
          <Label className="text-xs text-muted-foreground">{t(($) => $.fixed_repo.vcs_type_label)}</Label>
          <Select value={vcsType} onValueChange={setVcsType}>
            <SelectTrigger className="mt-1 h-8">
              <SelectValue>
                {() => vcsType === "none" ? t(($) => $.fixed_repo.vcs_none) : vcsType || t(($) => $.fixed_repo.vcs_type_placeholder)}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {VCS_OPTIONS.map((opt) => (
                <SelectItem key={opt} value={opt}>
                  {opt === "none" ? t(($) => $.fixed_repo.vcs_none) : opt}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div>
          <Label className="text-xs text-muted-foreground">{t(($) => $.fixed_repo.paths_label)}</Label>
          <div className="mt-1 flex flex-col gap-1.5">
            {paths.length === 0 && (
              <span className="text-xs italic text-muted-foreground/50">
                {t(($) => $.fixed_repo.paths_empty)}
              </span>
            )}
            {paths.map((p, i) => (
              <div key={i} className="flex items-center gap-1.5">
                <code className="flex-1 rounded border bg-muted/50 px-2 py-1 text-xs">
                  {p}
                </code>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-6 w-6 shrink-0"
                  onClick={() => removePath(i)}
                  aria-label={`Remove ${p}`}
                >
                  <X className="h-3 w-3" />
                </Button>
              </div>
            ))}
            <div className="flex items-center gap-1.5">
              <Input
                value={newPath}
                onChange={(e) => setNewPath(e.target.value)}
                placeholder={t(($) => $.fixed_repo.paths_placeholder)}
                className="h-8 text-xs"
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    e.preventDefault();
                    addPath();
                  }
                }}
              />
              <Button
                variant="ghost"
                size="icon"
                className="h-8 w-8 shrink-0"
                onClick={addPath}
                disabled={!newPath.trim()}
              >
                <Plus className="h-3.5 w-3.5" />
              </Button>
            </div>
          </div>
        </div>

        <div>
          <Label className="text-xs text-muted-foreground">{t(($) => $.fixed_repo.init_script_label)}</Label>
          <Input
            value={initScript}
            onChange={(e) => setInitScript(e.target.value)}
            placeholder={t(($) => $.fixed_repo.init_script_placeholder)}
            className="mt-1 h-8 text-xs"
          />
        </div>

        <div>
          <Label className="text-xs text-muted-foreground">{t(($) => $.fixed_repo.cleanup_script_label)}</Label>
          <Input
            value={cleanupScript}
            onChange={(e) => setCleanupScript(e.target.value)}
            placeholder={t(($) => $.fixed_repo.cleanup_script_placeholder)}
            className="mt-1 h-8 text-xs"
          />
        </div>
      </div>

      <DialogFooter>
        <Button variant="ghost" size="sm" onClick={onClose} disabled={saving}>
          {t(($) => $.fixed_repo.cancel)}
        </Button>
        <Button size="sm" onClick={() => void save()} disabled={saving || !dirty}>
          {t(($) => $.fixed_repo.save)}
        </Button>
      </DialogFooter>
    </>
  );
}
