import { TabBar as SharedTabBar } from "@multica/views/layout";
import { parseIssueWindowPath } from "../../../shared/issue-window";

export function TabBar() {
  return (
    <SharedTabBar
      issueWindow={{
        pathFromUrl: (url) => parseIssueWindowPath(url)?.path ?? null,
        open: (path, title) => {
          void window.desktopAPI.openIssueWindow({ path, title });
        },
      }}
    />
  );
}
