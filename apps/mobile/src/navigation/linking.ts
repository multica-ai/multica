import * as Linking from "expo-linking";
import { MOBILE_ENV } from "../runtime/env";

export const linking = {
  prefixes: [
    Linking.createURL("/"),
    `${MOBILE_ENV.appScheme}://`,
    "wujieai_multicam://",
  ],
  config: {
    screens: {
      Main: {
        screens: {
          Issues: "issues",
          Mine: "mine",
        },
      },
      IssueDetail: "issues/:issueId",
      IssueProperties: "issues/:issueId/properties",
      IssueTaskTranscript: "issues/:issueId/runs/:taskId",
      Search: "search",
      Runtimes: "runtimes",
      Agents: "agents",
      Inbox: "inbox",
      InboxDetail: "inbox/:inboxItemId",
      Setting: "setting",
    },
  },
};
