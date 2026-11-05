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
  type Variants
} from "framer-motion";
import { cn } from "../../lib/utils";
import { observer } from "mobx-react-lite";

export interface FileTextIconHandle {
  startAnimation: () => void;
  stopAnimation: () => void;
}

interface FileTextIconProps extends HTMLAttributes<HTMLDivElement> {
  size?: number;
  active?: boolean;
}

const FILE_TEXT_LINE_VARIANTS: Variants = {
  normal: {
    pathLength: 1,
    pathOffset: 0
  },
  animate: (delay: number) => ({
    pathLength: [1, 0, 1],
    pathOffset: [0, 1, 0],
    transition: {
      duration: 0.7,
      delay,
      ease: "easeInOut"
    }
  }),
  active: (delay: number) => ({
    pathLength: [1, 0, 1],
    pathOffset: [0, 1, 0],
    transition: {
      duration: 0.7,
      delay,
      ease: "easeInOut",
      repeat: Number.POSITIVE_INFINITY,
      repeatDelay: 1.1
    }
  })
};

const FileTextIcon = observer(
  forwardRef<FileTextIconHandle, FileTextIconProps>(
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
          <MotionsvgBinding1 controls={controls} size={size} />
        </div>
      );
    }
  )
);

FileTextIcon.displayName = "FileTextIcon";

export { FileTextIcon };

const MotionsvgBinding1 = observer(function MotionsvgBinding1({
  controls,
  size
}: {
  controls: ReturnType<typeof useAnimation>;
  size: number;
}) {
  const variantsProp1Value1 = Number.POSITIVE_INFINITY;
  const variantsProp1 = useMemo<
    NonNullable<React.ComponentProps<typeof motion.svg>["variants"]>
  >(
    () => ({
      normal: { scale: 1 },
      animate: { scale: 1.05, transition: { duration: 0.3, ease: "easeOut" } },
      active: {
        scale: [1, 1.05, 1],
        transition: {
          duration: 0.9,
          ease: "easeInOut",
          repeat: variantsProp1Value1,
          repeatDelay: 0.9
        }
      }
    }),
    [variantsProp1Value1]
  );
  return (
    <motion.svg
      animate={controls}
      fill="none"
      height={size}
      initial="normal"
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      strokeWidth="2"
      variants={variantsProp1}
      viewBox="0 0 24 24"
      width={size}
      xmlns="http://www.w3.org/2000/svg"
    >
      <path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z" />
      <path d="M14 2v4a2 2 0 0 0 2 2h4" />
      <motion.path
        animate={controls}
        custom={0.3}
        d="M10 9H8"
        variants={FILE_TEXT_LINE_VARIANTS}
      />
      <motion.path
        animate={controls}
        custom={0.5}
        d="M16 13H8"
        variants={FILE_TEXT_LINE_VARIANTS}
      />
      <motion.path
        animate={controls}
        custom={0.7}
        d="M16 17H8"
        variants={FILE_TEXT_LINE_VARIANTS}
      />
    </motion.svg>
  );
});
