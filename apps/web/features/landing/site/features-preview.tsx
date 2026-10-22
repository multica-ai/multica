"use client";

import { ChatVisual, SkillsVisual } from "./demo/feature-visuals";
import { AutopilotsFigure, RuntimesFigure, WorkflowFigure } from "./demo/figure-visuals";
import { FeatureSection } from "./home-page";
import { SitePage } from "./site-page";

const FIGURE_VISUALS = {
  workflows: WorkflowFigure,
  runtimes: RuntimesFigure,
  chat: ChatVisual,
  autopilots: AutopilotsFigure,
  skills: SkillsVisual,
};

/** The feature section with isometric figures, to compare against the home page. */
export function FeaturesPreview() {
  return (
    <SitePage>
      <div className="site-rails">
        <FeatureSection visuals={FIGURE_VISUALS} />
      </div>
    </SitePage>
  );
}
