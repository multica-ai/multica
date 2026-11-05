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

export interface FilePenLineIconHandle {
  startAnimation: () => void;
  stopAnimation: () => void;
}

interface FilePenLineIconProps extends HTMLAttributes<HTMLDivElement> {
  size?: number;
  active?: boolean;
}

const PEN_VARIANTS: Variants = {
  normal: {
    rotate: 0,
    x: 0,
    y: 0
  },
  animate: {
    rotate: [-0.3, 0.2, -0.4],
    x: [0, -0.5, 1, 0],
    y: [0, 1, -0.5, 0],
    transition: {
      duration: 0.5,
      repeat: 1,
      ease: "easeInOut"
    }
  },
  active: {
    rotate: [-0.3, 0.2, -0.4],
    x: [0, -0.5, 1, 0],
    y: [0, 1, -0.5, 0],
    transition: {
      duration: 0.5,
      repeat: Number.POSITIVE_INFINITY,
      repeatDelay: 0.75,
      ease: "easeInOut"
    }
  }
};

const LINE_VARIANTS: Variants = {
  normal: { d: "M8 18h1" },
  animate: {
    d: "M8 18h5",
    transition: { duration: 0.5 }
  },
  active: {
    d: ["M8 18h1", "M8 18h5", "M8 18h1"],
    transition: {
      duration: 0.8,
      repeat: Number.POSITIVE_INFINITY,
      repeatDelay: 0.45,
      ease: "easeInOut"
    }
  }
};

const FilePenLineIcon = observer(
  forwardRef<FilePenLineIconHandle, FilePenLineIconProps>(
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
        void controls.start(active ? "active" : "animate");
      }, [active, controls, reduceMotion]);

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
          <svg
            fill="none"
            height={size}
            stroke="currentColor"
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth="2"
            viewBox="0 0 24 24"
            width={size}
            xmlns="http://www.w3.org/2000/svg"
          >
            <path d="m18 5-2.414-2.414A2 2 0 0 0 14.172 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2" />
            <motion.path
              animate={controls}
              d="M21.378 12.626a1 1 0 0 0-3.004-3.004l-4.01 4.012a2 2 0 0 0-.506.854l-.837 2.87a.5.5 0 0 0 .62.62l2.87-.837a2 2 0 0 0 .854-.506z"
              initial="normal"
              variants={PEN_VARIANTS}
            />
            <motion.path
              animate={controls}
              d="M8 18h1"
              initial="normal"
              variants={LINE_VARIANTS}
            />
          </svg>
        </div>
      );
    }
  )
);

FilePenLineIcon.displayName = "FilePenLineIcon";

export { FilePenLineIcon };
