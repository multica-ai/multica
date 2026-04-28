import type { MyIssuesDict } from "./types";

export function createZhTwDict(): MyIssuesDict {
  return {
    page: {
      breadcrumbFallback: "工作區",
      title: "我的 Issue",
      emptyTitle: "目前沒有指派給你的 Issue",
      emptyHint: "你建立或被指派的 Issue 會顯示在這裡。",
      moveFailed: "Issue 搬移失敗",
    },
    scopes: {
      assigned: { label: "指派給我", description: "指派給我的 Issue" },
      created: { label: "我建立的", description: "我建立的 Issue" },
      agents: { label: "我的 Agent", description: "指派給我的 Agent 的 Issue" },
    },
    toolbar: {
      filter: "篩選",
      displaySettings: "顯示設定",
      boardView: "看板檢視",
      listView: "列表檢視",
      viewLabel: "檢視",
      boardOption: "看板",
      listOption: "列表",
      sortAscending: "由小到大",
      sortDescending: "由大到小",
      sortFallback: "手動",
      ordering: "排序",
      cardProperties: "卡片屬性",
      statusLabel: "狀態",
      priorityLabel: "優先順序",
      issueCount: (count) => `${count} 個 Issue`,
      resetFilters: "重設所有篩選",
    },
  };
}
