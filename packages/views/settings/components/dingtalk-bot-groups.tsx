"use client";

import { useState } from "react";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ChevronDown, ChevronRight } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { dingtalkKeys } from "@multica/core/dingtalk";
import { api } from "@multica/core/api";
import type { DingTalkGroup, DingTalkGroupBot } from "@multica/core/types";
import { useT, useTimeAgo } from "../../i18n";

export function getDingTalkBotIdentity(
  groups: DingTalkGroup[],
  installationId: string,
): DingTalkGroupBot | undefined {
  const bots = groups.flatMap((group) =>
    group.bots.filter((bot) => bot.installation_id === installationId),
  );
  const identity = bots.find((bot) => bot.bot_name) ?? bots[0];
  const issue = bots.find((bot) => bot.bot_identity_issue)?.bot_identity_issue;
  return identity
    ? { ...identity, bot_identity_issue: issue ?? identity.bot_identity_issue }
    : undefined;
}

export function DingTalkBotGroups({
  workspaceId,
  agentId,
  installationId,
  groups,
  inactiveCount = 0,
  canForget = false,
  isLoading,
  isError,
  onRetry,
  showDescription = true,
  className,
}: {
  workspaceId: string;
  agentId?: string;
  installationId: string;
  groups: DingTalkGroup[];
  inactiveCount?: number;
  canForget?: boolean;
  isLoading: boolean;
  isError: boolean;
  onRetry: () => void;
  showDescription?: boolean;
  className?: string;
}) {
  const { t } = useT("settings");
  const timeAgo = useTimeAgo();
  const qc = useQueryClient();
  const [inactiveExpanded, setInactiveExpanded] = useState(false);
  const [forgetTarget, setForgetTarget] = useState<DingTalkGroup | null>(null);
  const [forgetting, setForgetting] = useState(false);
  const inactiveQuery = useInfiniteQuery({
    queryKey: agentId
      ? dingtalkKeys.agentInactiveGroups(workspaceId, agentId, installationId)
      : dingtalkKeys.inactiveGroups(workspaceId, installationId),
    initialPageParam: 0,
    queryFn: ({ pageParam }) => {
      const params = {
        activity: "inactive" as const,
        installationId,
        offset: pageParam,
      };
      return agentId
        ? api.listAgentDingTalkGroups(agentId, params)
        : api.listDingTalkGroups(workspaceId, params);
    },
    getNextPageParam: (page) => page.next_offset,
    enabled: inactiveExpanded && inactiveCount > 0,
  });

  const collectObserved = (source: DingTalkGroup[]) =>
    source
      .flatMap((group) => {
        const bot = group.bots.find(
          (candidate) => candidate.installation_id === installationId,
        );
        return bot ? [{ group, bot }] : [];
      })
      .sort((left, right) => {
        const leftActiveAt = Date.parse(left.bot.last_active_at ?? "");
        const rightActiveAt = Date.parse(right.bot.last_active_at ?? "");
        const leftIsActive = !Number.isNaN(leftActiveAt);
        const rightIsActive = !Number.isNaN(rightActiveAt);

        if (leftIsActive && rightIsActive && leftActiveAt !== rightActiveAt) {
          return rightActiveAt - leftActiveAt;
        }
        if (leftIsActive !== rightIsActive) return leftIsActive ? -1 : 1;

        const leftTitle = left.group.conversation_title;
        const rightTitle = right.group.conversation_title;
        if (leftTitle !== rightTitle) {
          if (!leftTitle) return 1;
          if (!rightTitle) return -1;
          return leftTitle < rightTitle ? -1 : 1;
        }
        if (left.group.conversation_id === right.group.conversation_id)
          return 0;
        return left.group.conversation_id < right.group.conversation_id
          ? -1
          : 1;
      });
  const observed = collectObserved(groups);
  const inactiveObserved = collectObserved(
    inactiveQuery.data?.pages.flatMap((page) => page.groups) ?? [],
  );

  async function handleForget() {
    if (!forgetTarget || forgetting) return;
    setForgetting(true);
    try {
      await api.forgetDingTalkGroup(
        workspaceId,
        installationId,
        forgetTarget.conversation_id,
      );
      await qc.invalidateQueries({
        queryKey: dingtalkKeys.groups(workspaceId),
      });
      toast.success(t(($) => $.dingtalk.group_forget_success));
      setForgetTarget(null);
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t(($) => $.dingtalk.group_forget_failed),
      );
    } finally {
      setForgetting(false);
    }
  }

  const renderObserved = (
    entries: Array<{ group: DingTalkGroup; bot: DingTalkGroupBot }>,
  ) => (
    <div>
      {entries.map(({ group, bot }) => (
        <div
          key={group.conversation_id}
          className="group space-y-1.5 border-t py-2.5"
          data-testid="dingtalk-group-item"
        >
          <div className="flex min-w-0 items-baseline justify-between gap-3">
            <div className="flex min-w-0 flex-1 items-baseline gap-3">
              <p className="min-w-0 truncate text-caption font-medium">
                {group.conversation_title ||
                  t(($) => $.dingtalk.group_untitled)}
              </p>
              {canForget && (
                <button
                  type="button"
                  className="pointer-events-none shrink-0 text-micro text-muted-foreground opacity-0 underline-offset-2 transition-opacity group-hover:pointer-events-auto group-hover:opacity-100 group-focus-within:pointer-events-auto group-focus-within:opacity-100 hover:text-foreground hover:underline focus-visible:pointer-events-auto focus-visible:opacity-100"
                  onClick={() => setForgetTarget(group)}
                >
                  {t(($) => $.dingtalk.group_forget)}
                </button>
              )}
            </div>
            <div className="flex min-w-0 max-w-[60%] items-center">
              <TooltipProvider delay={0}>
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <code
                        tabIndex={0}
                        translate="no"
                        aria-label={`${t(($) => $.dingtalk.conversation_id_label)} ${group.conversation_id}`}
                        className="block min-w-0 truncate font-mono text-micro text-faint-foreground transition-colors group-hover:text-muted-foreground focus-visible:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                      >
                        {group.conversation_id}
                      </code>
                    }
                  />
                  <TooltipContent side="top">
                    {t(($) => $.dingtalk.conversation_id_label)}
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
            </div>
          </div>
          {bot.last_active_at && (
            <p
              className="text-micro tabular-nums text-muted-foreground"
              data-testid="dingtalk-group-activity"
            >
              {t(($) => $.dingtalk.group_last_active, {
                time: timeAgo(bot.last_active_at),
              })}
              <span className="mx-1.5" aria-hidden="true">
                ·
              </span>
              {t(($) => $.dingtalk.group_mentions, {
                count: bot.mention_count ?? 0,
              })}
            </p>
          )}
        </div>
      ))}
    </div>
  );

  return (
    <div
      className={cn("space-y-2 pt-3", className)}
      data-testid="dingtalk-bot-groups"
    >
      {showDescription && (
        <p className="text-caption leading-relaxed text-muted-foreground">
          {t(($) => $.dingtalk.groups_description)}
        </p>
      )}
      {isLoading ? (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.dingtalk.groups_loading)}
        </p>
      ) : isError ? (
        <div className="flex items-center justify-between gap-3" role="alert">
          <p className="text-caption text-muted-foreground">
            {t(($) => $.dingtalk.groups_error)}
          </p>
          <button
            type="button"
            className="shrink-0 text-caption font-medium underline-offset-2 hover:underline"
            onClick={onRetry}
          >
            {t(($) => $.dingtalk.groups_retry)}
          </button>
        </div>
      ) : observed.length === 0 ? (
        <p className="text-caption text-muted-foreground">
          {inactiveCount > 0
            ? t(($) => $.dingtalk.groups_no_recent)
            : t(($) => $.dingtalk.groups_empty)}
        </p>
      ) : (
        renderObserved(observed)
      )}
      {!isLoading && !isError && inactiveCount > 0 && (
        <div className="border-t pt-2">
          <button
            type="button"
            className="flex w-full items-center gap-1.5 py-1 text-caption font-medium text-muted-foreground hover:text-foreground"
            onClick={() => setInactiveExpanded((value) => !value)}
            aria-expanded={inactiveExpanded}
          >
            {inactiveExpanded ? (
              <ChevronDown className="h-3.5 w-3.5" aria-hidden="true" />
            ) : (
              <ChevronRight className="h-3.5 w-3.5" aria-hidden="true" />
            )}
            {t(($) => $.dingtalk.groups_inactive, { count: inactiveCount })}
          </button>
          {inactiveExpanded && (
            <div className="pt-1">
              {inactiveQuery.isLoading ? (
                <p className="py-2 text-caption text-muted-foreground">
                  {t(($) => $.dingtalk.groups_loading)}
                </p>
              ) : inactiveQuery.isError ? (
                <button
                  type="button"
                  className="py-2 text-caption font-medium underline-offset-2 hover:underline"
                  onClick={() => void inactiveQuery.refetch()}
                >
                  {t(($) => $.dingtalk.groups_retry)}
                </button>
              ) : (
                <>
                  {renderObserved(inactiveObserved)}
                  {inactiveQuery.hasNextPage && (
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      disabled={inactiveQuery.isFetchingNextPage}
                      onClick={() => void inactiveQuery.fetchNextPage()}
                    >
                      {inactiveQuery.isFetchingNextPage
                        ? t(($) => $.dingtalk.groups_loading)
                        : t(($) => $.dingtalk.groups_load_more)}
                    </Button>
                  )}
                </>
              )}
            </div>
          )}
        </div>
      )}
      <AlertDialog
        open={!!forgetTarget}
        onOpenChange={(open) => {
          if (!open && !forgetting) setForgetTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.dingtalk.group_forget_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.dingtalk.group_forget_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={forgetting}>
              {t(($) => $.dingtalk.group_forget_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleForget} disabled={forgetting}>
              {t(($) => $.dingtalk.group_forget_confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
