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

export interface HammerIconHandle {
  startAnimation: () => void;
  stopAnimation: () => void;
}

interface HammerIconProps extends HTMLAttributes<HTMLDivElement> {
  size?: number;
  active?: boolean;
}

const HAMMER_VARIANTS: Variants = {
  normal: {
    rotate: 0,
    transition: {
      duration: 0.3,
      ease: "easeOut"
    }
  },
  animate: {
    rotate: [0, -20, 25, 0],
    transition: {
      duration: 0.8,
      times: [0, 0.6, 0.8, 1],
      ease: ["easeInOut", "easeOut", "easeOut"]
    }
  },
  active: {
    rotate: [0, -20, 25, 0],
    transition: {
      duration: 0.8,
      times: [0, 0.6, 0.8, 1],
      ease: ["easeInOut", "easeOut", "easeOut"],
      repeat: Infinity,
      repeatDelay: 0.9
    }
  }
};

const HammerIcon = observer(
  forwardRef<HammerIconHandle, HammerIconProps>(
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

HammerIcon.displayName = "HammerIcon";

export { HammerIcon };

const MotionsvgBinding1 = observer(function MotionsvgBinding1({
  controls,
  size
}: {
  controls: ReturnType<typeof useAnimation>;
  size: number;
}) {
  const styleProp1 = useMemo<
    NonNullable<React.ComponentProps<typeof motion.svg>["style"]>
  >(() => ({ transformOrigin: "0% 100%", transformBox: "fill-box" }), []);
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
      style={styleProp1}
      variants={HAMMER_VARIANTS}
      viewBox="0 0 24 24"
      width={size}
      xmlns="http://www.w3.org/2000/svg"
    >
      <path d="m15 12-9.373 9.373a1 1 0 0 1-3.001-3L12 9" />
      <path d="m18 15 4-4" />
      <path d="m21.5 11.5-1.914-1.914A2 2 0 0 1 19 8.172v-.344a2 2 0 0 0-.586-1.414l-1.657-1.657A6 6 0 0 0 12.516 3H9l1.243 1.243A6 6 0 0 1 12 8.485V10l2 2h1.172a2 2 0 0 1 1.414.586L18.5 14.5" />
    </motion.svg>
  );
});
