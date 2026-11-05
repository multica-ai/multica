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

export interface MessageSquareMoreIconHandle {
  startAnimation: () => void;
  stopAnimation: () => void;
}

interface MessageSquareMoreIconProps extends HTMLAttributes<HTMLDivElement> {
  size?: number;
  active?: boolean;
}

const DOT_TRANSITION = {
  times: [0, 0.1, 0.1, 0.2, 0.5, 0.6, 0.6, 0.7],
  duration: 1.5
};

const DOT_VARIANTS: Variants = {
  normal: {
    opacity: 1
  },
  animate: (custom: number) => ({
    opacity: [1, 0, 0, 1, 1, 0, 0, 1],
    transition: {
      opacity: {
        ...DOT_TRANSITION,
        times: DOT_TRANSITION.times.map((time, index) =>
          index === 2 || index === 3 || index === 6 || index === 7
            ? time + custom * 0.1
            : time
        )
      }
    }
  }),
  active: (custom: number) => ({
    opacity: [1, 0, 0, 1, 1, 0, 0, 1],
    transition: {
      opacity: {
        ...DOT_TRANSITION,
        repeat: Infinity,
        times: DOT_TRANSITION.times.map((time, index) =>
          index === 2 || index === 3 || index === 6 || index === 7
            ? time + custom * 0.1
            : time
        )
      }
    }
  })
};

const MessageSquareMoreIcon = observer(
  forwardRef<MessageSquareMoreIconHandle, MessageSquareMoreIconProps>(
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
            <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
            <motion.path
              animate={controls}
              custom={0}
              d="M8 10h.01"
              variants={DOT_VARIANTS}
            />
            <motion.path
              animate={controls}
              custom={1}
              d="M12 10h.01"
              variants={DOT_VARIANTS}
            />
            <motion.path
              animate={controls}
              custom={2}
              d="M16 10h.01"
              variants={DOT_VARIANTS}
            />
          </svg>
        </div>
      );
    }
  )
);

MessageSquareMoreIcon.displayName = "MessageSquareMoreIcon";

export { MessageSquareMoreIcon };
