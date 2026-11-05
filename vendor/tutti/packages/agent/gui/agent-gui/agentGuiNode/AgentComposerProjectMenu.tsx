import { useMemo } from "react";
import {
  WorkspaceUserProjectSelect,
  type WorkspaceUserProjectSelectChangeAction,
  type WorkspaceUserProjectSelectLabelOverrides,
  type WorkspaceUserProjectSelectProps
} from "@tutti-os/workspace-user-project/ui";
import type {
  WorkspaceUserProject,
  WorkspaceUserProjectApi
} from "@tutti-os/workspace-user-project/contracts";
import type { WorkspaceUserProjectI18nRuntime } from "@tutti-os/workspace-user-project/i18n";
import { useOptionalAgentHostApi } from "../../agentActivityHost";
import { NewWorkspaceLinedIcon, cn } from "@tutti-os/ui-system";
import type { AgentGUIComposerSettingsVM } from "./model/agentGuiNodeTypes";
import styles from "./AgentGUINode.styles";
import { createAgentGUIUserProjectSelectionApi } from "./agentGuiUserProjectSelectionApi";
import { observer } from "mobx-react-lite";
export const AgentProjectDropdown = observer(AgentProjectDropdownRender);

export type AgentProjectDropdownLabels = Pick<
  WorkspaceUserProjectSelectLabelOverrides,
  "projectLocked"
> & {
  projectMissingDescription: string;
};

export interface AgentProjectPathChangeMetadata {
  action: WorkspaceUserProjectSelectChangeAction;
  project?: WorkspaceUserProject;
}

export type AgentProjectDropdownOptions = Pick<
  WorkspaceUserProjectSelectProps,
  "labels" | "menuActions" | "showKnownProjectOptions"
> & {
  /** Optional Host-owned import flow. Absent by default, so existing Hosts are unchanged. */
  importDirectory?: WorkspaceUserProjectApi["importDirectory"];
};

function AgentProjectDropdownRender({
  composerSettings,
  labels,
  options,
  i18n,
  selectProjectDirectory,
  userProjectApi,
  onDismissAutoFocus,
  onProjectMissingChange,
  onProjectPathChange
}: AgentProjectDropdownProps): React.JSX.Element {
  "use memo";
  const agentHostApi = useOptionalAgentHostApi();
  const projectSource =
    userProjectApi === undefined ? agentHostApi?.userProjects : userProjectApi;
  const resolvedUserProjectApi = useMemo(
    () =>
      createAgentGUIUserProjectSelectionApi({
        importDirectory: options?.importDirectory,
        selectProjectDirectory,
        userProjects: projectSource
      }),
    [options?.importDirectory, projectSource, selectProjectDirectory]
  );

  const classNamesProp1Value1 = cn(
    styles.composerMenuContent,
    "w-[240px] min-w-[240px] data-[side=top]:!translate-y-0"
  );
  const classNamesProp1Value2 = styles.composerMenuItem;
  const classNamesProp1Value3 = cn(
    "w-auto max-w-full",
    styles.composerMenuTrigger,
    styles.composerProjectTrigger,
    "text-[var(--agent-gui-text-tertiary)]",
    "disabled:cursor-not-allowed disabled:text-[var(--agent-gui-text-tertiary)] disabled:opacity-60 disabled:hover:text-[var(--agent-gui-text-tertiary)]"
  );
  const classNamesProp1 = useMemo<
    NonNullable<
      React.ComponentProps<typeof WorkspaceUserProjectSelect>["classNames"]
    >
  >(
    () => ({
      content: classNamesProp1Value1,
      item: classNamesProp1Value2,
      trigger: classNamesProp1Value3
    }),
    [classNamesProp1Value1, classNamesProp1Value2, classNamesProp1Value3]
  );
  const labelsProp2Value1 = options?.labels;
  const labelsProp2 = useMemo<
    NonNullable<
      React.ComponentProps<typeof WorkspaceUserProjectSelect>["labels"]
    >
  >(() => ({ ...labelsProp2Value1, ...labels }), [labelsProp2Value1, labels]);
  const renderAddProjectIconProp3 = useMemo<
    NonNullable<
      React.ComponentProps<
        typeof WorkspaceUserProjectSelect
      >["renderAddProjectIcon"]
    >
  >(
    () => () => (
      <NewWorkspaceLinedIcon
        aria-hidden
        data-workspace-user-project-add-icon="true"
        size={15}
      />
    ),
    []
  );
  return (
    <WorkspaceUserProjectSelect
      api={resolvedUserProjectApi}
      classNames={classNamesProp1}
      i18n={i18n}
      labels={labelsProp2}
      menuActions={options?.menuActions}
      projectLocked={Boolean(composerSettings.projectLocked)}
      renderAddProjectIcon={renderAddProjectIconProp3}
      selectedProjectPath={composerSettings.selectedProjectPath}
      service={
        userProjectApi === undefined
          ? (agentHostApi?.userProjects?.service ?? null)
          : null
      }
      shouldApplyPreparedSelection={
        composerSettings.shouldApplyPreparedProjectSelection === true
      }
      showKnownProjectOptions={options?.showKnownProjectOptions}
      onDismissAutoFocus={onDismissAutoFocus}
      onProjectMissingChange={onProjectMissingChange}
      onProjectPathChange={onProjectPathChange}
    />
  );
}

type AgentProjectDropdownProps = {
  composerSettings: Pick<
    AgentGUIComposerSettingsVM,
    | "selectedProjectPath"
    | "projectLocked"
    | "shouldApplyPreparedProjectSelection"
  >;
  i18n: WorkspaceUserProjectI18nRuntime;
  labels: AgentProjectDropdownLabels;
  options?: AgentProjectDropdownOptions;
  selectProjectDirectory?: () => Promise<{ path: string } | null>;
  userProjectApi?: WorkspaceUserProjectApi | null;
  onDismissAutoFocus?: (event: Event) => void;
  onProjectMissingChange?: (isMissing: boolean) => void;
  onProjectPathChange: (
    path: string | null,
    metadata?: AgentProjectPathChangeMetadata
  ) => void;
};
