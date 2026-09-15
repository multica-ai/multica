"use client";

import { useState } from "react";
import { Cloud, Loader2 } from "lucide-react";
import { EMPTY_AGENT_DRAFT } from "@multica/core/agents";
import { runtimeDisplayLabel } from "@multica/core/runtimes";
import type { RuntimeDevice } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";
import { QoderAgentSelect } from "./qoder-agent-select";
import { useCreateAgentSubmit } from "../create/use-create-agent-submit";

export function AddQoderAgentDialog({
  runtime,
  onClose,
}: {
  runtime: RuntimeDevice;
  onClose: () => void;
}) {
  const { t } = useT("runtimes");
  const [draft, setDraft] = useState({
    ...EMPTY_AGENT_DRAFT,
    runtimeId: runtime.id,
  });
  const submit = useCreateAgentSubmit({
    draft,
    runtimeId: runtime.id,
    squadId: null,
    onCreated: onClose,
  });
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !submit.creating) onClose();
      }}
    >
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t(($) => $.qoder.add_agent)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.qoder.add_agent_description)}
          </DialogDescription>
        </DialogHeader>
        <form
          className="space-y-5"
          onSubmit={(event) => {
            event.preventDefault();
            if (draft.qoderAgentId && draft.name.trim()) void submit.create();
          }}
        >
          <div className="flex items-center gap-2 rounded-lg bg-muted/50 p-3 text-caption">
            <Cloud aria-hidden="true" className="size-4 text-info" />
            {runtimeDisplayLabel(runtime)}
          </div>
          <QoderAgentSelect
            value={draft.qoderAgentId}
            onChange={(qoderAgentId) =>
              setDraft((old) => ({ ...old, qoderAgentId }))
            }
            disabled={submit.creating}
          />
          <label className="block space-y-2 text-caption">
            {t(($) => $.qoder.agent_name)}
            <Input
              required
              value={draft.name}
              disabled={submit.creating}
              autoComplete="off"
              onChange={(event) => {
                setDraft((old) => ({ ...old, name: event.target.value }));
                submit.clearNameError();
              }}
            />
          </label>
          {(submit.nameError || submit.formError) && (
            <p role="alert" className="text-caption text-destructive">
              {submit.nameError || submit.formError}
            </p>
          )}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={submit.creating}
              onClick={onClose}
            >
              {t(($) => $.qoder.cancel)}
            </Button>
            <Button
              type="submit"
              disabled={
                !draft.qoderAgentId || !draft.name.trim() || submit.creating
              }
            >
              {submit.creating && (
                <Loader2 aria-hidden="true" className="size-4 animate-spin" />
              )}
              {t(($) => $.qoder.add_agent)}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
