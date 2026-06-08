"use client";

import { useEffect, useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { RuntimesPage, RuntimeDetailPage } from "@multica/views/runtimes";
import { DemoProductFrame } from "./demo-product-frame";
import { ValueDemoFrame } from "./value-demo-frame";

const DEMO_W = 980;
const DEMO_H = 740;
const PHASE_MS = 5200;

export function ValueBoardDemo() {
  const [detail, setDetail] = useState(false);
  const reduceMotion = useReducedMotion();

  useEffect(() => {
    const timer = window.setInterval(() => setDetail((v) => !v), PHASE_MS);
    return () => window.clearInterval(timer);
  }, []);

  return (
    <ValueDemoFrame width={DEMO_W} height={DEMO_H}>
      <DemoProductFrame
        activeTab="runtimes"
        pathname={detail ? "/demo/runtimes/rt-local-claude" : "/demo/runtimes"}
      >
        <AnimatePresence mode="wait" initial={false}>
          <motion.div
            key={detail ? "runtime-detail" : "runtime-list"}
            className="h-full"
            initial={reduceMotion ? false : { opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            exit={reduceMotion ? { opacity: 0 } : { opacity: 0, y: -8 }}
            transition={{ duration: reduceMotion ? 0 : 0.24, ease: [0.22, 1, 0.36, 1] }}
          >
            {detail ? (
              <RuntimeDetailPage runtimeId="rt-local-claude" />
            ) : (
              <RuntimesPage
                localDaemonId="daemon-local"
                localMachineName="Jiayuan MacBook Pro"
                hasLocalMachine
                cloudRuntimeEnabled
              />
            )}
          </motion.div>
        </AnimatePresence>
      </DemoProductFrame>
    </ValueDemoFrame>
  );
}
