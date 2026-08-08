"use client";

import { useState } from "react";
import { CheckCircle2, KeyRound, Settings2 } from "lucide-react";
import {
  isPiRuntimeModelConfigured,
  runtimeDefaultPiConfig,
  runtimeSupportsDefaultModelConnection,
} from "@multica/core/runtimes";
import type { AgentRuntime } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";
import { PiModelConnectionDialog } from "./pi-model-connection-dialog";

export function PiModelConnectionCard({
  runtime,
  wsId,
  canEdit,
}: {
  runtime: AgentRuntime;
  wsId: string;
  canEdit: boolean;
}) {
  const { t } = useT("runtimes");
  const [dialogOpen, setDialogOpen] = useState(false);

  if (
    runtime.provider !== "pi" ||
    !runtimeSupportsDefaultModelConnection(runtime)
  ) {
    return null;
  }

  const configured = isPiRuntimeModelConfigured(runtime);
  const config = runtimeDefaultPiConfig(runtime);

  return (
    <>
      <div className="rounded-lg border">
        <div className="flex items-center justify-between border-b px-4 py-2.5">
          <span className="text-caption font-semibold">
            {t(($) => $.detail.model_connection.title)}
          </span>
          <Badge
            variant="secondary"
            className={
              configured
                ? "bg-success/10 text-success"
                : "bg-warning/10 text-warning"
            }
          >
            {configured ? (
              <CheckCircle2 className="h-3 w-3" />
            ) : (
              <KeyRound className="h-3 w-3" />
            )}
            {configured
              ? t(($) => $.detail.model_connection.ready)
              : t(($) => $.detail.model_connection.needs_setup)}
          </Badge>
        </div>
        <div className="space-y-3 p-4">
          {configured ? (
            <div className="min-w-0">
              <div className="truncate font-mono text-caption font-medium">
                {config.model}
              </div>
              <div className="mt-0.5 truncate text-caption text-muted-foreground">
                {config.provider}
              </div>
            </div>
          ) : (
            <p className="text-caption leading-5 text-muted-foreground">
              {t(($) => $.detail.model_connection.setup_hint)}
            </p>
          )}

          {canEdit ? (
            <Button
              type="button"
              variant={configured ? "outline" : "default"}
              size="sm"
              className="w-full"
              onClick={() => setDialogOpen(true)}
            >
              <Settings2 className="h-3.5 w-3.5" />
              {configured
                ? t(($) => $.detail.model_connection.edit)
                : t(($) => $.detail.model_connection.configure)}
            </Button>
          ) : (
            !configured && (
              <p className="text-caption text-muted-foreground">
                {t(($) => $.detail.model_connection.read_only_hint)}
              </p>
            )
          )}
        </div>
      </div>

      {canEdit && (
        <PiModelConnectionDialog
          runtime={runtime}
          wsId={wsId}
          open={dialogOpen}
          onOpenChange={setDialogOpen}
        />
      )}
    </>
  );
}
