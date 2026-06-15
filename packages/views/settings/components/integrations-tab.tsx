"use client";

import { LarkTab } from "./lark-tab";
import { WechatTab } from "./wechat-tab";
import { useT } from "../../i18n";

// Integrations is the umbrella tab for third-party platform connections.
// GitHub has its own top-level tab (see github-tab.tsx); everything else
// — currently just Lark, with Slack/Linear etc. to follow — lives in
// here under its own section heading so additional integrations slot in
// without changing the IA. IntegrationsTab is just the host; each
// integration owns its own description and install flow.
export function IntegrationsTab() {
  const { t } = useT("settings");
  return (
    <div className="space-y-10">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">{t(($) => $.lark.section_title)}</h2>
        <LarkTab />
      </section>
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">企业微信</h2>
        <p className="text-sm text-muted-foreground">
          将 Multica 智能体绑定到企业微信 AI Bot。成员可在企微私聊或群聊中 @Bot 发起对话，智能体会以流式消息实时回复。
        </p>
        <WechatTab />
      </section>
    </div>
  );
}
