import * as Linking from "expo-linking";
import { MOBILE_ENV } from "../runtime/env";

export const linking = {
  prefixes: [Linking.createURL("/"), `${MOBILE_ENV.appScheme}://`],
  config: {
    screens: {
      Main: {
        screens: {
          Issues: "issues",
          Projects: "projects",
          Mine: "mine",
        },
      },
      IssueDetail: "issues/:issueId",
      ProjectDetail: "projects/:projectId",
      Search: "search",
    },
  },
};
