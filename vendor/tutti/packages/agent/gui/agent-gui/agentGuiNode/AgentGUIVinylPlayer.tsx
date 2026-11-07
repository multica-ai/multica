import { Slot } from "radix-ui";
import { useMemo } from "react";
import { motion } from "framer-motion";
import type { AgentGUIAgentAvatarPresentation } from "./model/agentGuiAgentAvatarPresentation";
import { isBetaAgentProvider } from "../../shared/managedAgentProviders";
import chassisAssetUrl from "../../app/renderer/assets/icons/agent-vinyl-player-chassis.png";
import tonearmAssetUrl from "../../app/renderer/assets/icons/agent-vinyl-tonearm.png";
import betaTagAssetUrl from "../../app/renderer/assets/icons/agent-vinyl-beta-tag.svg";
import { AgentGUIOwnerAvatar } from "./AgentGUIOwnerAvatar";
import { observer } from "mobx-react-lite";
export const AgentGUIVinylPlayer = observer(AgentGUIVinylPlayerRender);

interface AgentGUIVinylPlayerProps {
  selectedAgent: AgentGUIAgentAvatarPresentation | null;
  isPlaying: boolean;
}

/**
 * Directly ported from the retro-vinyl-player reference component and scaled
 * by the stylesheet for the compact AgentGUI hero slot.
 */
function AgentGUIVinylPlayerRender({
  selectedAgent,
  isPlaying
}: AgentGUIVinylPlayerProps): React.JSX.Element {
  const selectedAgentCover = selectedAgent?.iconUrl ?? null;
  const showBetaBadge = isBetaAgentProvider(selectedAgent?.provider);

  const animateProp1Value1 = isPlaying ? 25 : -25;
  const animateProp1 = useMemo<
    NonNullable<React.ComponentProps<typeof motion.span>["animate"]>
  >(() => ({ rotate: animateProp1Value1 }), [animateProp1Value1]);

  const transitionProp2 = useMemo<
    NonNullable<React.ComponentProps<typeof motion.span>["transition"]>
  >(() => ({ type: "spring", stiffness: 200, damping: 15 }), []);
  return (
    <div className="agent-gui-vinyl-player" aria-hidden="true">
      {showBetaBadge ? (
        <img
          draggable={false}
          className="agent-gui-vinyl-player__beta"
          src={betaTagAssetUrl}
          alt=""
        />
      ) : null}
      <img
        draggable={false}
        className="agent-gui-vinyl-player__chassis"
        src={chassisAssetUrl}
        alt=""
      />
      <div className="agent-gui-vinyl-player__indicator">
        <div
          className={`agent-gui-vinyl-player__indicator-light${
            isPlaying ? " agent-gui-vinyl-player__indicator-light--playing" : ""
          }`}
        >
          <span />
          <span />
          <span />
        </div>
      </div>
      <div className="agent-gui-vinyl-player__platter">
        <div className="agent-gui-vinyl-player__platter-inner">
          <div
            className={`agent-gui-vinyl-player__record${
              isPlaying ? " agent-gui-vinyl-player__record--playing" : ""
            }`}
          >
            {Array.from({ length: 18 }, (_, index) => (
              <SpanBinding1 key={index} index={index} />
            ))}
            <span className="agent-gui-vinyl-player__reflection" />
            <span className="agent-gui-vinyl-player__label">
              {selectedAgentCover ? (
                <img draggable={false} src={selectedAgentCover} alt="" />
              ) : null}
            </span>
          </div>
        </div>
      </div>
      <img
        draggable={false}
        className="agent-gui-vinyl-player__tonearm"
        src={tonearmAssetUrl}
        alt=""
      />
      <div className="agent-gui-vinyl-player__power-control">
        <motion.span
          className="agent-gui-vinyl-player__lever"
          animate={animateProp1}
          transition={transitionProp2}
        />
      </div>
      <div className="agent-gui-vinyl-player__tone-control">
        <span />
        <span>◦</span>
      </div>
      {selectedAgent?.badge?.iconUrl ? (
        <AgentGUIOwnerAvatar
          className="agent-gui-vinyl-player__owner-badge"
          iconUrl={selectedAgent.badge.iconUrl}
          label={selectedAgent.badge.label}
        />
      ) : null}
    </div>
  );
}

const SpanBinding1 = observer(function SpanBinding1({
  index,
  ...slotProps
}: {
  index: number;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    index: number;
  }
>) {
  const styleProp3Value1 = `${10 + index * 4.5}px`;
  const styleProp3 = useMemo<
    NonNullable<React.ComponentProps<"span">["style"]>
  >(() => ({ inset: styleProp3Value1 }), [styleProp3Value1]);
  return (
    <Slot.Root {...slotProps}>
      <span
        key={index}
        className="agent-gui-vinyl-player__groove"
        style={styleProp3}
      />
    </Slot.Root>
  );
});
