import { describe, expect, it } from "vitest";
import { agentChatUrl } from "./message-agent";

describe("agentChatUrl", () => {
  it("targets a new inbox chat for the selected agent", () => {
    expect(agentChatUrl("/acme/inbox", "agent/a b")).toBe(
      "/acme/inbox?chat=new-chat&agent=agent%2Fa%20b",
    );
  });
});
