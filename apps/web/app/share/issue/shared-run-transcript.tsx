import { useState } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { api, ApiError, type SharedIssueRun } from "@multica/core/api";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@multica/ui/components/ui/dialog";
import { ScrollText } from "@multica/ui/icons";
import { useT } from "@multica/views/i18n";

const SHARED_MESSAGE_TYPES = new Set(["text", "text_delta", "commentary"]);

export function SharedRunTranscript({ run, userId, token, label, iconOnly = false }: {
  run: SharedIssueRun;
  userId: string;
  token: string;
  label: string;
  iconOnly?: boolean;
}) {
  const [open, setOpen] = useState(false);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger
        render={<Button type="button" variant="ghost" size={iconOnly ? "icon-sm" : "xs"} aria-label={label} title={label} />}
      >
        <ScrollText aria-hidden />
        {!iconOnly && label}
      </DialogTrigger>
      <DialogContent className="sm:max-w-2xl">
        {open && <TranscriptContent run={run} userId={userId} token={token} />}
      </DialogContent>
    </Dialog>
  );
}

function TranscriptContent({ run, userId, token }: {
  run: SharedIssueRun;
  userId: string;
  token: string;
}) {
  const { t } = useT("issues");
  const { t: agentsT } = useT("agents");
  const transcript = useInfiniteQuery({
    queryKey: ["shared-issue", userId, token, "run", run.id],
    queryFn: ({ pageParam, signal }) => api.listSharedIssueRunMessages(token, run.id, pageParam, signal),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (page) => page.next_after ?? undefined,
    gcTime: 0,
    retry: false,
  });
  const unavailable = transcript.error instanceof ApiError && [403, 404].includes(transcript.error.status);
  const messages = transcript.data?.pages.flatMap(
    (page) => page.messages.filter((message) => SHARED_MESSAGE_TYPES.has(message.type)),
  ) ?? [];
  const content = messages.map((message, index) => {
    const previous = messages[index - 1];
    const continuesText = previous && message.type === "text_delta" &&
      previous.type !== "commentary" && message.seq === previous.seq + 1;
    return index && !continuesText ? `\n\n${message.content}` : message.content;
  }).join("");

  return (
    <>
      <DialogHeader>
        <DialogTitle>{agentsT(($) => $.transcript.dialog_title)}</DialogTitle>
        <DialogDescription>{run.agent_name || t(($) => $.workflows.handler.agent)}</DialogDescription>
      </DialogHeader>
      {transcript.isPending && <p role="status" className="text-caption">{agentsT(($) => $.transcript.waiting_events)}</p>}
      {unavailable ? (
        <p role="alert">{t(($) => $.sharing.unavailable)}</p>
      ) : (
        <>
          {content && <pre className="whitespace-pre-wrap break-words rounded-md bg-muted p-3 text-caption">{content}</pre>}
          {transcript.isSuccess && !content && <p className="text-caption">{agentsT(($) => $.transcript.no_data)}</p>}
          {transcript.isError && (
            <div className="space-y-2">
              <p role="alert">{t(($) => $.execution_log.load_failed_retry)}</p>
              <Button variant="outline" size="sm" onClick={() => void transcript.refetch()}>
                {t(($) => $.sharing.retry)}
              </Button>
            </div>
          )}
          {transcript.hasNextPage && !transcript.isError && (
            <Button
              className="justify-self-start"
              variant="outline"
              size="sm"
              disabled={transcript.isFetchingNextPage}
              onClick={() => void transcript.fetchNextPage()}
            >
              {t(($) => $.sharing.load_more)}
            </Button>
          )}
        </>
      )}
    </>
  );
}
