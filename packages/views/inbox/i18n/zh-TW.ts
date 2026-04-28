import type { InboxDict } from "./types";

export function createZhTwDict(): InboxDict {
  return {
    page: {
      title: "收件匣",
      backToInbox: "收件匣",
      emptyListTitle: "目前沒有通知",
      emptyDetailWithItems: "選擇一則通知以檢視詳細內容",
      emptyDetailNoItems: "收件匣是空的",
    },
    actions: {
      markAllRead: "全部標示為已讀",
      archiveAll: "全部封存",
      archiveAllRead: "封存所有已讀",
      archiveCompleted: "封存已完成項目",
      archive: "封存",
    },
    errors: {
      markRead: "標示為已讀失敗",
      archive: "封存失敗",
      markAllRead: "全部標示為已讀失敗",
      archiveAll: "全部封存失敗",
      archiveRead: "封存已讀項目失敗",
      archiveCompleted: "封存已完成項目失敗",
    },
    timeAgo: {
      justNow: "剛剛",
      minutes: (n) => `${n} 分鐘`,
      hours: (n) => `${n} 小時`,
      days: (n) => `${n} 天`,
    },
    types: {
      issue_assigned: "已指派",
      unassigned: "已取消指派",
      assignee_changed: "指派對象變更",
      status_changed: "狀態變更",
      priority_changed: "優先順序變更",
      due_date_changed: "到期日變更",
      new_comment: "新留言",
      mentioned: "提及你",
      review_requested: "請求審閱",
      task_completed: "任務完成",
      task_failed: "任務失敗",
      agent_blocked: "Agent 受阻",
      agent_completed: "Agent 已完成",
      reaction_added: "已回應",
    },
    detail: {
      setStatusTo: "狀態設為",
      setPriorityTo: "優先順序設為",
      assignedTo: (name) => `已指派給 ${name}`,
      removedAssignee: "已移除指派對象",
      setDueDateTo: (date) => `到期日設為 ${date}`,
      removedDueDate: "已移除到期日",
      reactedToComment: (emoji) => `對你的留言回應了 ${emoji}`,
    },
  };
}
