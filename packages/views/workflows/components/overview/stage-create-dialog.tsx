"use client";

import { useState } from "react";
import { useCreateStage } from "@multica/core/workflows/queries";
import { useT } from "../../../i18n";

interface StageCreateDialogProps {
  workflowId: string;
  wsId: string;
  onClose: () => void;
}

export function StageCreateDialog({
  workflowId,
  wsId,
  onClose,
}: StageCreateDialogProps) {
  const { t } = useT("workflows");
  const createStage = useCreateStage(wsId, workflowId);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!name.trim()) return;
    try {
      await createStage.mutateAsync({
        name: name.trim(),
        description: description.trim() || undefined,
      });
      onClose();
    } catch {
      // Error handled by mutation state
    }
  }

  return (
    <div
      className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center"
      onClick={onClose}
      data-testid="stage-dialog-overlay"
    >
      <div
        className="bg-background rounded-lg shadow-xl w-full max-w-md mx-4 p-6"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="text-lg font-semibold">
          {t(($) => $.overview.stage_dialog.create_title)}
        </h2>
        <form onSubmit={handleSubmit} className="mt-4 space-y-4">
          <div>
            <label className="block text-sm font-medium mb-1">
              {t(($) => $.overview.stage_dialog.name_label)}
            </label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t(($) => $.overview.stage_dialog.name_placeholder)}
              className="w-full px-3 py-2 border rounded-md text-sm bg-background"
              autoFocus
              data-testid="stage-name-input"
            />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1">
              {t(($) => $.overview.stage_dialog.description_label)}
            </label>
            <textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t(($) => $.overview.stage_dialog.description_placeholder)}
              className="w-full px-3 py-2 border rounded-md text-sm bg-background resize-none"
              rows={3}
            />
          </div>
          <div className="flex justify-end gap-3 pt-2">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 text-sm rounded-md border hover:bg-muted"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={!name.trim() || createStage.isPending}
              className="px-4 py-2 text-sm rounded-md bg-primary text-primary-foreground
                disabled:opacity-50 hover:opacity-90"
            >
              {createStage.isPending ? "Creating..." : "Create"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
