import * as React from "react";
import { Pressable, View, type PressableProps, type ViewProps } from "react-native";
import { cn } from "@/lib/utils";

const Card = React.forwardRef<View, ViewProps & { className?: string }>(
  ({ className, ...props }, ref) => (
    <View
      ref={ref}
      className={cn(
        "rounded-md border border-border bg-card p-4",
        className,
      )}
      {...props}
    />
  ),
);
Card.displayName = "Card";

const CardPressable = React.forwardRef<
  View,
  PressableProps & { className?: string; children?: React.ReactNode }
>(({ className, children, ...props }, ref) => (
  <Pressable
    ref={ref as React.Ref<View>}
    className={cn(
      "rounded-md border border-border bg-card p-4 active:bg-secondary",
      className,
    )}
    {...props}
  >
    {children as React.ReactNode}
  </Pressable>
));
CardPressable.displayName = "CardPressable";

export { Card, CardPressable };
