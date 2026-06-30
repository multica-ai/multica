import type { TFunction } from "i18next";
import { describe, expect, it } from "vitest";
import { createI18n } from "@multica/core/i18n/react";
import enAgents from "./en/agents.json";
import enAutopilots from "./en/autopilots.json";
import enChat from "./en/chat.json";
import enOnboarding from "./en/onboarding.json";
import enSettings from "./en/settings.json";
import enWorkspace from "./en/workspace.json";
import zhAgents from "./zh-Hans/agents.json";
import zhAutopilots from "./zh-Hans/autopilots.json";
import zhChat from "./zh-Hans/chat.json";
import zhOnboarding from "./zh-Hans/onboarding.json";
import zhSettings from "./zh-Hans/settings.json";
import zhWorkspace from "./zh-Hans/workspace.json";

const namespaces = {
  agents: enAgents,
  autopilots: enAutopilots,
  chat: enChat,
  onboarding: enOnboarding,
  settings: enSettings,
  workspace: enWorkspace,
};

const zhNamespaces = {
  agents: zhAgents,
  autopilots: zhAutopilots,
  chat: zhChat,
  onboarding: zhOnboarding,
  settings: zhSettings,
  workspace: zhWorkspace,
};

describe("shared brand interpolation", () => {
  it.each([
    ["en", namespaces],
    ["zh-Hans", zhNamespaces],
  ] as const)("uses the host product name for %s shared copy", (locale, resources) => {
    const i18n = createI18n(
      locale,
      { [locale]: resources },
      { productName: "CoStrict" },
    );

    const agents = i18n.getFixedT(locale, "agents") as TFunction<"agents">;
    const autopilots = i18n.getFixedT(locale, "autopilots") as TFunction<"autopilots">;
    const chat = i18n.getFixedT(locale, "chat") as TFunction<"chat">;
    const onboarding = i18n.getFixedT(locale, "onboarding") as TFunction<"onboarding">;
    const settings = i18n.getFixedT(locale, "settings") as TFunction<"settings">;
    const workspace = i18n.getFixedT(locale, "workspace") as TFunction<"workspace">;

    expect(agents(($) => $.empty.description)).toContain("CoStrict");
    expect(autopilots(($) => $.add_trigger_dialog.webhook_help)).toContain("CoStrict");
    expect(chat(($) => $.fab.running)).toContain("CoStrict");
    expect(onboarding(($) => $.welcome.wordmark)).toContain("CoStrict");
    expect(settings(($) => $.github.page_description)).toContain("CoStrict");
    expect(workspace(($) => $.new_page.title)).toContain("CoStrict");
  });

  it("preserves the names of products outside this migration", () => {
    const i18n = createI18n(
      "en",
      { en: namespaces },
      { productName: "CoStrict" },
    );

    const onboarding = i18n.getFixedT(
      "en",
      "onboarding",
    ) as TFunction<"onboarding">;

    expect(onboarding(($) => $.cli_install.step1_label)).toBe(
      "Install the Multica CLI",
    );
    expect(onboarding(($) => $.step_workspace.next_agent)).toContain(
      "Multica Helper",
    );
  });
});
