"use client";

import { LandingHeader } from "./landing-header";
import { LandingHero } from "./landing-hero";
import { FeaturesSection } from "./features-section";
import { HowItWorksSection } from "./how-it-works-section";
import { OpenSourceSection } from "./open-source-section";
import { FAQSection } from "./faq-section";
import { LandingFooter } from "./landing-footer";
import { LandingFunnelTracker } from "./landing-funnel-tracker";

export function MulticaLanding() {
  return (
    <>
      <LandingFunnelTracker />
      <div className="relative">
        <LandingHeader />
        <LandingHero />
      </div>

      <FeaturesSection />
      <HowItWorksSection />
      <OpenSourceSection />
      <FAQSection />
      <LandingFooter />
    </>
  );
}
