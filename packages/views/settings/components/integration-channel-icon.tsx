import { MessagesSquare, Webhook } from "lucide-react";
import { DingTalkMark } from "./dingtalk-mark";
import { WecomMark } from "./wecom-mark";

type IntegrationChannel = "lark" | "slack" | "dingtalk" | "wecom";

export function IntegrationChannelIcon({ channel }: { channel: IntegrationChannel }) {
  const icon = {
    lark: <Webhook className="h-4 w-4" />,
    slack: <MessagesSquare className="h-4 w-4" />,
    dingtalk: <DingTalkMark className="h-5 w-5" />,
    wecom: <WecomMark className="h-4 w-4" />,
  }[channel];

  return (
    <span
      aria-hidden="true"
      data-testid={`integration-channel-icon-${channel}`}
      className="flex size-5 shrink-0 items-center justify-center text-muted-foreground"
    >
      {icon}
    </span>
  );
}
