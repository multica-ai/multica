import { createElement } from "react";
import { NotebookPen } from "lucide-react";
import { NotesSettingsTab } from "./notes-settings-tab";

// Mirrors @multica/views ExtraSettingsTab. Defined locally to avoid a
// dependency cycle through views (same reasoning as cerebro-feature-flags).
interface ExtraSettingsTab {
  value: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  content: React.ReactNode;
}

/**
 * The "Notes" settings tab — the place to turn the Notes feature and its parts
 * on or off (TECH-3637). Always present so Notes can be re-enabled after being
 * switched off. Spliced into `<SettingsPage extraAccountTabs={...}>`.
 */
export const cerebroNotesSettingsTabs: ExtraSettingsTab[] = [
  {
    value: "cerebro-notes",
    label: "Notes",
    icon: NotebookPen,
    content: createElement(NotesSettingsTab),
  },
];
