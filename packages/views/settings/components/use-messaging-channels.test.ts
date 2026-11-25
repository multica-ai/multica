// @vitest-environment node
import { describe, expect, it } from "vitest";
import { channelChatLink, type ChannelConnection } from "./use-messaging-channels";

describe("channel chat destinations", () => {
  it.each([
    ["feishu", "applink.feishu.cn"],
    ["lark", "applink.larksuite.com"],
    [undefined, "applink.feishu.cn"],
  ])("opens the %s bot conversation with the application id", (region, host) => {
    expect(channelChatLink({ channel: "lark", installation: { app_id: "cli_a&b", region } } as ChannelConnection))
      .toBe(`https://${host}/client/bot/open?appId=cli_a%26b`);
  });

  it("opens Slack in the correct workspace", () => {
    expect(channelChatLink({ channel: "slack", installation: { team_id: "T1", bot_user_id: "U2" } } as ChannelConnection))
      .toBe("https://app.slack.com/client/T1/U2");
  });

  it("does not offer a broken link when a bot identity is missing", () => {
    for (const channel of ["lark", "slack", "telegram", "wecom", "dingtalk"] as const) {
      expect(channelChatLink({ channel, installation: {} } as ChannelConnection)).toBeUndefined();
    }
  });
});
