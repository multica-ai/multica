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
      <ConnectorComposerMenu
        disabled={disabled}
        items={projectConnectorComposerItems(
          availableSkills ?? [],
          selectedConnectorKeys
        )}
        labels={{
          authorize: labels.addContentConnectorAuthorize,
          connect: labels.addContentConnectorConnect,
          connected: labels.addContentConnectorConnected,
          connectors: labels.addContentConnectors,
          empty: labels.addContentConnectorEmpty,
          loading: labels.addContentConnectorLoading,
          more: labels.addContentConnectorMore,
          selected: labels.addContentConnectorSelected
        }}
        loading={loading}
        onInstallConnector={
          connectorsReadOnly || !onCapabilitySettingsRequest
            ? undefined
            : async (connectorKey) => {
                await onCapabilitySettingsRequest({
                  kind: "connector",
                  connectorKey,
                  action: "install"
                });
                onRetryComposerOptions?.({
                  force: true,
                  section: "connectors"
                });
              }
        }
        onOpenChange={(open) => {
          if (open) {
            onRetryComposerOptions?.({ force: true, section: "connectors" });
          }
        }}
        onOpenConnector={
          connectorsReadOnly
            ? undefined
            : (connectorKey) =>
                onCapabilitySettingsRequest?.({
                  kind: "connector",
                  connectorKey,
                  action: "open"
                })
        }
        onOpenMarket={
          !showConnectorViewMore
            ? undefined
            : () =>
                openWorkspaceSettingsPanel({
                  section: "agent",
                  pane: "connectors"
                })
        }
        onRuntimeEnabledChange={
          connectorsReadOnly || !onCapabilitySettingsRequest
            ? undefined
            : (connectorKey, enabled) =>
                onCapabilitySettingsRequest({
                  kind: "connector",
                  connectorKey,
                  action: "set_runtime_enabled",
                  enabled
                })
        }
        onSelectConnector={connectorsReadOnly ? undefined : onConnectorSelected}
        readOnly={connectorsReadOnly}
      />
    );
  }
);
