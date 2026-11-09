"use client";

import { useRef, useState } from "react";
import { toast } from "sonner";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { useUpdateProject } from "@multica/core/projects/mutations";
import { useT } from "../i18n";

/**
 * Rename a project from its menus.
 *
 * The title in the project page's properties panel is editable in place, but
 * nothing marks it as editable and the panel can be collapsed, so the project
 * page's name menu and the projects list row menu open this dialog instead. It
 * writes through the same `useUpdateProject` mutation as every other project
 * field edit, so the list, the detail page and the sidebar all reconcile from
 * one code path.
 */
export function RenameProjectModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const { t } = useT("modals");
  const projectId = typeof data?.projectId === "string" ? data.projectId : "";
  const seedTitle = typeof data?.title === "string" ? data.title : "";
  const [title, setTitle] = useState(seedTitle);
  const inputRef = useRef<HTMLInputElement>(null);
  const updateProject = useUpdateProject();
  const pending = updateProject.isPending;

  // Open with the current name selected so the first keystroke replaces it.
  // The popup's content mounts after this component does, so this runs when
  // the dialog moves focus in rather than from an effect here.
  const focusNameInput = () => {
    const input = inputRef.current;
    input?.select();
    return input;
  };

  const trimmed = title.trim();
  const unchanged = trimmed === seedTitle.trim();
  // Not gated on "empty" or "unchanged": handleSubmit explains the first and
  // closes on the second, so the button never becomes a dead control.
  const canSubmit = !!projectId && !pending;

  const handleSubmit = async () => {
    if (pending || !projectId) return;
    // The user cleared the field on purpose; say why nothing was saved rather
    // than closing as if it had been.
    if (!trimmed) {
      toast.error(t(($) => $.rename_project.title_required));
      return;
    }
    if (unchanged) {
      onClose();
      return;
    }
    try {
      await updateProject.mutateAsync({ id: projectId, title: trimmed });
      toast.success(t(($) => $.rename_project.toast_renamed));
      onClose();
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.rename_project.toast_failed),
      );
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(v) => {
        // The write is already in flight; closing now would drop its outcome.
        if (!v && !pending) onClose();
      }}
    >
      <DialogContent className="sm:max-w-md" initialFocus={focusNameInput}>
        <DialogHeader>
          <DialogTitle>{t(($) => $.rename_project.title)}</DialogTitle>
        </DialogHeader>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void handleSubmit();
          }}
        >
          <Input
            ref={inputRef}
            value={title}
            disabled={pending}
            placeholder={t(($) => $.rename_project.placeholder)}
            aria-label={t(($) => $.rename_project.title)}
            onChange={(event) => setTitle(event.target.value)}
          />
        </form>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={pending}
            onClick={onClose}
          >
            {t(($) => $.common.cancel)}
          </Button>
          <Button
            type="button"
            size="sm"
            disabled={!canSubmit}
            aria-busy={pending}
            onClick={() => void handleSubmit()}
          >
            {pending && <Spinner className="size-3.5" aria-hidden="true" />}
            {t(($) => $.rename_project.submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
