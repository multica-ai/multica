import { createIssueViewStore } from "./view-store";

export const backlogViewStore = createIssueViewStore("multica_backlog_view");
backlogViewStore.setState({ viewMode: "list" });

export const todayViewStore = createIssueViewStore("multica_today_view");
todayViewStore.setState({ viewMode: "list" });

export const upcomingViewStore = createIssueViewStore("multica_upcoming_view");
upcomingViewStore.setState({ viewMode: "list" });

const projectBoardViewStores = new Map<string, ReturnType<typeof createIssueViewStore>>();

export function getProjectBoardViewStore(projectId: string) {
	const existingStore = projectBoardViewStores.get(projectId);
	if (existingStore) return existingStore;

	const nextStore = createIssueViewStore(`multica_project_${projectId}_board_view`);
	nextStore.setState({ viewMode: "board" });
	projectBoardViewStores.set(projectId, nextStore);
	return nextStore;
}