import type { NavigationContainerRef } from "@react-navigation/native";
import type { RootStackParamList } from "./root-navigator";

export function createMobileNavigationAdapter(
  navigationRef: React.RefObject<NavigationContainerRef<RootStackParamList> | null>,
) {
  return {
    push(path: string) {
      if (!navigationRef.current) return;
      if (path.includes("/issues/")) {
        navigationRef.current.navigate("IssueDetail", {
          issueId: path.split("/").at(-1) ?? "",
        });
        return;
      }
      navigationRef.current.navigate("Main");
    },
    replace(path: string) {
      this.push(path);
    },
    goBack() {
      navigationRef.current?.goBack();
    },
    pathname: "/",
    searchParams: new URLSearchParams(),
    getShareableUrl: (path: string) => path,
    openInNewTab: (_path: string) => {},
  };
}
