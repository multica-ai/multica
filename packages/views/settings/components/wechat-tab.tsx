"use client";

import { useQuery } from "@tanstack/react-query";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { useWorkspaceId } from "@multica/core/hooks";
import { wechatInstallationsOptions } from "@multica/core/wechat";

export function WechatTab() {
  const wsId = useWorkspaceId();

  const { data, isLoading } = useQuery({
    ...wechatInstallationsOptions(wsId),
    enabled: !!wsId,
  });
  const configured = data?.configured === true;
  const activeCount =
    data?.installations.filter((i) => i.status === "active").length ?? 0;

  if (isLoading) {
    return (
      <Card>
        <CardContent>
          <p className="text-sm text-muted-foreground">Loading...</p>
        </CardContent>
      </Card>
    );
  }

  if (!configured) {
    return (
      <Card>
        <CardContent className="space-y-2">
          <p className="text-sm font-medium">未启用企微集成</p>
          <p className="text-xs text-muted-foreground">
            需要在服务器上设置{" "}
            <code className="rounded bg-muted px-1 py-0.5 text-[10px]">
              MULTICA_WECHAT_SECRET_KEY
            </code>{" "}
            才能启用企业微信 Bot 绑定。
          </p>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardContent className="space-y-2">
        <p className="text-sm font-medium">已启用</p>
        <p className="text-xs text-muted-foreground">
          {activeCount === 0
            ? "暂无机器人连接。前往智能体的「集成」选项卡绑定企业微信机器人。"
            : `${activeCount} 个机器人已连接。在各智能体的「集成」选项卡中管理绑定。`}
        </p>
      </CardContent>
    </Card>
  );
}
