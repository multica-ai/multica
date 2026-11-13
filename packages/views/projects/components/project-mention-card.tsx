"use client";

import { useMemo, useState } from "react";
import { AppLink } from "../../navigation";
import { useRowMenu, yieldsToTextMenu } from "../../common/point-menu";
import {
  ContextMenuScopeSubmenu,
  findContextMenuScope,
  useScopeSubmenuFocus,
  type ResolvedContextMenuScope,
} from "../../common/context-menu-scope";
import { dropdownPrimitives } from "../../common/menu-primitives";
import { CollectionRowContext } from "../../layout/collection-list/row-context";
import { ProjectMenu } from "./project-row-actions";
import { useWorkspacePaths } from "@multica/core/paths";
import { ProjectChip, useChipProject } from "./project-chip";

interface ProjectMentionCardProps {
  projectId: string;
  /** Fallback text when the project is not resolvable (e.g. its title). */
  fallbackLabel?: string;
}

/**
 * Navigable chip — wraps ProjectChip in an AppLink pointing at the project's
 * detail page. Counterpart of IssueMentionCard, and deliberately shaped like
 * it: `ProjectChip` is presentational, so whatever wraps it decides that a
 * mention is a link, and that decision belongs in one place.
 *
 * It was in none until now — the wrapper lived inline in the readonly
 * renderer — and the cost showed up as gaps that landed on project mentions
 * alone: `.project-mention` never got the CSS rule cancelling generic link
 * chrome, and the link hover card never learned to skip it. A named component
 * is where the next such rule has somewhere obvious to go.
 *
 * Rendered as a real anchor: a mention must be reachable by Tab, activatable by
 * Enter, and expose a URL to copy or open in a new tab. A `<span onClick>`
 * looks identical and works for a mouse, which is exactly why that regression
 * is easy to miss (see project-mention-a11y.test.tsx).
 *
 * AppLink owns plain-click, modifier-click and the desktop new-tab adapter, so
 * none of that is reimplemented here. Unlike an issue there is no "open in new
 * tab" preference to honour — that setting is scoped to issue links.
 */
export function ProjectMentionCard({
  projectId,
  fallbackLabel,
}: ProjectMentionCardProps) {
  const p = useWorkspacePaths();
  const project = useChipProject(projectId);
  // Right-click opens the projects page's row menu at the cursor; it mounts
  // on the first right-click, so a page of mentions holds no menus.
  const rowMenu = useRowMenu();
  // The comment around the mention, read when the menu opens.
  const [outer, setOuter] = useState<ResolvedContextMenuScope["submenu"]>(null);
  const scope = useScopeSubmenuFocus(outer);
  const rowMenuContext = useMemo(
    () => ({ menu: rowMenu.menu, setMenu: rowMenu.setMenu }),
    [rowMenu.menu, rowMenu.setMenu],
  );
  return (
    <>
      <AppLink
        href={p.projectDetail(projectId)}
        newTabTitle={fallbackLabel}
        className="project-mention inline-flex"
        onContextMenu={
          project
            ? (event) => {
                // The innermost object's menu wins; a row around the mention
                // must not open its own as well. Selected text and an editor
                // keep their text menu.
                event.stopPropagation();
                if (yieldsToTextMenu(event.target as Element)) return;
                setOuter(findContextMenuScope(event.currentTarget.parentElement).submenu);
                rowMenu.openAtPointer(event);
              }
            : undefined
        }
      >
        <ProjectChip
          projectId={projectId}
          fallbackLabel={fallbackLabel}
          className="cursor-pointer hover:bg-accent transition-colors"
        />
      </AppLink>
      {project && rowMenu.menu.point && (
        <CollectionRowContext.Provider value={rowMenuContext}>
          <ProjectMenu
            project={project}
            triggerHidden
            trailing={<ContextMenuScopeSubmenu submenu={scope.submenu} primitives={dropdownPrimitives} />}
            finalFocus={scope.finalFocus}
          />
        </CollectionRowContext.Provider>
      )}
    </>
  );
}
