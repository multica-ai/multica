import * as React from "react";
import { Button as UISystemButton, buttonVariants } from "@tutti-os/ui-system";
import { cn } from "../../lib/utils";
import { observer } from "mobx-react-lite";

type UISystemButtonProps = React.ComponentProps<typeof UISystemButton>;
type LegacyButtonSize =
  | UISystemButtonProps["size"]
  | "mini"
  | "md"
  | "max"
  | "iconClose";

export interface ButtonProps extends Omit<UISystemButtonProps, "size"> {
  size?: LegacyButtonSize;
}

function resolveButtonSize(
  size: LegacyButtonSize | undefined
): UISystemButtonProps["size"] {
  switch (size) {
    case "mini":
      return "xs";
    case "md":
      return "default";
    case "max":
      return "lg";
    case "iconClose":
      return "icon-sm";
    default:
      return size;
  }
}

function resolveButtonClassName(
  size: LegacyButtonSize | undefined,
  className: string | undefined
): string | undefined {
  if (size !== "iconClose") {
    return className;
  }

  return cn(
    "h-[28px] w-[28px] min-h-[28px] min-w-[28px] shrink-0 rounded-[4px] p-0 [&_svg]:size-[14px]",
    className
  );
}

const Button = observer(
  React.forwardRef<HTMLButtonElement, ButtonProps>(
    ({ className, size, ...props }, ref) => (
      <UISystemButton
        ref={ref}
        className={resolveButtonClassName(size, className)}
        size={resolveButtonSize(size)}
        {...props}
      />
    )
  )
);

Button.displayName = "Button";

export { Button, buttonVariants };
