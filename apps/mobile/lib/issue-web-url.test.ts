// @vitest-environment node
import { expect, it } from "vitest";
import { useServerConfigStore } from "@/data/server-config-store";
import { issueWebUrl } from "./issue-web-url";

it("copies canonical issue/comment links on the independently chosen Web host and base path", () => {
  useServerConfigStore.setState({ config: {
    apiUrl: "https://api.example.test:8443/backend",
    webUrl: "https://web.example.test:9443/ui",
  } });
  expect(issueWebUrl("acceptance", "DAT-1")).toBe("https://web.example.test:9443/ui/acceptance/issues/DAT-1");
  expect(issueWebUrl("acceptance", "DAT-1", "comment-1"))
    .toBe("https://web.example.test:9443/ui/acceptance/issues/DAT-1#comment-comment-1");
});
