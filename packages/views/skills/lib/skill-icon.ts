import type { LucideIcon } from "lucide-react";
import { WORKSPACE_PAGES } from "@multica/core/paths";
import { ROUTE_ICON_COMPONENTS } from "../../layout/route-icon-components";

/**
 * The icon that means "a skill", anywhere one is rendered as an entity: list
 * header, empty states, detail identity, agent skill rows, skill pickers.
 *
 * Derived from the skills route icon rather than re-declared, so the sidebar,
 * the desktop tab bar, and every in-page surface can never drift apart again
 * — they previously showed four different icons (BookOpenText, BookOpen,
 * FileText, Sparkles) for the same thing.
 *
 * This is for the skill *entity*. A file inside a skill is not a skill; those
 * keep the file-type icons in `file-tree.tsx`.
 */
export const SkillIcon: LucideIcon =
  ROUTE_ICON_COMPONENTS[WORKSPACE_PAGES.skills.icon];
