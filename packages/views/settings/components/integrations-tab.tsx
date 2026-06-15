"use client";

import { LarkTab } from "./lark-tab";
import { WechatTab } from "./wechat-tab";
import { useT } from "../../i18n";

export function IntegrationsTab() {
  const { t } = useT("settings");
  return (
    <div className="space-y-10">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">{t(($) => $.lark.section_title)}</h2>
        <LarkTab />
      </section>
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">
          {t(($) => $.wechat.section_title)}
        </h2>
        <p className="text-sm text-muted-foreground">
          {t(($) => $.wechat.page_description)}
        </p>
        <WechatTab />
      </section>
    </div>
  );
}
