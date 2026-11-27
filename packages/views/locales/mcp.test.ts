// @vitest-environment node

import { describe, expect, it } from "vitest";
import { SUPPORTED_LOCALES } from "@multica/core/i18n";
import { createI18n } from "@multica/core/i18n/react";
import enAgents from "./en/agents.json";
import enSettings from "./en/settings.json";
import { RESOURCES } from "./index";

const AGENT_KEYS = Object.keys(enAgents.tab_body.mcp_config) as Array<
  keyof typeof enAgents.tab_body.mcp_config
>;
const SETTINGS_KEYS = Object.keys(enSettings.mcp) as Array<
  keyof typeof enSettings.mcp
>;

describe("MCP locale contracts", () => {
  it.each(SUPPORTED_LOCALES)(
    "resolves every MCP message without fallback or unexpanded tokens in %s",
    (locale) => {
      const i18n = createI18n(locale, RESOURCES);
      const messages = [
        ...AGENT_KEYS.map((key) => ({
          namespace: "agents",
          key: `tab_body.mcp_config.${key}`,
          source: enAgents.tab_body.mcp_config[key],
          resolve: (params: Record<string, string>) =>
            i18n.t(($) => $.tab_body.mcp_config[key], {
              ns: "agents",
              ...params,
            }),
        })),
        ...SETTINGS_KEYS.map((key) => ({
          namespace: "settings",
          key: `mcp.${key}`,
          source: enSettings.mcp[key],
          resolve: (params: Record<string, string>) =>
            i18n.t(($) => $.mcp[key], { ns: "settings", ...params }),
        })),
      ];
      for (const { namespace, key, source, resolve } of messages) {
        const context = `${locale}:${namespace}:${key}`;
        // Check the selected bundle directly: an English fallback can make
        // i18next.t appear successful after a translation has disappeared.
        expect(i18n.getResource(locale, namespace, key), context).toBeTypeOf(
          "string",
        );
        const tokens = [...source.matchAll(/\{\{(\w+)\}\}/g)].map(
          (match) => match[1]!,
        );
        const params = Object.fromEntries(
          tokens.map((token) => [token, `test-${token}`]),
        );
        const resolved = resolve(params);
        expect(resolved.trim().length, context).toBeGreaterThan(0);
        expect(resolved, context).not.toBe(key);
        expect(resolved, context).not.toContain("{{");
        for (const token of tokens) {
          expect(resolved, context).toContain(`test-${token}`);
        }
      }
    },
  );

  it.each([
    ["en", "Rename", "Edit configuration", "Replace configuration"],
    ["zh-Hans", "重命名", "编辑配置", "替换配置"],
    ["ja", "名前を変更", "設定を編集", "設定を置き換え"],
    ["ko", "이름 변경", "설정 편집", "설정 교체"],
  ] as const)(
    "distinguishes rename, readable editing, and replacement actions in %s",
    (locale, rename, edit, replace) => {
      const i18n = createI18n(locale, RESOURCES);
      const agentT = i18n.getFixedT(locale, "agents");
      const settingsT = i18n.getFixedT(locale, "settings");

      expect(agentT(($) => $.tab_body.mcp_config.rename_action)).toBe(rename);
      expect(settingsT(($) => $.mcp.rename_action)).toBe(rename);
      expect(agentT(($) => $.tab_body.mcp_config.edit_config)).toBe(edit);
      expect(agentT(($) => $.tab_body.mcp_config.dialog_replace_action)).toBe(
        replace,
      );
      expect(settingsT(($) => $.mcp.replace_config)).toBe(replace);
      expect(new Set([rename, edit, replace]).size).toBe(3);
    },
  );

  it.each(SUPPORTED_LOCALES)(
    "identifies the target server in edit and replacement titles in %s",
    (locale) => {
      const i18n = createI18n(locale, RESOURCES);
      const name = "org.example-mcp";
      const edit = i18n.t(($) => $.tab_body.mcp_config.dialog_edit_title_named, {
        ns: "agents",
        name,
      });
      const replace = i18n.t(($) => $.tab_body.mcp_config.dialog_replace_title, {
        ns: "agents",
        name,
      });
      expect(edit).toContain(name);
      expect(replace).toContain(name);
      expect(edit).not.toBe(replace);
    },
  );
});
