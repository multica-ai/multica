import { observer } from "mobx-react-lite";
import type { JSX, RefObject } from "react";
import { CircleHelp, Clock3, Loader2, MonitorSmartphone, MoreHorizontal, Pin } from "lucide-react";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { ContextMenuTrigger } from "@multica/ui/components/ui/context-menu";
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent } from "@multica/ui/components/ui/dropdown-menu";
import { AppLink } from "@multica/views/navigation";
import { ProviderLogo } from "@multica/views/runtimes/provider-logo";
import type { useT } from "@multica/views/i18n";
import type { LocalProviderId } from "@multica/core/local-conversation/protocol";
import type { ConversationService } from "@multica/conversation-client/services";
import type { LocalConversationListModel } from "../platform/local-conversation-list-model";
import type { LocalSessionEntry } from "../platform/local-conversation-projects";

interface LocalConversationSessionRowProps {
  model: LocalConversationListModel;
  id: string;
  selected: boolean;
  sessionLinks: RefObject<Map<string, HTMLAnchorElement>>;
  flowPath: string;
  select(id: string | null, provider?: LocalProviderId): void;
  updatingId: string | null;
  labels: { pinned: string; storage: string };
  projectLabels: { actions: string };
  t: ReturnType<typeof useT<"chat">>["t"];
  renderSessionActions(entry: LocalSessionEntry, contextMenu: boolean): JSX.Element;
  setContextSelection(value: string): void;
  chinese: boolean;
  remote: boolean;
  v2: ConversationService;
}

export const LocalConversationSessionRow = observer(function LocalConversationSessionRow({
  model, id, selected, sessionLinks, flowPath, select, updatingId, labels, projectLabels,
  t, renderSessionActions, setContextSelection, chinese, remote, v2
}: LocalConversationSessionRowProps) {
  const entry = model.activity(id);
  const handleContextMenu = usePersistCallback<
    NonNullable<
      React.ComponentProps<typeof ContextMenuTrigger>["onContextMenu"]
    >
  >((event) => {
    const selection = window.getSelection();
    setContextSelection(
      selection &&
        event.currentTarget.contains(selection.anchorNode) &&
        event.currentTarget.contains(selection.focusNode)
        ? selection.toString()
        : ""
    );
  });
  if (!entry) return null;
  const entryRunning = entry.running;
  const waitingForAnswer = entry.waitingForAnswer;
  const background = entry.backgroundTasks;
  return (
    <ContextMenuTrigger
      className="group relative block select-text"
      data-session-id={entry.id}
      onContextMenu={handleContextMenu}
    >
      <LocalConversationSummary
        model={model}
        id={id}
        sessionLinks={sessionLinks}
        flowPath={flowPath}
        selected={selected}
        updating={updatingId === id}
        select={select}
        labels={labels}
        t={t}
      />
      <div className="absolute right-2 top-1/2 flex -translate-y-1/2 items-center gap-1">
        <div
          className={`focus-within:opacity-100 group-hover:opacity-100 ${updatingId === entry.id ? "opacity-100" : "opacity-0"}`}
        >
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <IconButton
                  label={`${projectLabels.actions}：${entry.title}`}
                  disabled={updatingId !== null}
                  aria-busy={updatingId === entry.id}
                />
              }
            >
              {updatingId === entry.id ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <MoreHorizontal className="size-3.5" />
              )}
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <LocalSessionMenu model={model} id={id} renderActions={renderSessionActions} />
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
        {waitingForAnswer ? (
          <SessionWaitingAction t={t} entry={entry} select={select} />
        ) : (
          entryRunning && (
            <Spinner
              aria-label={
                chinese
                  ? `会话运行中：${entry.title}`
                  : `Conversation running: ${entry.title}`
              }
              className="shrink-0 text-muted-foreground motion-reduce:animate-none"
            />
          )
        )}
        {!remote &&
          (() => {
            const users = v2.list
              .presence(entry.id)
              .map((user) => user.device_name || user.peer_id.slice(-6))
              .join(chinese ? "、" : ", ");
            return (
              users && (
                <span
                  role="status"
                  aria-label={
                    chinese
                      ? `${users} 正在使用：${entry.title}`
                      : `In use on ${users}: ${entry.title}`
                  }
                  title={chinese ? `${users} 正在使用` : `In use on ${users}`}
                  className="flex shrink-0 items-center text-muted-foreground"
                >
                  <MonitorSmartphone aria-hidden="true" className="size-4" />
                </span>
              )
            );
          })()}
        {!entryRunning && background > 0 && (
          <span
            role="status"
            aria-label={
              chinese
                ? `后台运行中：${entry.title}（${background}）`
                : `Background tasks: ${entry.title} (${background})`
            }
            title={chinese ? "后台运行中" : "Running in background"}
            className="flex shrink-0 items-center text-muted-foreground"
          >
            <Clock3 aria-hidden="true" className="size-4" />
          </span>
        )}
      </div>
    </ContextMenuTrigger>
  );
}
);


const LocalConversationSummary = observer(function LocalConversationSummary({
  model, id, sessionLinks, flowPath, selected, updating, select, labels, t
}: Pick<LocalConversationSessionRowProps, "model" | "id" | "sessionLinks" | "flowPath" | "selected" | "select" | "labels" | "t"> & { updating: boolean }) {
  const entry = model.summary(id);
  const registerSessionLink = usePersistCallback<
    Extract<
      NonNullable<React.ComponentProps<typeof AppLink>["ref"]>,
      (...args: never[]) => unknown
    >
  >((node) => {
    if (node) sessionLinks.current.set(id, node);
    else sessionLinks.current.delete(id);
  });
  const selectConversation = usePersistCallback<
    NonNullable<React.ComponentProps<typeof AppLink>["onClick"]>
  >((event) => {
    event.preventDefault();
    if (entry) select(id, entry.provider);
  });
  if (!entry) return null;
  const entryRunning = entry.running;
  const background = entry.backgroundTasks;
  const waitingForAnswer = entry.waitingForAnswer;
  return (
    <AppLink
      ref={registerSessionLink}
      href={`${flowPath}?${new URLSearchParams({ provider: entry.provider, session: id })}`}
      aria-current={selected ? "true" : undefined}
      onClick={selectConversation}
      className={`flex min-w-0 items-start gap-3 rounded-lg px-3 py-3 text-body outline-none focus-visible:ring-2 focus-visible:ring-ring ${entryRunning || background ? "pr-16" : `group-hover:pr-11 group-focus-within:pr-11 ${updating ? "pr-11" : ""}`} ${selected ? "bg-accent text-accent-foreground" : "hover:bg-accent/60"}`}
    >
      <span
        role="img"
        aria-label={entry.provider === "codex" ? "Codex" : "Claude Code"}
        className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-lg border bg-muted/50 text-foreground"
      >
        <ProviderLogo provider={entry.provider} className="size-5" />
      </span>
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-1.5">
          {entry.pinned && (
            <Pin
              aria-label={labels.pinned}
              className="size-3 shrink-0 text-muted-foreground"
            />
          )}
          <span className="truncate font-medium">{entry.title}</span>
        </span>
        <span
          className={`mt-0.5 block truncate text-caption ${waitingForAnswer ? "text-foreground" : "text-muted-foreground"}`}
        >
          {waitingForAnswer ? (
            t(($) => $.desktop.local_conversation.waiting_for_answer)
          ) : (
            entry.preview || labels.storage
          )}
        </span>
      </span>
    </AppLink>
  );
});

const SessionWaitingAction = observer(function SessionWaitingAction({
  t,
  entry,
  select
}: {
  t: ReturnType<typeof useT<"chat">>["t"];
  entry: Pick<LocalSessionEntry, "id" | "title" | "provider">;
  select: (id: string | null, choice?: LocalProviderId) => void;
}) {
  const selectWaitingConversation = usePersistCallback<
    NonNullable<React.ComponentProps<typeof IconButton>["onClick"]>
  >(() => select(entry.id, entry.provider));
  return (
    <IconButton
      size="icon-xs"
      label={t(($) => $.desktop.local_conversation.waiting_for_answer_label, {
        title: entry.title
      })}
      onClick={selectWaitingConversation}
    >
      <CircleHelp aria-hidden="true" className="size-4 text-muted-foreground" />
    </IconButton>
  );
});

const LocalSessionMenu = observer(function LocalSessionMenu({ model, id, renderActions }: {
  model: LocalConversationListModel; id: string;
  renderActions(entry: LocalSessionEntry, contextMenu: boolean): JSX.Element;
}) {
  const entry = model.entry(id);
  return entry ? renderActions(entry, false) : null;
});
