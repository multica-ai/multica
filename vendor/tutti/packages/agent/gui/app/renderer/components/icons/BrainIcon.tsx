"use client";

import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { useMemo } from "react";

import type { HTMLAttributes, MouseEvent } from "react";
import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useRef
} from "react";
import {
  motion,
  useAnimation,
  useReducedMotion,
  type Transition,
  type Variants
} from "framer-motion";
import { cn } from "../../lib/utils";
import { observer } from "mobx-react-lite";

export interface BrainIconHandle {
  startAnimation: () => void;
  stopAnimation: () => void;
}

interface BrainIconProps extends HTMLAttributes<HTMLDivElement> {
  size?: number;
  active?: boolean;
}

const brainTransition: Transition = {
  duration: 1.4,
  repeat: Number.POSITIVE_INFINITY,
  repeatType: "mirror" as const,
  ease: "easeInOut"
};

const BRAIN_STEM_VARIANTS: Variants = {
  normal: { pathLength: 1, pathOffset: 0 },
  animate: {
    pathLength: [1, 0.4, 1],
    pathOffset: [0, 0.25, 0],
    transition: brainTransition
  }
};

const BRAIN_SIDE_VARIANTS: Variants = {
  normal: { pathLength: 1, pathOffset: 0 },
  animate: {
    pathLength: [1, 0.5, 1],
    pathOffset: [0, 0.25, 0],
    transition: brainTransition
  }
};

const BRAIN_TOP_ARC_VARIANTS: Variants = {
  normal: { pathLength: 1, pathOffset: 0 },
  animate: {
    pathLength: [1, 0.8, 1],
    pathOffset: [0, 0.07, 0],
    transition: brainTransition
  }
};

const BRAIN_LOWER_ARC_VARIANTS: Variants = {
  normal: { pathLength: 1, pathOffset: 0 },
  animate: {
    pathLength: [1, 0.8, 1],
    pathOffset: [0, 0.14, 0],
    transition: brainTransition
  }
};

const BrainIcon = observer(
  forwardRef<BrainIconHandle, BrainIconProps>(
    (
      {
        active = false,
        onMouseEnter,
        onMouseLeave,
        className,
        size = 28,
        ...props
      },
      ref
    ) => {
      const controls = useAnimation();
      const reduceMotion = useReducedMotion();
      const isControlledRef = useRef(false);

      const startAnimation = useCallback(() => {
        if (reduceMotion) {
          return;
        }
        void controls.start("animate");
      }, [controls, reduceMotion]);

      const stopAnimation = useCallback(() => {
        void controls.start("normal");
      }, [controls]);

      useImperativeHandle(ref, () => {
        isControlledRef.current = true;

        return {
          startAnimation,
          stopAnimation
        };
      });

      useEffect(() => {
        if (active) {
          startAnimation();
          return;
        }
        stopAnimation();
      }, [active, startAnimation, stopAnimation]);

      const handleMouseEnter = useCallback(
        (event: MouseEvent<HTMLDivElement>) => {
          if (isControlledRef.current) {
            onMouseEnter?.(event);
          } else {
            startAnimation();
          }
        },
        [onMouseEnter, startAnimation]
      );
      const handleMouseEnterCallback = usePersistCallback(handleMouseEnter);

      const handleMouseLeave = useCallback(
        (event: MouseEvent<HTMLDivElement>) => {
          if (isControlledRef.current) {
            onMouseLeave?.(event);
          } else {
            stopAnimation();
          }
        },
        [onMouseLeave, stopAnimation]
      );
      const handleMouseLeaveCallback = usePersistCallback(handleMouseLeave);

      return (
        <div
          className={cn("inline-flex items-center justify-center", className)}
          onMouseEnter={handleMouseEnterCallback}
          onMouseLeave={handleMouseLeaveCallback}
          {...props}
        >
          <MotionsvgBinding1 controls={controls} size={size} />
        </div>
      );
    }
  )
);

BrainIcon.displayName = "BrainIcon";

export { BrainIcon };

const MotionsvgBinding1 = observer(function MotionsvgBinding1({
  controls,
  size
}: {
  controls: ReturnType<typeof useAnimation>;
  size: number;
}) {
  const variantsProp1 = useMemo<
    NonNullable<React.ComponentProps<typeof motion.svg>["variants"]>
  >(
    () => ({
      normal: { scale: 1, strokeWidth: 2 },
      animate: {
        scale: [1, 1.08, 1],
        strokeWidth: [2, 2.25, 2],
        transition: brainTransition
      }
    }),
    [brainTransition]
  );
  return (
    <motion.svg
      animate={controls}
      fill="none"
      height={size}
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      strokeWidth={2}
      variants={variantsProp1}
      viewBox="0 0 24 24"
      width={size}
      xmlns="http://www.w3.org/2000/svg"
    >
      <motion.path
        animate={controls}
        d="M12 18V5"
        variants={BRAIN_STEM_VARIANTS}
      />
      <motion.path
        animate={controls}
        d="M15 13a4.17 4.17 0 0 1-3-4 4.17 4.17 0 0 1-3 4"
        variants={BRAIN_SIDE_VARIANTS}
      />

      <motion.path
        animate={controls}
        d="M12 5A3 3 0 1 1 17.598 6.5"
        variants={BRAIN_TOP_ARC_VARIANTS}
      />
      <motion.path
        animate={controls}
        d="M12 5A3 3 0 1 0 6.402 6.5"
        variants={BRAIN_TOP_ARC_VARIANTS}
      />

      <path d="M17.997 5.125a4 4 0 0 1 2.526 5.77" />

      <motion.path
        animate={controls}
        d="M18 18a4 4 0 0 0 2-7.464"
        variants={BRAIN_LOWER_ARC_VARIANTS}
      />

      <path d="M19.967 17.483A4 4 0 1 1 12 18a4 4 0 1 1-7.967-.517" />

      <motion.path
        animate={controls}
        d="M6 18a4 4 0 0 1-2-7.464"
        variants={BRAIN_LOWER_ARC_VARIANTS}
      />
      <path d="M6.003 5.125a4 4 0 0 0-2.526 5.77" />
    </motion.svg>
  );
});
