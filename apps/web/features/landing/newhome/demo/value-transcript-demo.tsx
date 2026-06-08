"use client";

import { useEffect, useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { SkillsPage } from "@multica/views/skills";
import { DemoIssueDetail } from "./demo-issue-detail";
import { DemoProductFrame, type DemoProductTab } from "./demo-product-frame";
import { ValueDemoFrame } from "./value-demo-frame";

const DEMO_H = 624;
const PHASE_MS = 5600;

export function ValueTranscriptDemo() {
  const [showSkills, setShowSkills] = useState(false);
  const reduceMotion = useReducedMotion();
  const activeTab: DemoProductTab = showSkills ? "skills" : "issues";

  useEffect(() => {
    const timer = window.setInterval(() => setShowSkills((v) => !v), PHASE_MS);
    return () => window.clearInterval(timer);
  }, []);

  return (
    <ValueDemoFrame height={DEMO_H}>
      <DemoProductFrame
        activeTab={activeTab}
        pathname={showSkills ? "/demo/skills" : "/demo/issues/issue-129"}
      >
        <AnimatePresence mode="wait" initial={false}>
          <motion.div
            key={showSkills ? "skills" : "record"}
            className="h-full"
            initial={reduceMotion ? false : { opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            exit={reduceMotion ? { opacity: 0 } : { opacity: 0, y: -8 }}
            transition={{ duration: reduceMotion ? 0 : 0.24, ease: [0.22, 1, 0.36, 1] }}
          >
            {showSkills ? (
              <SkillsPage />
            ) : (
              <DemoIssueDetail issueId="issue-129" initialScrollTop={640} />
            )}
          </motion.div>
        </AnimatePresence>
      </DemoProductFrame>
    </ValueDemoFrame>
  );
}
