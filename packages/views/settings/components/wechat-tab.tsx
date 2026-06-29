"use client";

import { useQuery } from "@tanstack/react-query";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { useWorkspaceId } from "@multica/core/hooks";
import { wechatInstallationsOptions } from "@multica/core/wechat";
import { useT } from "../../i18n";

export function WechatTab() {
  const wsId = useWorkspaceId();
  const { t } = useT("settings");

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
          <p className="text-sm text-muted-foreground">
            {t(($) => $.wechat.loading)}
          </p>
        </CardContent>
      </Card>
    );
  }

  if (!configured) {
    return (
      <Card>
        <CardContent className="space-y-2">
          <p className="text-sm font-medium">
            {t(($) => $.wechat.not_configured_title)}
          </p>
          <p className="text-xs text-muted-foreground">
            {t(($) => $.wechat.not_configured_description_prefix)}{" "}
            <code className="rounded bg-muted px-1 py-0.5 text-[10px]">
              MULTICA_WECHAT_SECRET_KEY
            </code>{" "}
            {t(($) => $.wechat.not_configured_description_suffix)}
          </p>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardContent className="space-y-2">
        <p className="text-sm font-medium">
          {t(($) => $.wechat.enabled)}
        </p>
        <p className="text-xs text-muted-foreground">
          {activeCount === 0
            ? t(($) => $.wechat.no_bots)
            : t(($) => $.wechat.bots_connected, { count: activeCount })}
        </p>
      </CardContent>
    </Card>
  );
}
