"use client";

import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";

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
  type Variants
} from "framer-motion";
import { cn } from "../../lib/utils";
import { observer } from "mobx-react-lite";

export interface CastIconHandle {
  startAnimation: () => void;
  stopAnimation: () => void;
}

interface CastIconProps extends HTMLAttributes<HTMLDivElement> {
  size?: number;
  active?: boolean;
}

const CAST_VARIANTS: Variants = {
  normal: { opacity: 1 },
  animate: (custom: number) => ({
    opacity: [0.18, 1, 0.18],
    transition: {
      delay: custom,
      duration: 1.1,
      repeat: Number.POSITIVE_INFINITY,
      ease: "easeInOut"
    }
  })
};

const CastIcon = observer(
  forwardRef<CastIconHandle, CastIconProps>(
    (
      {
        active = false,
        onMouseEnter,
        onMouseLeave,
        className,
        size = 16,
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
            return;
          }
          startAnimation();
        },
        [onMouseEnter, startAnimation]
      );
      const handleMouseEnterCallback = usePersistCallback(handleMouseEnter);

      const handleMouseLeave = useCallback(
        (event: MouseEvent<HTMLDivElement>) => {
          if (isControlledRef.current) {
            onMouseLeave?.(event);
            return;
          }
          stopAnimation();
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
          <svg
            fill="none"
            height={size}
            stroke="currentColor"
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2}
            viewBox="0 0 24 24"
            width={size}
            xmlns="http://www.w3.org/2000/svg"
          >
            <path d="M2 8V6a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-6" />
            <motion.path
              animate={controls}
              custom={0.2}
              d="M2 12a9 9 0 0 1 8 8"
              variants={CAST_VARIANTS}
            />
            <motion.path
              animate={controls}
              custom={0.1}
              d="M2 16a5 5 0 0 1 4 4"
              variants={CAST_VARIANTS}
            />
            <motion.line
              animate={controls}
              custom={0}
              variants={CAST_VARIANTS}
              x1="2"
              x2="2.01"
              y1="20"
              y2="20"
            />
          </svg>
        </div>
      );
    }
  )
);

CastIcon.displayName = "CastIcon";

export { CastIcon };
