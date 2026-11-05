import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { useMemo } from "react";
import { ConnectorComposerMenu } from "@tutti-os/connector-renderer/ui";
import { observer } from "mobx-react-lite";
import { openWorkspaceSettingsPanel } from "../../../shared/workspaceSettingsPanel/workspaceSettingsPanelStore";
import { projectConnectorComposerItems } from "../integrations/connector/model/connectorPresentation";
import type { AgentComposerRenderProps as AgentComposerProps } from "./AgentComposer.types";

interface Props {
  availableSkills: AgentComposerProps["availableSkills"];
  connectorsVisible: boolean;
  connectorsReadOnly?: boolean;
  showConnectorViewMore?: boolean;
  disabled: boolean;
  labels: AgentComposerProps["labels"];
  loading: boolean;
  onRetryComposerOptions?: AgentComposerProps["onRetryComposerOptions"];
  onCapabilitySettingsRequest: AgentComposerProps["onCapabilitySettingsRequest"];
  onConnectorSelected: (connectorKey: string, selected: boolean) => void;
  selectedConnectorKeys: readonly string[];
}

/**
 * Owns the host-gated connector control between the mention and handoff
 * controls.
 */
export const ComposerPrimaryCapabilityControl = observer(
  function ComposerPrimaryCapabilityControl({
    availableSkills,
    connectorsVisible,
    connectorsReadOnly = false,
    disabled,
    labels,
    loading,
    onRetryComposerOptions,
    onCapabilitySettingsRequest,
    onConnectorSelected,
    selectedConnectorKeys,
    showConnectorViewMore = true
  }: Props): React.JSX.Element | null {
    if (!connectorsVisible) {
      return null;
    }

    return (
      <ConnectorComposerMenuBinding1
        disabled={disabled}
        availableSkills={availableSkills}
        selectedConnectorKeys={selectedConnectorKeys}
        labels={labels}
        loading={loading}
        connectorsReadOnly={connectorsReadOnly}
        onCapabilitySettingsRequest={onCapabilitySettingsRequest}
        onRetryComposerOptions={onRetryComposerOptions}
        showConnectorViewMore={showConnectorViewMore}
        onConnectorSelected={onConnectorSelected}
      />
    );
  }
);

const ConnectorComposerMenuBinding1 = observer(
  function ConnectorComposerMenuBinding1({
    disabled,
    availableSkills,
    selectedConnectorKeys,
    labels,
    loading,
    connectorsReadOnly,
    onCapabilitySettingsRequest,
    onRetryComposerOptions,
    showConnectorViewMore,
    onConnectorSelected
  }: ConnectorComposerMenuBinding1Props) {
    const connectorItems = useMemo(
      () =>
        projectConnectorComposerItems(
          availableSkills ?? [],
          selectedConnectorKeys
        ),
      [availableSkills, selectedConnectorKeys]
    );
    const labelsProp1Value1 = labels.addContentConnectorAuthorize;
    const labelsProp1Value2 = labels.addContentConnectorConnect;
    const labelsProp1Value3 = labels.addContentConnectorConnected;
    const labelsProp1Value4 = labels.addContentConnectors;
    const labelsProp1Value5 = labels.addContentConnectorEmpty;
    const labelsProp1Value6 = labels.addContentConnectorLoading;
    const labelsProp1Value7 = labels.addContentConnectorMore;
    const labelsProp1Value8 = labels.addContentConnectorSelected;
    const labelsProp1 = useMemo<
      NonNullable<React.ComponentProps<typeof ConnectorComposerMenu>["labels"]>
    >(
      () => ({
        authorize: labelsProp1Value1,
        connect: labelsProp1Value2,
        connected: labelsProp1Value3,
        connectors: labelsProp1Value4,
        empty: labelsProp1Value5,
        loading: labelsProp1Value6,
        more: labelsProp1Value7,
        selected: labelsProp1Value8
      }),
      [
        labelsProp1Value1,
        labelsProp1Value2,
        labelsProp1Value3,
        labelsProp1Value4,
        labelsProp1Value5,
        labelsProp1Value6,
        labelsProp1Value7,
        labelsProp1Value8
      ]
    );
    const onOpenChangeProp2 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof ConnectorComposerMenu>["onOpenChange"]
      >
    >((open) => {
      if (open) {
        onRetryComposerOptions?.({ force: true, section: "connectors" });
      }
    });
    const onInstallConnectorProp1 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof ConnectorComposerMenu>["onInstallConnector"]
      >
    >(async (connectorKey) => {
      await onCapabilitySettingsRequest!({
        kind: "connector",
        connectorKey,
        action: "install"
      });
      onRetryComposerOptions?.({
        force: true,
        section: "connectors"
      });
    });
    const onOpenConnectorProp2 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof ConnectorComposerMenu>["onOpenConnector"]
      >
    >((connectorKey) =>
      onCapabilitySettingsRequest?.({
        kind: "connector",
        connectorKey,
        action: "open"
      })
    );
    const onOpenMarketProp3 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof ConnectorComposerMenu>["onOpenMarket"]
      >
    >(() =>
      openWorkspaceSettingsPanel({
        section: "agent",
        pane: "connectors"
      })
    );
    const onRuntimeEnabledChangeProp4 = usePersistCallback<
      NonNullable<
        React.ComponentProps<
          typeof ConnectorComposerMenu
        >["onRuntimeEnabledChange"]
      >
    >((connectorKey, enabled) =>
      onCapabilitySettingsRequest!({
        kind: "connector",
        connectorKey,
        action: "set_runtime_enabled",
        enabled
      })
    );
    return (
      <ConnectorComposerMenu
        disabled={disabled}
        items={connectorItems}
        labels={labelsProp1}
        loading={loading}
        onInstallConnector={
          connectorsReadOnly || !onCapabilitySettingsRequest
            ? undefined
            : onInstallConnectorProp1
        }
        onOpenChange={onOpenChangeProp2}
        onOpenConnector={connectorsReadOnly ? undefined : onOpenConnectorProp2}
        onOpenMarket={!showConnectorViewMore ? undefined : onOpenMarketProp3}
        onRuntimeEnabledChange={
          connectorsReadOnly || !onCapabilitySettingsRequest
            ? undefined
            : onRuntimeEnabledChangeProp4
        }
        onSelectConnector={connectorsReadOnly ? undefined : onConnectorSelected}
        readOnly={connectorsReadOnly}
      />
    );
  }
);

type ConnectorComposerMenuBinding1Props = {
  disabled: NonNullable<Props>["disabled"];
  availableSkills: NonNullable<Props>["availableSkills"];
  selectedConnectorKeys: NonNullable<Props>["selectedConnectorKeys"];
  labels: NonNullable<Props>["labels"];
  loading: NonNullable<Props>["loading"];
  connectorsReadOnly: boolean;
  onCapabilitySettingsRequest: NonNullable<Props>["onCapabilitySettingsRequest"];
  onRetryComposerOptions: NonNullable<Props>["onRetryComposerOptions"];
  showConnectorViewMore: boolean;
  onConnectorSelected: NonNullable<Props>["onConnectorSelected"];
};
