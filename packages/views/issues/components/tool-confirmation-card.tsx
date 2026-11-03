"use client";

import { useState } from "react";
import { toast } from "sonner";
import { useDecideToolConfirmation } from "@multica/core/issues/mutations";
import type { TaskToolConfirmation } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useT } from "../../i18n";

const MAX_DENY_MESSAGE = 2000;

function formatInput(input: unknown): string {
  if (input == null) return "";
  if (typeof input === "string") return input;
  try {
    return JSON.stringify(input, null, 2);
  } catch {
    return String(input);
  }
}

export function ToolConfirmationCard({
  issueId,
  taskId,
  confirmation,
}: {
  issueId: string;
  taskId: string;
  confirmation: TaskToolConfirmation;
}) {
  const { t } = useT("issues");
  const decide = useDecideToolConfirmation(issueId);
  const [denying, setDenying] = useState(false);
  const [message, setMessage] = useState("");
  const tool = confirmation.mcp_server_name
    ? `${confirmation.mcp_server_name}/${confirmation.tool_name}`
    : confirmation.tool_name;
  const input = formatInput(confirmation.input);

  const submit = (decision: "allow" | "deny") => {
    decide.mutate(
      {
        taskId,
        callId: confirmation.call_id,
        decision,
        message: decision === "deny" ? message.trim() || undefined : undefined,
      },
      {
        onError: (e) =>
          toast.error(e instanceof Error ? e.message : t(($) => $.tool_confirmation.failed)),
      },
    );
  };

  return (
    <div
      role="group"
      aria-label={t(($) => $.tool_confirmation.title, { tool })}
      className="mx-2 my-1 space-y-2 rounded-md border bg-muted/40 p-2"
    >
      <p className="text-caption font-medium break-all">
        {t(($) => $.tool_confirmation.title, { tool })}
      </p>
      {input && (
        <pre className="max-h-24 overflow-auto rounded-xs bg-background p-1.5 text-caption whitespace-pre-wrap break-all">
          {input}
        </pre>
      )}
      {denying && (
        <Input
          autoFocus
          value={message}
          maxLength={MAX_DENY_MESSAGE}
          onChange={(e) => setMessage(e.target.value)}
          placeholder={t(($) => $.tool_confirmation.deny_placeholder)}
          aria-label={t(($) => $.tool_confirmation.deny_placeholder)}
        />
      )}
      <div className="flex justify-end gap-2">
        {denying ? (
          <>
            <Button size="sm" variant="ghost" disabled={decide.isPending} onClick={() => setDenying(false)}>
              {t(($) => $.tool_confirmation.back)}
            </Button>
            <Button size="sm" variant="destructive" disabled={decide.isPending} onClick={() => submit("deny")}>
              {t(($) => $.tool_confirmation.deny)}
            </Button>
          </>
        ) : (
          <>
            <Button size="sm" variant="outline" disabled={decide.isPending} onClick={() => setDenying(true)}>
              {t(($) => $.tool_confirmation.deny)}
            </Button>
            <Button size="sm" disabled={decide.isPending} onClick={() => submit("allow")}>
              {t(($) => $.tool_confirmation.allow)}
            </Button>
          </>
        )}
      </div>
    </div>
  );
}
