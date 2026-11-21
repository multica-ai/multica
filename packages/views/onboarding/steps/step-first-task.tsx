"use client";

import { useState } from "react";
import { Folder, Loader2, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { isImeComposing } from "@multica/core/utils";
import type { AgentRuntime } from "@multica/core/types";
import { useT } from "../../i18n";
import { pickDirectory, validateLocalDirectory } from "../../platform";
import { StepFooter, StepHeading } from "../components/step-shell";
import { ToolTile } from "../components/install-tool-dialog";
import type { ToolSelection } from "../components/tool-picker";
import { toolDisplayName, type FirstTaskFolder } from "../provisioning";

/**
 * The first task, written as one sentence. Submitting creates the agent and
 * the task together and opens it, so the first thing the member sees in the
 * product is their agent working.
 */
export function StepFirstTask({
  runtime,
  selection,
  canPickFolder,
  onSubmit,
  onBrowse,
}: {
  runtime: AgentRuntime;
  selection: ToolSelection;
  /** The tool runs on this computer, so a folder here is one it can open. */
  canPickFolder: boolean;
  onSubmit: (text: string, folder: FirstTaskFolder | null) => Promise<void>;
  /** Create the agent but no task, and land in the workspace. */
  onBrowse: () => Promise<void>;
}) {
  const { t } = useT("onboarding");
  const name = toolDisplayName(runtime.provider);
  const [text, setText] = useState("");
  const [folder, setFolder] = useState<FirstTaskFolder | null>(null);
  const [folderError, setFolderError] = useState<string | null>(null);
  const [picking, setPicking] = useState(false);
  const [pending, setPending] = useState<"submit" | "browse" | null>(null);

  const canSubmit = text.trim().length > 0 && pending === null;

  const run = async (which: "submit" | "browse", action: () => Promise<void>) => {
    if (pending) return;
    setPending(which);
    try {
      await action();
    } finally {
      setPending(null);
    }
  };
  const submit = () => {
    if (canSubmit) void run("submit", () => onSubmit(text.trim(), folder));
  };

  const chooseFolder = async () => {
    setFolderError(null);
    setPicking(true);
    try {
      const picked = await pickDirectory(folder?.path);
      if (!picked.ok || !picked.path) {
        if (picked.reason && picked.reason !== "cancelled") {
          setFolderError(picked.error ?? t(($) => $.step_first_task.folder_failed));
        }
        return;
      }
      const validation = await validateLocalDirectory(picked.path);
      if (!validation.ok) {
        setFolderError(validation.error ?? t(($) => $.step_first_task.folder_failed));
        return;
      }
      setFolder({ path: picked.path, label: picked.basename ?? null });
    } finally {
      setPicking(false);
    }
  };

  const suggestions = folder
    ? [
        t(($) => $.step_first_task.suggestion_folder_1),
        t(($) => $.step_first_task.suggestion_folder_2),
        t(($) => $.step_first_task.suggestion_folder_3),
      ]
    : [
        t(($) => $.step_first_task.suggestion_1),
        t(($) => $.step_first_task.suggestion_2),
        t(($) => $.step_first_task.suggestion_3),
      ];

  const meta = [
    selection.model || t(($) => $.step_first_task.default_model),
    runtime.device_info,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <>
      <div className="flex flex-col gap-6 pt-6">
        <StepHeading
          title={t(($) => $.step_first_task.title, { name })}
          description={t(($) => $.step_first_task.description, { name })}
        />

        <div className="flex flex-col gap-3">
          <div className="flex items-center gap-3 rounded-lg border bg-card px-4 py-3">
            <ToolTile provider={runtime.provider} />
            <div className="min-w-0 flex-1">
              <div className="truncate text-body font-medium text-foreground">{name}</div>
              <div className="truncate text-caption text-muted-foreground">{meta}</div>
            </div>
          </div>

          <div className="rounded-lg border bg-card focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50">
            <Textarea
              autoFocus
              value={text}
              onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && !isImeComposing(e)) {
                  e.preventDefault();
                  submit();
                }
              }}
              placeholder={t(($) => $.step_first_task.placeholder, { name })}
              aria-label={t(($) => $.step_first_task.placeholder, { name })}
              disabled={pending !== null}
              className="min-h-28 resize-none border-0 bg-transparent px-4 py-3 shadow-none focus-visible:ring-0 dark:bg-transparent"
            />
            {canPickFolder ? (
              <div className="flex items-center gap-1 border-t px-2 py-1.5">
                <Button
                  variant="ghost"
                  size="sm"
                  className="min-w-0 text-muted-foreground"
                  onClick={chooseFolder}
                  disabled={picking || pending !== null}
                  aria-busy={picking}
                >
                  {picking ? <Loader2 className="animate-spin" /> : <Folder />}
                  <span className="truncate">
                    {folder ? folder.path : t(($) => $.step_first_task.choose_folder)}
                  </span>
                </Button>
                {folder ? (
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="text-muted-foreground"
                    onClick={() => setFolder(null)}
                    disabled={pending !== null}
                    aria-label={t(($) => $.step_first_task.clear_folder)}
                  >
                    <X />
                  </Button>
                ) : null}
              </div>
            ) : null}
          </div>
          {folderError ? (
            <p role="alert" className="text-caption text-destructive">
              {folderError}
            </p>
          ) : null}

          <div className="flex flex-wrap gap-2">
            {suggestions.map((s) => (
              <Button
                key={s}
                variant="outline"
                size="sm"
                className="h-auto whitespace-normal py-1 text-left"
                onClick={() => setText(s)}
                disabled={pending !== null}
              >
                {s}
              </Button>
            ))}
          </div>
        </div>
      </div>

      <StepFooter hint={t(($) => $.step_first_task.hint, { name })}>
        <Button
          className="w-full"
          onClick={submit}
          disabled={!canSubmit}
          aria-busy={pending === "submit"}
        >
          {pending === "submit" ? <Loader2 className="animate-spin" /> : null}
          {t(($) => $.step_first_task.submit, { name })}
        </Button>
        <Button
          variant="ghost"
          className="w-full text-muted-foreground"
          onClick={() => run("browse", onBrowse)}
          disabled={pending !== null}
          aria-busy={pending === "browse"}
        >
          {pending === "browse" ? <Loader2 className="animate-spin" /> : null}
          {t(($) => $.step_first_task.browse)}
        </Button>
      </StepFooter>
    </>
  );
}
