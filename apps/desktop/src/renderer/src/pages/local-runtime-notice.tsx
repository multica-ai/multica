import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Alert, AlertDescription } from "@multica/ui/components/ui/alert";
import type { LocalConversationScope } from "@multica/core/local-conversation/protocol";
import {
  localHostAvailabilityMessage,
  type LocalHostAvailability
} from "../../../shared/local-host-availability";
import { conversationService } from "../platform/conversation-services";
import { LocalV2RequestError } from "@multica/conversation-client/services";
import { useLocalDevice } from "../platform/local-device";
import { observer } from "mobx-react-lite";
export const LocalRuntimeNotice = observer(LocalRuntimeNoticeRender);

export function localRuntimeRestartMessage(chinese: boolean): string {
  return chinese
    ? "桌面后台需要更新，请在运行中的任务结束后完全退出并重新打开 Multica。刷新页面无法完成更新。"
    : "The desktop runtime needs updating. After running tasks finish, fully quit and reopen Multica. Reloading the page does not update it.";
}

function LocalRuntimeNoticeRender({
  scope,
  chinese
}: {
  scope: LocalConversationScope;
  chinese: boolean;
}) {
  const device = useLocalDevice();
  // A daemon older than the local v2 API answers its routes with 404.
  const probe = useQuery({
    queryKey: [
      "local-v2-providers",
      device.id,
      scope.userId,
      scope.workspaceId
    ],
    queryFn: () =>
      conversationService(device.transport, scope).request("GET", "/providers"),
    retry: false,
    staleTime: 60_000
  });
  const outdated =
    probe.error instanceof LocalV2RequestError &&
    probe.error.kind === "http_404";
  const [availability, setAvailability] =
    useState<LocalHostAvailability | null>(() =>
      device.transport.availability()
    );
  useEffect(
    () => device.transport.onAvailability(setAvailability),
    [device.transport]
  );
  const message = outdated
    ? localRuntimeRestartMessage(chinese)
    : localHostAvailabilityMessage(
        availability,
        chinese,
        device.id === "local" ? undefined : device.name
      );
  if (!message) return null;
  return (
    <div className="shrink-0 px-4 py-2">
      <Alert>
        <AlertDescription>{message}</AlertDescription>
      </Alert>
    </div>
  );
}
