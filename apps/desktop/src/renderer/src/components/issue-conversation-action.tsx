import { MessageSquare, Monitor } from "lucide-react";
import { toast } from "sonner";
import type { Issue } from "@multica/core/types";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { getCurrentWsId } from "@multica/core/platform";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";
import { useT } from "@multica/views/i18n";
import { DropdownMenuItem, DropdownMenuSub, DropdownMenuSubTrigger, DropdownMenuSubContent } from "@multica/ui/components/ui/dropdown-menu";
import { deviceName, useLinkedDevicesQuery } from "../platform/linked-devices";
import { conversationService } from "../platform/conversation-services";
import { localV2Transport } from "../platform/local-v2-transport";
import { issueConversationPendingKey, launchIssueConversation } from "../platform/issue-conversation";
import { getActiveTab, useTabStore } from "../stores/tab-store";
import { useIssueConversationStore } from "../stores/issue-conversation-store";

export function DesktopIssueConversationAction({ issue }: { issue: Issue }) {
  const { t } = useT("issues");
  const devicesQuery = useLinkedDevicesQuery();
  const devices = devicesQuery.data ?? [];
  const userId = useAuthStore((state) => state.user?.id);
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const pendingKey = issueConversationPendingKey(userId ?? "", issue);
  const pending = useIssueConversationStore((state) => !!state.pending[pendingKey]);
  const disabled = pending || !userId || devicesQuery.isPending || devicesQuery.isError;
  const title = devicesQuery.isError ? t(($) => $.actions.conversation_devices_failed) : undefined;
  const launch = (deviceId: string) => {
    if (!userId) return;
    const origin = getActiveTab(useTabStore.getState());
    const originUrl = origin?.url;
    const apiUrl = api.getBaseUrl();
    const isCurrent = () => {
      const current = getActiveTab(useTabStore.getState());
      return api.getBaseUrl() === apiUrl && useAuthStore.getState().user?.id === userId
        && getCurrentWsId() === issue.workspace_id && current?.id === origin?.id && current?.url === originUrl;
    };
    const service = conversationService(localV2Transport(deviceId), { userId, workspaceId: issue.workspace_id });
    void launchIssueConversation({
      issue, userId, deviceId, service, isCurrent,
      unavailableMessage: t(($) => $.actions.conversation_unavailable),
      open: (summary) => {
        const flow = deviceId === "local" ? paths.conversationFlow() : paths.conversationFlowDevice(deviceId);
        const path = `${flow}?${new URLSearchParams({ session: summary.conversation_id, provider: summary.provider })}`;
        navigation.openInNewTab?.(path, undefined, { activate: true });
        // Tabs deduplicate by pathname and keep the previous session's query.
        navigation.replace(path);
      },
    }).catch((error: unknown) => {
      if (isCurrent()) toast.error(t(($) => $.actions.conversation_create_failed, { message: error instanceof Error ? error.message : String(error) }));
    });
  };
  const label = t(($) => $.actions.open_ai_conversation);
  if (!devices.length) return (
    <DropdownMenuItem disabled={disabled} title={title} onClick={() => launch("local")}>
      <MessageSquare className="h-3.5 w-3.5" />{label}
    </DropdownMenuItem>
  );
  return (
    <DropdownMenuSub>
      <DropdownMenuSubTrigger disabled={disabled} title={title}>
        <MessageSquare className="h-3.5 w-3.5" />{label}
      </DropdownMenuSubTrigger>
      <DropdownMenuSubContent>
        <DropdownMenuItem onClick={() => launch("local")}><Monitor className="h-3.5 w-3.5" />{t(($) => $.actions.conversation_local_device)}</DropdownMenuItem>
        {devices.map((device) => <DropdownMenuItem key={device.peer_id} onClick={() => launch(device.peer_id)}><Monitor className="h-3.5 w-3.5" />{deviceName(devices, device.peer_id)}</DropdownMenuItem>)}
      </DropdownMenuSubContent>
    </DropdownMenuSub>
  );
}
