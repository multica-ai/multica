"use client";

// Strings for the cerebro reminder overview / card / create sheet.
// The app is English — these strings are the single source and are not
// translated. Strings live here (rather than inline in components) so copy is
// edited in one place.

export type ReminderStringTable = {
  // Sidebar + overview
  nav_reminders: string;
  overview_title: string;
  overview_empty_title: string;
  overview_empty_hint: string;
  overview_new: string;
  overview_select_prompt: string;
  // Card
  card_badge: string;
  card_to_prefix: string;
  card_about_label: string;
  card_source_gone: string;
  snooze_label: string;
  snooze_1h: string;
  snooze_3h: string;
  snooze_tomorrow_9: string;
  done_label: string;
  recipient_agent_word: string;
  recipient_member_word: string;
  toast_snoozed: string;
  toast_snooze_failed: string;
  toast_done: string;
  toast_done_failed: string;
  // Create sheet
  create_title: string;
  create_about_prefix: string;
  create_recipient_label: string;
  create_recipient_people: string;
  create_recipient_agents: string;
  create_recipient_you_suffix: string;
  create_recipient_agent_hint: string;
  create_recipient_member_hint: string;
  create_text_label: string;
  create_text_optional_suffix: string;
  create_text_placeholder_free: string;
  create_text_placeholder_anchored: string;
  create_time_label: string;
  create_preset_1h: string;
  create_preset_3h: string;
  create_preset_tomorrow_9: string;
  create_cancel: string;
  create_submit: string;
  toast_created: string;
  toast_create_failed: string;
  // Anchor + source labels
  anchor_free: string;
  anchor_untitled: string;
  go_to_message: string;
  go_to_issue: string;
  go_to_project: string;
  go_to_chat: string;
  prefix_issue: string;
  prefix_project: string;
  prefix_chat: string;
  source_dm: string;
  source_direct_message: string;
  source_channel: string;
  source_message: string;
  // Relative date words
  date_today: string;
  date_tomorrow: string;
  due_today_prefix: string;
  due_tomorrow_prefix: string;
  due_prefix: string;
  due_at_word: string;
};

const en: ReminderStringTable = {
  nav_reminders: "Reminders",
  overview_title: "My reminders",
  overview_empty_title: "You have no reminders.",
  overview_empty_hint: "Press “New”, or set one on a message, an issue or a project.",
  overview_new: "New",
  overview_select_prompt: "Select a reminder to see it.",
  card_badge: "Reminder",
  card_to_prefix: "To",
  card_about_label: "This reminder is about:",
  card_source_gone: "The source message is no longer available.",
  snooze_label: "Snooze",
  snooze_1h: "In 1 hour",
  snooze_3h: "In 3 hours",
  snooze_tomorrow_9: "Tomorrow at 9",
  done_label: "Done",
  recipient_agent_word: "agent",
  recipient_member_word: "recipient",
  toast_snoozed: "Reminder snoozed",
  toast_snooze_failed: "Couldn't snooze the reminder",
  toast_done: "Reminder marked done",
  toast_done_failed: "Couldn't mark the reminder done",
  create_title: "New reminder",
  create_about_prefix: "About:",
  create_recipient_label: "Recipient",
  create_recipient_people: "People",
  create_recipient_agents: "Agents",
  create_recipient_you_suffix: "(you)",
  create_recipient_agent_hint: "The agent runs automatically when it's due.",
  create_recipient_member_hint: "Lands in the recipient's inbox.",
  create_text_label: "Text",
  create_text_optional_suffix: " (optional)",
  create_text_placeholder_free: "What should you be reminded about?",
  create_text_placeholder_anchored: "Suggested from the source if empty",
  create_time_label: "Time",
  create_preset_1h: "In 1 hour",
  create_preset_3h: "In 3 hours",
  create_preset_tomorrow_9: "Tomorrow at 9",
  create_cancel: "Cancel",
  create_submit: "Create reminder",
  toast_created: "Reminder created",
  toast_create_failed: "Couldn't create the reminder",
  anchor_free: "Free reminder",
  anchor_untitled: "untitled",
  go_to_message: "Go to message",
  go_to_issue: "Go to issue",
  go_to_project: "Go to project",
  go_to_chat: "Go to chat",
  prefix_issue: "Issue",
  prefix_project: "Project",
  prefix_chat: "Chat",
  source_dm: "DM",
  source_direct_message: "Direct message",
  source_channel: "Channel",
  source_message: "Message",
  date_today: "Today",
  date_tomorrow: "Tomorrow",
  due_today_prefix: "Due today at",
  due_tomorrow_prefix: "Due tomorrow at",
  due_prefix: "Due",
  due_at_word: "at",
};

export function pickReminderStrings(_language?: string | undefined): ReminderStringTable {
  return en;
}

/** Returns the reminder string table (English — the app is English). */
export function useCerebroReminderStrings(): ReminderStringTable {
  return en;
}
