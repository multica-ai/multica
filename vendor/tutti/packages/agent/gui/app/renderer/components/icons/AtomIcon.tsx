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

export interface AtomIconHandle {
  startAnimation: () => void;
  stopAnimation: () => void;
}

interface AtomIconProps extends HTMLAttributes<HTMLDivElement> {
  size?: number;
  active?: boolean;
}

const ATOM_ICON_ANIMATION_DURATION = 0.75;
const ATOM_ICON_REPEAT_DELAY = 1.5;

const PATH_VARIANTS: Variants = {
  normal: (custom: number) => ({
    opacity: 1,
    pathLength: 1,
    pathOffset: 0,
    transition: {
      duration: ATOM_ICON_ANIMATION_DURATION,
      ease: "easeInOut",
      delay: custom
    }
  }),
  animate: (custom: number) => ({
    opacity: [0, 1],
    pathLength: [0, 1],
    pathOffset: [1, 0],
    transition: {
      duration: ATOM_ICON_ANIMATION_DURATION,
      ease: "easeInOut",
      delay: custom
    }
  }),
  active: (custom: number) => ({
    opacity: [0, 1],
    pathLength: [0, 1],
    pathOffset: [1, 0],
    transition: {
      duration: ATOM_ICON_ANIMATION_DURATION,
      ease: "easeInOut",
      delay: custom,
      repeat: Infinity,
      repeatDelay: ATOM_ICON_REPEAT_DELAY
    }
  })
};

const AtomIcon = observer(
  forwardRef<AtomIconHandle, AtomIconProps>(
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
            <motion.circle
              animate={controls}
              custom={0}
              cx="12"
              cy="12"
              r="1"
              variants={PATH_VARIANTS}
            />
            <motion.path
              animate={controls}
              custom={0.3}
              d="M20.2 20.2c2.04-2.03.02-7.36-4.5-11.9-4.54-4.52-9.87-6.54-11.9-4.5-2.04 2.03-.02 7.36 4.5 11.9 4.54 4.52 9.87 6.54 11.9 4.5Z"
              variants={PATH_VARIANTS}
            />
            <motion.path
              animate={controls}
              custom={0.6}
              d="M15.7 15.7c4.52-4.54 6.54-9.87 4.5-11.9-2.03-2.04-7.36-.02-11.9 4.5-4.52 4.54-6.54 9.87-4.5 11.9 2.03 2.04 7.36.02 11.9-4.5Z"
              variants={PATH_VARIANTS}
            />
          </svg>
        </div>
      );
    }
  )
);

AtomIcon.displayName = "AtomIcon";

export { AtomIcon };
