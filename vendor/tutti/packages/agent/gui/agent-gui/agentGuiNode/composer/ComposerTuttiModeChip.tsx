import { useId, useState } from "react";
import {
  Switch,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger
} from "@tutti-os/ui-system";
import { cn } from "../../../app/renderer/lib/utils";
import tuttiModeLinedIconUrl from "../../../app/renderer/assets/icons/tutti-mode-lined.svg";
import tuttiSnapStarsLightUrl from "../../../app/renderer/assets/animations/tutti-snap-stars-light.png";
import tuttiSnapStarsLightActiveUrl from "../../../app/renderer/assets/animations/tutti-snap-stars-light-active.png";
import tuttiSnapStarsDarkUrl from "../../../app/renderer/assets/animations/tutti-snap-stars-dark.png";
import tuttiSnapStarsDarkActiveUrl from "../../../app/renderer/assets/animations/tutti-snap-stars-dark-active.png";
import styles from "../AgentGUINode.styles";
import { getPortalContainer } from "@tutti-os/ui-system/portal-container";

/**
 * Compact Tutti Mode activation switch in the composer footer. It drives the
 * same activation path as /tutti and disappears under the same host gate.
 */
export function ComposerTuttiModeChip({
  active,
  updating,
  label,
  description,
  tuttiModeSupported,
  onTuttiModeChange
}: {
  active: boolean;
  updating: boolean;
  label: string;
  description?: string;
  tuttiModeSupported: boolean;
  onTuttiModeChange?: (active: boolean) => void;
}): React.JSX.Element | null {
  const switchId = useId();
  const [hovered, setHovered] = useState(false);
  const shouldPlaySnap = hovered && !updating;

  if (!onTuttiModeChange || !tuttiModeSupported) {
    return null;
  }

  return (
    <TooltipProvider delayDuration={120}>
      <Tooltip>
        <TooltipTrigger asChild>
          <label
            htmlFor={switchId}
            data-testid="agent-gui-composer-tutti-mode-toggle"
            data-agent-tutti-mode-active={active ? "true" : undefined}
            className={cn(
              styles.composerMenuTrigger,
              "group w-auto !gap-1.5",
              updating ? "cursor-wait" : "cursor-pointer"
            )}
            onMouseEnter={() => setHovered(true)}
            onMouseLeave={() => setHovered(false)}
          >
            <span
              aria-hidden
              className={styles.composerTuttiModeIcon}
              data-snap-active={shouldPlaySnap ? "true" : undefined}
            >
              <span
                className={styles.composerTuttiModeIconStatic}
                style={{
                  WebkitMaskImage: `url("${tuttiModeLinedIconUrl}")`,
                  WebkitMaskPosition: "center",
                  WebkitMaskRepeat: "no-repeat",
                  WebkitMaskSize: "contain",
                  maskImage: `url("${tuttiModeLinedIconUrl}")`,
                  maskPosition: "center",
                  maskRepeat: "no-repeat",
                  maskSize: "contain"
                }}
              />
              {shouldPlaySnap ? (
                <ComposerTuttiModeSnapAnimation active={active} />
              ) : null}
            </span>
            <span className="min-w-0 truncate">{label}</span>
            <Switch
              id={switchId}
              size="sm"
              checked={active}
              disabled={updating}
              aria-label={label}
              className="ml-0.5"
              data-testid="agent-gui-composer-tutti-mode-toggle-switch"
              onCheckedChange={(checked) => onTuttiModeChange(checked)}
            />
          </label>
        </TooltipTrigger>
        <TooltipContent side="top">{description ?? label}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}

const SNAP_STARS_URLS = {
  light: { idle: tuttiSnapStarsLightUrl, active: tuttiSnapStarsLightActiveUrl },
  dark: { idle: tuttiSnapStarsDarkUrl, active: tuttiSnapStarsDarkActiveUrl }
} as const;

function ComposerTuttiModeSnapAnimation({
  active
}: {
  active: boolean;
}): React.JSX.Element {
  const isDark =
    typeof document !== "undefined" &&
    getPortalContainer().getAttribute("data-theme") === "dark";
  const src =
    SNAP_STARS_URLS[isDark ? "dark" : "light"][active ? "active" : "idle"];
  const [isLoaded, setIsLoaded] = useState(false);

  return (
    <img
      alt=""
      aria-hidden="true"
      className={styles.composerTuttiModeIconAnimated}
      data-active={isLoaded ? "true" : undefined}
      decoding="async"
      draggable={false}
      src={src}
      key={src}
      onLoad={() => setIsLoaded(true)}
    />
  );
}
