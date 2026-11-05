import { observer } from "mobx-react-lite";
import type { ReactNode, RefObject } from "react";
import styles from "./AgentGUINode.styles";

/**
 * Stable layout boundary for the conversation composer area.
 *
 * Floating controls, lifted interaction prompts, session accessories, and the
 * primary composer each have an explicit slot. Domain workflows compose into
 * `accessories`; this primitive owns no workflow or lifecycle policy.
 */
export const AgentComposerRegion = observer(function AgentComposerRegion({
  accessories,
  floating,
  lifted,
  primary,
  regionRef
}: {
  accessories?: ReactNode;
  floating?: ReactNode;
  lifted?: ReactNode;
  primary: ReactNode;
  regionRef: RefObject<HTMLDivElement | null>;
}): React.JSX.Element {
  return (
    <div
      ref={regionRef}
      className={styles.bottomDock}
      data-testid="agent-gui-bottom-dock"
    >
      {floating}
      {lifted}
      {accessories}
      {primary}
    </div>
  );
});
