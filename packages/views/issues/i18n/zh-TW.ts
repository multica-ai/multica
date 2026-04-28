import type { IssuesDict } from "./types";

export function createZhTwDict(): IssuesDict {
  return {
    page: {
      breadcrumbFallback: "工作區",
      title: "Issues",
      emptyTitle: "目前沒有任何 Issue",
      emptyHint: "建立第一個 Issue 開始使用。",
      moveFailed: "Issue 搬移失敗",
    },
    scope: {
      all: { label: "全部", description: "此工作區的所有 Issue" },
      members: { label: "成員", description: "指派給團隊成員的 Issue" },
      agents: { label: "Agent", description: "指派給 AI Agent 的 Issue" },
    },
    toolbar: {
      filterPlaceholder: "篩選…",
      filterTooltip: "篩選",
      displayTooltip: "顯示設定",
      sortAscending: "由小到大",
      sortDescending: "由大到小",
      boardView: "看板檢視",
      listView: "列表檢視",
      viewLabel: "檢視",
      membersLabel: "成員",
      agentsLabel: "Agent",
      sortFallback: "手動",
    },
    detail: {
      description: "描述",
      activity: "活動紀錄",
      properties: "屬性",
      status: "狀態",
      priority: "優先順序",
      assignee: "指派對象",
      labels: "標籤",
      project: "專案",
      parent: "父 Issue",
      subIssues: "子 Issue",
      created: "建立時間",
      updated: "更新時間",
    },
  };
}
