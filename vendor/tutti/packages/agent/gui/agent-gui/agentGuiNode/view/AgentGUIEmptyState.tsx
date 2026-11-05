import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  cn
} from "@tutti-os/ui-system";
import { memo, useMemo } from "react";
import { Button } from "../../../app/renderer/components/ui/button";
import {
  MANAGED_AGENT_ICON_FALLBACK_URL,
  MANAGED_AGENT_ICON_URLS,
  MANAGED_AGENT_PROVIDER_RAIL_ICON_URLS
} from "../../../shared/managedAgentIcons";
import { normalizeManagedAgentProvider } from "../../../shared/managedAgentProviders";
import type {
  AgentGUIAgentTarget,
  AgentGUIProviderReadinessGate
} from "../../../types";
import { AgentComposer, type AgentComposerProps } from "../AgentComposer";
import { AgentGUIHeroAgentCarousel } from "../AgentGUIHeroAgentCarousel";
import styles from "../AgentGUINode.styles";
import type {
  AgentGUINodeViewProps,
  AgentGUIViewLabels
} from "../AgentGUINodeView";
import { AgentGUIOwnerAvatar } from "../AgentGUIOwnerAvatar";
import { AgentHomeSuggestions } from "../AgentHomeSuggestions";
import {
  AgentSessionChrome,
  type AgentSessionChromeSource
} from "../AgentSessionChrome";
import {
  createFallbackAgentGUIAgentAvatar,
  projectAgentGUIAgentTargetAvatar,
  type AgentGUIAgentAvatarPresentation
} from "../model/agentGuiAgentAvatarPresentation";
import type {
  AgentGUIShellViewModel,
  AgentHomeSuggestionAction,
  AgentHomeSuggestionCategory
} from "../model/agentGuiNodeTypes";
import {
  resolveAgentGUIProviderReadinessAction,
  resolveAgentGUIProviderReadinessContent
} from "../model/agentGuiProviderReadiness";
import { projectAgentGUIAgentTargetName } from "../model/agentGuiTargetName";
import { AgentGUIAgentTargetName } from "./AgentGUIAgentTargetName";
import { AgentGUIEmptyHeroCarouselStage } from "./AgentGUIEmptyHeroCarouselStage";
import type { ChromeLabels } from "./AgentGUINodeView.types";
import { AgentTargetSetupGate } from "./AgentTargetSetupGate.tsx";
import { observer } from "mobx-react-lite";
export const AgentGUIProviderIconVisual = observer(
  AgentGUIProviderIconVisualRender
);
const AgentGUIAgentAvatarVisual = observer(AgentGUIAgentAvatarVisualRender);
const EmptyHeroTitle = observer(EmptyHeroTitleRender);

export interface AgentGUIProviderIconPresentation {
  iconUrl: string;
  provider: string;
}

export function resolveAgentGUIHeroIconUrl(
  provider: string | undefined
): string {
  const normalizedProvider = normalizeManagedAgentProvider(provider);
  return (
    MANAGED_AGENT_ICON_URLS[normalizedProvider] ??
    MANAGED_AGENT_ICON_FALLBACK_URL
  );
}

export function agentGUIProviderRailIconPresentation(
  provider: string | undefined,
  iconUrl?: string | null
): AgentGUIProviderIconPresentation {
  const normalizedProvider = normalizeManagedAgentProvider(provider);
  const providerRailIconUrl =
    MANAGED_AGENT_PROVIDER_RAIL_ICON_URLS[normalizedProvider] ?? null;
  return {
    provider: normalizedProvider,
    iconUrl:
      iconUrl?.trim() ||
      providerRailIconUrl ||
      resolveAgentGUIHeroIconUrl(normalizedProvider)
  };
}

export function shouldEmphasizeEmptyHeroProvider(label: string): boolean {
  return label.trim().length > 0;
}

export const EMPTY_HOME_SUGGESTIONS: readonly AgentHomeSuggestionCategory[] =
  Object.freeze([]);

interface AgentGUIEmptyHomePaneProps {
  isActive: boolean;
  isVisible: boolean;
  provider: AgentGUIShellViewModel["data"]["provider"];
  providerReadinessGate: AgentGUIProviderReadinessGate | null;
  showAllProviders: boolean;
  agentTargets: readonly AgentGUIAgentTarget[];
  selectedAgentTarget: AgentGUIAgentTarget | null;
  onProviderSelect?: AgentGUINodeViewProps["actions"]["selectHomeComposerAgentTarget"];
  noticeSource: AgentSessionChromeSource;
  onSubmitApprovalOption: AgentGUINodeViewProps["actions"]["submitApprovalOption"];
  onAuthLogin?: (provider?: string | null) => void;
  onRetryActivation: AgentGUINodeViewProps["actions"]["retryActivation"];
  onContinueInNewConversation: AgentGUINodeViewProps["actions"]["continueInNewConversation"];
  chromeLabels: ChromeLabels;
  composerProps: AgentComposerProps | null;
  labels: AgentGUIViewLabels;
  suggestions: readonly AgentHomeSuggestionCategory[];
  suggestionsCloseLabel?: string;
  onSelectSuggestion: (prompt: string) => void;
  onSelectSuggestionAction?: (action: AgentHomeSuggestionAction) => void;
}

export const AgentGUIEmptyHomePane = observer(function AgentGUIEmptyHomePane({
  isActive,
  isVisible,
  provider,
  providerReadinessGate,
  showAllProviders,
  agentTargets,
  selectedAgentTarget,
  onProviderSelect,
  labels,
  ...heroProps
}: AgentGUIEmptyHomePaneProps): React.JSX.Element {
  "use memo";

  const presentedSelectedAgentTarget =
    selectedAgentTarget?.ref.kind === "loading" ? null : selectedAgentTarget;
  const runtimeProviderLabel =
    labels.emptyProviderForProvider?.(provider) ?? labels.emptyProvider ?? "";
  const providerLabel = presentedSelectedAgentTarget
    ? projectAgentGUIAgentTargetName({
        ownerSeparator: labels.sharedAgentOwnerSeparator,
        target: presentedSelectedAgentTarget
      }).fullLabel
    : runtimeProviderLabel;
  const baseLabel = labels.emptyForProvider?.(provider) ?? labels.empty;
  const emptyLabel = runtimeProviderLabel
    ? baseLabel.replace(runtimeProviderLabel, providerLabel)
    : baseLabel;
  const avatarPresentations = useMemo(
    () =>
      agentTargets.length
        ? agentTargets.map((target) => ({
            ...projectAgentGUIAgentTargetAvatar(target),
            iconUrl: agentGUIProviderRailIconPresentation(target.provider)
              .iconUrl
          }))
        : presentedSelectedAgentTarget
          ? [
              {
                ...projectAgentGUIAgentTargetAvatar(
                  presentedSelectedAgentTarget
                ),
                iconUrl: agentGUIProviderRailIconPresentation(
                  presentedSelectedAgentTarget.provider
                ).iconUrl
              }
            ]
          : [
              createFallbackAgentGUIAgentAvatar({
                provider,
                label: providerLabel
              })
            ],
    [agentTargets, presentedSelectedAgentTarget, provider, providerLabel]
  );
  const carouselMountedExternally = avatarPresentations.length > 1;

  return (
    <AgentGUIEmptyHeroCarouselStage
      activeAgentTargetId={
        presentedSelectedAgentTarget?.agentTargetId ??
        presentedSelectedAgentTarget?.targetId
      }
      isActive={isActive}
      isVisible={isVisible}
      items={avatarPresentations}
      onProviderSelect={onProviderSelect}
      providerSelectLabel={labels.providerSwitchLabel}
    >
      <AgentTargetSetupGate
        carouselMountedExternally={carouselMountedExternally}
      >
        {providerReadinessGate ? (
          <AgentGUIProviderReadinessGatePane
            isActive={isActive}
            isVisible={isVisible}
            provider={provider}
            gate={providerReadinessGate}
            showAllProviders={showAllProviders}
            emptyLabel={emptyLabel}
            agentTargets={agentTargets}
            avatarPresentations={avatarPresentations}
            carouselMountedExternally={carouselMountedExternally}
            onProviderSelect={onProviderSelect}
            providerLabel={providerLabel}
            providerSelectLabel={labels.providerSwitchLabel}
            selectedAgentTarget={presentedSelectedAgentTarget}
            labels={labels}
          />
        ) : (
          <AgentGUIEmptyHeroPane
            {...heroProps}
            isActive={isActive}
            isVisible={isVisible}
            provider={provider}
            emptyLabel={emptyLabel}
            emptyProvider={providerLabel}
            avatarPresentations={avatarPresentations}
            carouselMountedExternally={carouselMountedExternally}
            onProviderSelect={onProviderSelect}
            agentTargets={agentTargets}
            selectedAgentTarget={presentedSelectedAgentTarget}
            providerSelectLabel={labels.providerSwitchLabel}
            sharedAgentOwnerSeparator={labels.sharedAgentOwnerSeparator}
          />
        )}
      </AgentTargetSetupGate>
    </AgentGUIEmptyHeroCarouselStage>
  );
});

interface AgentGUIEmptyHeroPaneProps {
  isActive?: boolean;
  isVisible?: boolean;
  provider: AgentGUIShellViewModel["data"]["provider"];
  emptyLabel: string;
  emptyProvider: string;
  avatarPresentations: readonly AgentGUIAgentAvatarPresentation[];
  carouselMountedExternally?: boolean;
  noticeSource: AgentSessionChromeSource;
  onSubmitApprovalOption: AgentGUINodeViewProps["actions"]["submitApprovalOption"];
  onAuthLogin?: (provider?: string | null) => void;
  onRetryActivation: AgentGUINodeViewProps["actions"]["retryActivation"];
  onContinueInNewConversation: AgentGUINodeViewProps["actions"]["continueInNewConversation"];
  onProviderSelect?: AgentGUINodeViewProps["actions"]["selectHomeComposerAgentTarget"];
  agentTargets: readonly AgentGUIAgentTarget[];
  selectedAgentTarget: AgentGUIAgentTarget | null;
  chromeLabels: ChromeLabels;
  composerProps: AgentComposerProps | null;
  providerSelectLabel: string;
  sharedAgentOwnerSeparator: string;
  suggestions: readonly AgentHomeSuggestionCategory[];
  suggestionsCloseLabel?: string;
  onSelectSuggestion: (prompt: string) => void;
  onSelectSuggestionAction?: (action: AgentHomeSuggestionAction) => void;
}

export const AgentGUIEmptyHeroPane = observer(function AgentGUIEmptyHeroPane({
  isActive = true,
  isVisible = true,
  provider,
  emptyLabel,
  emptyProvider,
  avatarPresentations,
  carouselMountedExternally = false,
  noticeSource,
  onSubmitApprovalOption,
  onAuthLogin,
  onRetryActivation,
  onContinueInNewConversation,
  onProviderSelect,
  agentTargets,
  selectedAgentTarget,
  chromeLabels,
  composerProps,
  providerSelectLabel,
  sharedAgentOwnerSeparator,
  suggestions,
  suggestionsCloseLabel,
  onSelectSuggestion,
  onSelectSuggestionAction
}: AgentGUIEmptyHeroPaneProps): React.JSX.Element {
  "use memo";

  const heroAvatarPresentations = useMemo(
    () =>
      avatarPresentations.length > 0
        ? avatarPresentations
        : [
            createFallbackAgentGUIAgentAvatar({
              provider,
              label: emptyProvider
            })
          ],
    [avatarPresentations, provider, emptyProvider]
  );
  const heroIconAnimationKey = heroAvatarPresentations
    .map(
      (avatar) =>
        `${avatar.agentTargetId}:${avatar.iconUrl}:${avatar.badge?.iconUrl ?? ""}`
    )
    .join("|");

  return (
    <div
      className={cn(
        styles.emptyHero,
        !composerProps && "multica-empty-conversation-welcome"
      )}
    >
      <div className={styles.emptyHeroBody}>
        <div
          className={styles.emptyHeroIconSlot}
          data-carousel-placeholder={carouselMountedExternally || undefined}
        >
          {carouselMountedExternally ? null : heroAvatarPresentations.length >
            1 ? (
            <AgentGUIHeroAgentCarousel
              activeAgentTargetId={
                selectedAgentTarget?.agentTargetId ??
                selectedAgentTarget?.targetId
              }
              isActive={isActive}
              items={heroAvatarPresentations}
              isVisible={isVisible}
              onProviderSelect={onProviderSelect}
              providerSelectLabel={providerSelectLabel}
            />
          ) : (
            <AgentGUIAgentAvatarVisual
              key={heroIconAnimationKey}
              className={styles.emptyHeroIconEffect}
              presentation={heroAvatarPresentations[0]!}
            />
          )}
        </div>
        <h2 className={styles.emptyHeroTitle}>
          <EmptyHeroTitle
            label={emptyLabel}
            providerLabel={emptyProvider}
            providerSelectLabel={providerSelectLabel}
            agentTargets={agentTargets}
            selectedAgentTarget={selectedAgentTarget}
            onProviderSelect={onProviderSelect}
            sharedAgentOwnerSeparator={sharedAgentOwnerSeparator}
          />
        </h2>
        <AgentSessionChrome
          source={noticeSource}
          onSubmitApprovalOption={onSubmitApprovalOption}
          onAuthLogin={onAuthLogin}
          onRetryActivation={onRetryActivation}
          onContinueInNewConversation={onContinueInNewConversation}
          labels={chromeLabels}
        />
        {composerProps ? <AgentComposer {...composerProps} /> : null}
        <AgentHomeSuggestions
          categories={suggestions}
          onSelectSuggestion={onSelectSuggestion}
          onSelectAction={onSelectSuggestionAction}
          closeLabel={suggestionsCloseLabel}
        />
      </div>
    </div>
  );
});

interface AgentGUIProviderReadinessGatePaneProps {
  isActive?: boolean;
  isVisible?: boolean;
  provider: AgentGUIShellViewModel["data"]["provider"];
  gate: AgentGUIProviderReadinessGate;
  showAllProviders?: boolean;
  emptyLabel: string;
  agentTargets: readonly AgentGUIAgentTarget[];
  avatarPresentations: readonly AgentGUIAgentAvatarPresentation[];
  carouselMountedExternally?: boolean;
  onProviderSelect?: AgentGUINodeViewProps["actions"]["selectHomeComposerAgentTarget"];
  providerLabel: string;
  providerSelectLabel: string;
  selectedAgentTarget: AgentGUIAgentTarget | null;
  labels: Pick<
    AgentGUIViewLabels,
    | "providerGateCheckingTitle"
    | "providerGateCheckingDescription"
    | "providerGateCheckingAgentsDescription"
    | "providerGateInstallTitle"
    | "providerGateInstallDescription"
    | "providerGateInstallAction"
    | "providerGateLoginTitle"
    | "providerGateLoginDescription"
    | "providerGateLoginAction"
    | "providerGateModelPlanAction"
    | "providerGateComingSoonTitle"
    | "providerGateComingSoonDescription"
    | "providerGateComingSoonAction"
    | "providerGateUnavailableTitle"
    | "providerGateUnavailableDescription"
    | "providerGateRetryAction"
    | "providerGateRuntimeSelectionTitle"
    | "providerGateRuntimeSelectionDescription"
    | "providerGateRuntimeSelectionAction"
    | "providerGatePendingInstall"
    | "providerGatePendingLogin"
    | "providerGatePendingRefresh"
    | "sharedAgentOwnerSeparator"
  >;
}

export const AgentGUIProviderReadinessGatePane = observer(
  function AgentGUIProviderReadinessGatePane({
    isActive = true,
    isVisible = true,
    provider,
    gate,
    showAllProviders = false,
    emptyLabel,
    agentTargets,
    avatarPresentations,
    carouselMountedExternally = false,
    onProviderSelect,
    providerLabel,
    providerSelectLabel,
    selectedAgentTarget,
    labels
  }: AgentGUIProviderReadinessGatePaneProps): React.JSX.Element {
    "use memo";

    const heroAvatarPresentations = useMemo(
      () =>
        avatarPresentations.length > 0
          ? avatarPresentations
          : [
              selectedAgentTarget
                ? projectAgentGUIAgentTargetAvatar(selectedAgentTarget)
                : createFallbackAgentGUIAgentAvatar({
                    provider,
                    label: provider
                  })
            ],
      [avatarPresentations, selectedAgentTarget, provider]
    );
    const pendingAction = gate.pendingAction ?? null;
    const isPending = pendingAction !== null;
    const showAllProvidersChecking =
      showAllProviders && gate.status === "checking";
    const content = resolveAgentGUIProviderReadinessContent(
      gate.status,
      labels,
      {
        showAllProviders: showAllProvidersChecking
      }
    );
    const titleLabel =
      gate.status === "not_installed" || gate.status === "auth_required"
        ? emptyLabel
        : content.title;
    const action = resolveAgentGUIProviderReadinessAction(gate.status);
    const modelPlanActionAvailable =
      typeof gate.onModelPlanSetup === "function" &&
      (gate.status === "not_installed" || gate.status === "auth_required");
    const pendingLabel =
      pendingAction === "install"
        ? labels.providerGatePendingInstall
        : pendingAction === "login"
          ? labels.providerGatePendingLogin
          : pendingAction === "refresh"
            ? labels.providerGatePendingRefresh
            : null;

    return (
      <div className={styles.emptyHero}>
        <div
          className={cn(styles.emptyHeroBody, styles.emptyProviderGate)}
          data-testid="agent-gui-provider-readiness-gate"
          role="status"
        >
          {carouselMountedExternally ? (
            <div
              aria-hidden="true"
              className={styles.emptyHeroCarouselPlaceholder}
            />
          ) : heroAvatarPresentations.length > 1 ? (
            <AgentGUIHeroAgentCarousel
              activeAgentTargetId={
                selectedAgentTarget?.agentTargetId ??
                selectedAgentTarget?.targetId
              }
              isActive={isActive}
              items={heroAvatarPresentations}
              isVisible={isVisible}
              onProviderSelect={onProviderSelect}
              providerSelectLabel={providerSelectLabel}
            />
          ) : (
            <AgentGUIAgentAvatarVisual
              className={styles.emptyHeroIconEffect}
              presentation={heroAvatarPresentations[0]!}
            />
          )}
          <h2 className={styles.emptyHeroTitle}>
            <EmptyHeroTitle
              label={titleLabel}
              providerLabel={providerLabel}
              providerSelectLabel={providerSelectLabel}
              agentTargets={agentTargets}
              selectedAgentTarget={selectedAgentTarget}
              onProviderSelect={onProviderSelect}
              sharedAgentOwnerSeparator={labels.sharedAgentOwnerSeparator}
            />
          </h2>
          <p className={styles.emptyProviderGateDescription}>
            {content.description}
          </p>
          {pendingLabel && !action ? (
            <div
              className={styles.emptyProviderGateStatus}
              data-testid="agent-gui-provider-readiness-gate-pending"
            >
              {pendingLabel}
            </div>
          ) : null}
          {action || content.actionLabel || modelPlanActionAvailable ? (
            <div
              className={cn(
                "items-center justify-center gap-2",
                modelPlanActionAvailable
                  ? "grid w-full max-w-[320px] grid-cols-2"
                  : "flex flex-wrap"
              )}
            >
              {action ? (
                <ButtonBinding1
                  isPending={isPending}
                  gate={gate}
                  provider={provider}
                  action={action}
                  pendingLabel={pendingLabel}
                  content={content}
                />
              ) : content.actionLabel ? (
                <ButtonBinding2 content={content} />
              ) : null}
              {modelPlanActionAvailable ? (
                <ButtonBinding3 gate={gate} labels={labels} />
              ) : null}
            </div>
          ) : null}
        </div>
      </div>
    );
  }
);

function AgentGUIProviderIconVisualRender({
  ariaHidden = false,
  icon,
  imageClassName
}: {
  ariaHidden?: boolean;
  icon: AgentGUIProviderIconPresentation;
  imageClassName: string;
}): React.JSX.Element {
  return (
    <img
      alt=""
      aria-hidden={ariaHidden ? "true" : undefined}
      className={imageClassName}
      draggable={false}
      src={icon.iconUrl}
    />
  );
}

function AgentGUIAgentAvatarVisualRender({
  className,
  imageClassName,
  presentation
}: {
  className?: string;
  imageClassName?: string;
  presentation: AgentGUIAgentAvatarPresentation;
}): React.JSX.Element {
  return (
    <span
      aria-hidden="true"
      className={cn(styles.agentAvatar, className)}
      data-agent-target-id={presentation.agentTargetId}
    >
      <img
        alt=""
        className={cn(styles.agentAvatarImage, imageClassName)}
        draggable={false}
        src={presentation.iconUrl}
      />
      {presentation.badge?.iconUrl ? (
        <AgentGUIOwnerAvatar
          className={styles.agentAvatarBadge}
          iconUrl={presentation.badge.iconUrl}
          imageClassName={styles.agentAvatarBadgeImage}
          label={presentation.badge.label}
        />
      ) : null}
    </span>
  );
}

function EmptyHeroTitleRender({
  label,
  providerLabel,
  providerSelectLabel,
  agentTargets = [],
  selectedAgentTarget = null,
  onProviderSelect,
  sharedAgentOwnerSeparator
}: EmptyHeroTitleProps): React.JSX.Element {
  const providerStart = providerLabel ? label.indexOf(providerLabel) : -1;

  if (!shouldEmphasizeEmptyHeroProvider(label) || providerStart < 0) {
    return <>{label}</>;
  }

  const providerEnd = providerStart + providerLabel.length;
  const selectedAgentTargetId =
    selectedAgentTarget?.targetId ??
    `local:${selectedAgentTarget?.provider ?? ""}`;
  const canSwitchProvider =
    agentTargets.length > 1 && selectedAgentTarget && onProviderSelect;
  const providerName = label.slice(providerStart, providerEnd);

  return (
    <>
      {label.slice(0, providerStart)}
      {canSwitchProvider ? (
        <SelectBinding4
          selectedAgentTargetId={selectedAgentTargetId}
          agentTargets={agentTargets}
          onProviderSelect={onProviderSelect}
          providerSelectLabel={providerSelectLabel}
          sharedAgentOwnerSeparator={sharedAgentOwnerSeparator}
          selectedAgentTarget={selectedAgentTarget}
        />
      ) : selectedAgentTarget ? (
        <AgentGUIAgentTargetName
          className={cn(styles.emptyHeroProvider, "inline-flex max-w-[240px]")}
          ownerSeparator={sharedAgentOwnerSeparator}
          target={selectedAgentTarget}
        />
      ) : (
        <span className={styles.emptyHeroProvider}>{providerName}</span>
      )}
      {label.slice(providerEnd)}
    </>
  );
}

const ButtonBinding1 = observer(function ButtonBinding1({
  isPending,
  gate,
  provider,
  action,
  pendingLabel,
  content,
  ...slotProps
}: ButtonBinding1Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof ButtonBinding1Props>) {
  const onPointerDownProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onPointerDown"]>
  >((event) => event.stopPropagation());
  const onClickProp2 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => {
    if (isPending) {
      return;
    }
    gate.onAction?.(provider, action);
  });
  return (
    <Slot.Root {...slotProps}>
      <Button
        type="button"
        className={cn(
          styles.emptyProviderGateAction,
          "nodrag tsh-desktop-no-drag [-webkit-app-region:no-drag]"
        )}
        data-testid="agent-gui-provider-readiness-gate-action"
        disabled={isPending}
        onPointerDown={onPointerDownProp1}
        onClick={onClickProp2}
      >
        {isPending && pendingLabel ? pendingLabel : content.actionLabel}
      </Button>
    </Slot.Root>
  );
});

const ButtonBinding2 = observer(function ButtonBinding2({
  content,
  ...slotProps
}: {
  content: { title: string; description: string; actionLabel?: string };
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    content: { title: string; description: string; actionLabel?: string };
  }
>) {
  const onPointerDownProp3 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onPointerDown"]>
  >((event) => event.stopPropagation());
  return (
    <Slot.Root {...slotProps}>
      <Button
        type="button"
        className={cn(
          styles.emptyProviderGateAction,
          "nodrag tsh-desktop-no-drag [-webkit-app-region:no-drag]"
        )}
        data-testid="agent-gui-provider-readiness-gate-action"
        disabled
        onPointerDown={onPointerDownProp3}
      >
        {content.actionLabel}
      </Button>
    </Slot.Root>
  );
});

const ButtonBinding3 = observer(function ButtonBinding3({
  gate,
  labels,
  ...slotProps
}: {
  gate: NonNullable<AgentGUIProviderReadinessGatePaneProps>["gate"];
  labels: NonNullable<AgentGUIProviderReadinessGatePaneProps>["labels"];
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    gate: NonNullable<AgentGUIProviderReadinessGatePaneProps>["gate"];
    labels: NonNullable<AgentGUIProviderReadinessGatePaneProps>["labels"];
  }
>) {
  const onPointerDownProp4 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onPointerDown"]>
  >((event) => event.stopPropagation());
  const onClickProp5 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => gate.onModelPlanSetup?.());
  return (
    <Slot.Root {...slotProps}>
      <Button
        type="button"
        variant="secondary"
        className={cn(
          styles.emptyProviderGateAction,
          "nodrag tsh-desktop-no-drag [-webkit-app-region:no-drag]"
        )}
        data-testid="agent-gui-provider-readiness-model-plan-action"
        onPointerDown={onPointerDownProp4}
        onClick={onClickProp5}
      >
        {labels.providerGateModelPlanAction}
      </Button>
    </Slot.Root>
  );
});

const SelectBinding4 = observer(function SelectBinding4({
  selectedAgentTargetId,
  agentTargets,
  onProviderSelect,
  providerSelectLabel,
  sharedAgentOwnerSeparator,
  selectedAgentTarget
}: {
  selectedAgentTargetId: string;
  agentTargets: readonly AgentGUIAgentTarget[];
  onProviderSelect: (input: {
    provider: import("../../../types").AgentGUIProvider;
    agentTargetId?: string | null;
  }) => void;
  providerSelectLabel: NonNullable<EmptyHeroTitleProps>["providerSelectLabel"];
  sharedAgentOwnerSeparator: NonNullable<EmptyHeroTitleProps>["sharedAgentOwnerSeparator"];
  selectedAgentTarget: AgentGUIAgentTarget;
}) {
  const onValueChangeProp6 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Select>["onValueChange"]>
  >((nextTargetId) => {
    const target = agentTargets.find(
      (candidate) => candidate.targetId === nextTargetId
    );
    if (!target) {
      return;
    }
    onProviderSelect({
      provider: target.provider,
      agentTargetId: target.targetId
    });
  });
  return (
    <Select value={selectedAgentTargetId} onValueChange={onValueChangeProp6}>
      <SelectTrigger
        size="sm"
        aria-label={providerSelectLabel}
        title={providerSelectLabel}
        className={styles.emptyHeroProviderSelect}
      >
        <AgentGUIAgentTargetName
          className={cn(styles.emptyHeroProvider, "max-w-[240px]")}
          ownerSeparator={sharedAgentOwnerSeparator}
          target={selectedAgentTarget}
        />
      </SelectTrigger>
      <SelectContent
        align="center"
        className={cn(
          styles.composerMenuContent,
          "min-w-[190px] max-w-[min(420px,calc(100vw-32px))]"
        )}
      >
        {agentTargets.map((target) => (
          <SelectItem
            key={`${target.provider}:${target.targetId}`}
            value={target.targetId}
            className={cn(styles.composerMenuItem, "gap-2")}
          >
            <span className="flex min-w-0 items-center gap-1.5">
              <img
                alt=""
                aria-hidden="true"
                className="size-4 shrink-0 rounded-[4px]"
                src={
                  agentGUIProviderRailIconPresentation(
                    target.provider,
                    target.iconUrl
                  ).iconUrl
                }
              />
              <AgentGUIAgentTargetName
                ownerSeparator={sharedAgentOwnerSeparator}
                target={target}
              />
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
});

type EmptyHeroTitleProps = {
  label: string;
  providerLabel: string;
  providerSelectLabel: string;
  agentTargets?: readonly AgentGUIAgentTarget[];
  selectedAgentTarget?: AgentGUIAgentTarget | null;
  onProviderSelect?: AgentGUINodeViewProps["actions"]["selectHomeComposerAgentTarget"];
  sharedAgentOwnerSeparator: string;
};

type ButtonBinding1Props = {
  isPending: boolean;
  gate: NonNullable<AgentGUIProviderReadinessGatePaneProps>["gate"];
  provider: NonNullable<AgentGUIProviderReadinessGatePaneProps>["provider"];
  action: import("../../../types").AgentGUIProviderReadinessGateAction;
  pendingLabel: string | null;
  content: { title: string; description: string; actionLabel?: string };
};

type SelectBinding4Props = {
  selectedAgentTargetId: string;
  agentTargets: readonly AgentGUIAgentTarget[];
  onProviderSelect: (input: {
    provider: import("../../../types").AgentGUIProvider;
    agentTargetId?: string | null;
  }) => void;
  providerSelectLabel: string;
  sharedAgentOwnerSeparator: string;
  selectedAgentTarget: AgentGUIAgentTarget;
};
