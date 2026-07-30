"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, MessageCircle, RotateCcw, Unlink } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  issueChannelTopicOptions,
  issueKeys,
} from "@multica/core/issues/queries";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

function mutationError(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

export function IssueChannelTopicSection({
  issueId,
  canManage,
}: {
  issueId: string;
  canManage: boolean;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { data } = useQuery(issueChannelTopicOptions(wsId, issueId));
  const binding = data?.channel_topic_binding ?? null;

  const invalidate = () =>
    qc.invalidateQueries({ queryKey: issueKeys.channelTopic(wsId, issueId) });

  const unbind = useMutation({
    mutationFn: () => api.deleteIssueChannelTopicBinding(issueId),
    onSuccess: async () => {
      await invalidate();
      toast.success(t(($) => $.feishu_topic.toast_unbound));
    },
    onError: (error) => {
      toast.error(mutationError(error, t(($) => $.feishu_topic.toast_unbind_failed)));
    },
  });

  const enable = useMutation({
    mutationFn: () => api.enableIssueChannelTopicBinding(issueId),
    onSuccess: async () => {
      await invalidate();
      toast.success(t(($) => $.feishu_topic.toast_enabled));
    },
    onError: (error) => {
      toast.error(mutationError(error, t(($) => $.feishu_topic.toast_enable_failed)));
    },
  });

  const stateLabel =
    binding?.state === "active"
      ? t(($) => $.feishu_topic.state_active)
      : binding?.state === "manual_unbound"
        ? t(($) => $.feishu_topic.state_paused)
        : binding
          ? t(($) => $.feishu_topic.state_historical)
          : t(($) => $.feishu_topic.state_none);

  return (
    <div>
      <div className="mb-2 flex items-center gap-1 px-2 py-1 text-xs font-medium">
        <MessageCircle className="size-3.5 text-muted-foreground" />
        {t(($) => $.feishu_topic.section_title)}
      </div>
      <div className="space-y-2 rounded-lg border bg-muted/20 p-3 text-xs">
        <div className="flex items-center justify-between gap-2">
          <span className="text-muted-foreground">{stateLabel}</span>
          {binding && (
            <span className="rounded-full bg-muted px-2 py-0.5 text-[10px] text-muted-foreground">
              {binding.binding_source}
            </span>
          )}
        </div>
        {binding ? (
          <div className="space-y-1 text-[11px] text-muted-foreground">
            <p>{t(($) => $.feishu_topic.chat_label, { id: binding.chat_id })}</p>
            <p className="truncate" title={binding.topic_root_message_id}>
              {t(($) => $.feishu_topic.root_label, { id: binding.topic_root_message_id })}
            </p>
          </div>
        ) : (
          <p className="text-[11px] text-muted-foreground">
            {t(($) => $.feishu_topic.empty_hint)}
          </p>
        )}

        {canManage && binding?.state === "active" && (
          <Button
            size="sm"
            variant="outline"
            onClick={() => unbind.mutate()}
            disabled={unbind.isPending}
          >
            {unbind.isPending ? <Loader2 className="animate-spin" /> : <Unlink />}
            {t(($) => $.feishu_topic.unbind)}
          </Button>
        )}
        {canManage && binding?.state === "manual_unbound" && (
          <Button
            size="sm"
            variant="outline"
            onClick={() => enable.mutate()}
            disabled={enable.isPending}
          >
            {enable.isPending ? <Loader2 className="animate-spin" /> : <RotateCcw />}
            {t(($) => $.feishu_topic.enable)}
          </Button>
        )}
      </div>
    </div>
  );
}
