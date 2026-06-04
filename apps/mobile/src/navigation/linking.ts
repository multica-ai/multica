import * as Linking from "expo-linking";
import { getMobileIssueLinkBaseUrls, MOBILE_ENV } from "../runtime/env";
import { parseMobileIssueLink } from "./issue-links";

export const linking = {
  prefixes: [
    Linking.createURL("/"),
    `${MOBILE_ENV.appScheme}://`,
    "wujieai-multicam://",
  ],
  filter: (url: string) => !parseMobileIssueLink(url, getMobileIssueLinkBaseUrls()),
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
      IssueTimeline: "issues/:issueId/timeline",
      IssueTaskRuns: "issues/:issueId/runs",
      IssueTaskTranscript: "issues/:issueId/runs/:taskId",
      Search: "search",
      Runtimes: "runtimes",
      Agents: "agents",
      Squads: "squads",
      Inbox: "inbox",
      InboxDetail: "inbox/:inboxItemId",
      Wiki: "wiki",
      WikiDetail: "wiki/:pageId",
      Setting: "setting",
    },
  },
};
