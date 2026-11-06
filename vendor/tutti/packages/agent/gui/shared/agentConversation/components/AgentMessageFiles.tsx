import { Button } from "@multica/ui/components/ui/button";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { File, LoaderCircle } from "lucide-react";
import { observer } from "mobx-react-lite";
import { useState } from "react";
import styles from "../../../agent-gui/agentGuiNode/AgentGUIConversation.styles";
import { useOptionalConversationServices } from "../../../conversationServices";
import { useTranslation } from "../../../i18n/index";
import type { AgentMessageContentVM, AgentMessageFileVM } from "../contracts/agentMessageRowVM";

export const AgentUserFileList = observer(function AgentUserFileList({ message, onLinkClick }: {
  message: AgentMessageContentVM;
  onLinkClick: (url: string) => void;
}) {
  return <div className="flex max-w-full flex-col items-end gap-2">
    {message.files?.map((file) => <AgentUserFile key={file.id} file={file} onLinkClick={onLinkClick} />)}
  </div>;
});

const AgentUserFile = observer(function AgentUserFile({ file, onLinkClick }: {
  file: AgentMessageFileVM;
  onLinkClick: (url: string) => void;
}) {
  const runtime = useOptionalConversationServices();
  const { t } = useTranslation();
  const [pending, setPending] = useState(false);
  const [failed, setFailed] = useState(false);
  const reader = runtime?.attachments?.readSessionAttachmentSource;
  const canRead = Boolean(file.url || (reader && file.workspaceId && file.attachmentId));
  const canRetry = Boolean(file.metadataUnavailable && runtime && file.workspaceId);
  const run = async (action: () => Promise<void>) => {
    setPending(true);
    setFailed(false);
    try { await action(); } catch { setFailed(true); } finally { setPending(false); }
  };
  const open = usePersistCallback(() => run(async () => {
    const url = file.url || (await reader!({ workspaceId: file.workspaceId!, attachmentId: file.attachmentId! })).url;
    if (!url) throw new Error("Attachment URL unavailable");
    onLinkClick(url);
  }));
  const retry = usePersistCallback(() => run(async () => {
    await runtime!.conversations.load(file.workspaceId!, file.agentSessionId);
  }));
  return <div className={`${styles.userMessageBubble} flex max-w-full items-center gap-2`} data-agent-file-attachment={file.attachmentId}>
    <File className="size-4 shrink-0" aria-hidden="true" />
    <Button variant="link" size="sm" className="min-w-0 max-w-full" disabled={!canRead || pending} onClick={open}>
      <span className="truncate">{file.name || t("agentHost.agentGui.mentionKindFile")}</span>
    </Button>
    {(canRetry || failed) && <Button variant="ghost" size="sm" disabled={pending} aria-busy={pending} onClick={canRetry ? retry : open}>
      {pending && <LoaderCircle className="size-4 animate-spin" aria-hidden="true" />}
      {t("agentHost.agentGui.retryActivation")}
    </Button>}
  </div>;
});
