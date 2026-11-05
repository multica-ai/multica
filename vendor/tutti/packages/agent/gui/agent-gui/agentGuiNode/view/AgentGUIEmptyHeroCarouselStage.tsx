import { Component, type ReactNode } from "react";
import type { AgentGUIAgentAvatarPresentation } from "../model/agentGuiAgentAvatarPresentation";
import type { AgentGUINodeViewProps } from "../AgentGUINodeView";
import { AgentGUIHeroAgentCarousel } from "../AgentGUIHeroAgentCarousel";
import styles from "../AgentGUINode.styles";
import { observer as observeClass } from "mobx-react";

interface AgentGUIEmptyHeroCarouselStageProps {
  activeAgentTargetId?: string | null;
  children: ReactNode;
  isActive: boolean;
  isVisible: boolean;
  items: readonly AgentGUIAgentAvatarPresentation[];
  onProviderSelect?: AgentGUINodeViewProps["actions"]["selectHomeComposerAgentTarget"];
  providerSelectLabel: string;
}

// Keep the carousel outside the ready/readiness-gate branch. Runtime
// readiness changes must not replace the WebGL canvas or reset its position.
export class AgentGUIEmptyHeroCarouselStage extends Component<AgentGUIEmptyHeroCarouselStageProps> {
  private alignmentActive = false;
  private animationFrame: number | null = null;
  private layer: HTMLDivElement | null = null;
  private mutationObserver: MutationObserver | null = null;
  private resizeObserver: ResizeObserver | null = null;
  private stage: HTMLDivElement | null = null;

  componentDidMount(): void {
    this.startAlignment();
  }

  componentDidUpdate(): void {
    if (!this.canAlign()) {
      this.stopAlignment();
      return;
    }
    if (!this.alignmentActive) {
      this.startAlignment();
      return;
    }
    this.scheduleAlignment();
  }

  componentWillUnmount(): void {
    this.stopAlignment();
  }

  render(): React.JSX.Element {
    const {
      activeAgentTargetId,
      children,
      isActive,
      isVisible,
      items,
      onProviderSelect,
      providerSelectLabel
    } = this.props;
    const hasCarousel = items.length > 1;

    return (
      <div ref={this.setStage} className={styles.emptyHeroCarouselStage}>
        {hasCarousel ? (
          <div ref={this.setLayer} className={styles.emptyHeroCarouselLayer}>
            <AgentGUIHeroAgentCarousel
              activeAgentTargetId={activeAgentTargetId}
              isActive={isActive}
              isVisible={isVisible}
              items={items}
              onProviderSelect={onProviderSelect}
              providerSelectLabel={providerSelectLabel}
            />
          </div>
        ) : null}
        {children}
      </div>
    );
  }

  // The floating layer must sit exactly on the body's placeholder slot. The
  // shared anchor frame keeps ready and gated content on one top baseline,
  // while measuring the slot preserves alignment across host padding and
  // ready/gated subtree changes. The CSS fallback covers the pre-measure paint.
  private startAlignment(): void {
    const stage = this.stage;
    if (!this.canAlign() || !stage) {
      this.stopAlignment();
      return;
    }

    this.alignmentActive = true;
    if (!this.resizeObserver && typeof ResizeObserver === "function") {
      this.resizeObserver = new ResizeObserver(this.scheduleAlignment);
    }
    if (!this.mutationObserver && typeof MutationObserver === "function") {
      this.mutationObserver = new MutationObserver(() => {
        this.observeLayoutRoots();
        this.scheduleAlignment();
      });
      this.mutationObserver.observe(stage, {
        childList: true,
        subtree: true
      });
    }
    this.observeLayoutRoots();
    this.syncAlignment();
  }

  private canAlign(): boolean {
    return Boolean(
      this.props.isVisible &&
        this.props.items.length > 1 &&
        this.stage &&
        this.layer
    );
  }

  private stopAlignment(): void {
    this.alignmentActive = false;
    if (this.animationFrame !== null) {
      cancelAnimationFrame(this.animationFrame);
      this.animationFrame = null;
    }
    this.resizeObserver?.disconnect();
    this.resizeObserver = null;
    this.mutationObserver?.disconnect();
    this.mutationObserver = null;
  }

  private observeLayoutRoots(): void {
    if (!this.stage) {
      return;
    }
    this.resizeObserver?.disconnect();
    this.resizeObserver?.observe(this.stage);
    const body = this.stage.querySelector(`.${styles.emptyHeroBody}`);
    if (body) {
      this.resizeObserver?.observe(body);
    }
  }

  private readonly scheduleAlignment = (): void => {
    if (this.animationFrame === null) {
      this.animationFrame = requestAnimationFrame(this.syncAlignment);
    }
  };

  private readonly syncAlignment = (): void => {
    this.animationFrame = null;
    if (!this.stage || !this.layer) {
      return;
    }
    const slot = this.stage.querySelector<HTMLElement>(
      `[data-carousel-placeholder], .${styles.emptyHeroCarouselPlaceholder}`
    );
    if (!slot) {
      this.layer.style.removeProperty("--agent-gui-hero-carousel-slot-top");
      this.layer.style.removeProperty("--agent-gui-hero-carousel-slot-left");
      return;
    }
    const stageRect = this.stage.getBoundingClientRect();
    const slotRect = slot.getBoundingClientRect();
    const top = slotRect.top - stageRect.top;
    const left = slotRect.left + slotRect.width / 2 - stageRect.left;
    this.layer.style.setProperty(
      "--agent-gui-hero-carousel-slot-top",
      `${top}px`
    );
    this.layer.style.setProperty(
      "--agent-gui-hero-carousel-slot-left",
      `${left}px`
    );
  };

  private readonly setLayer = (element: HTMLDivElement | null): void => {
    this.layer = element;
  };

  private readonly setStage = (element: HTMLDivElement | null): void => {
    this.stage = element;
  };
}
observeClass(AgentGUIEmptyHeroCarouselStage);
