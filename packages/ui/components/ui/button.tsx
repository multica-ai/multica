"use client"

import { Button as ButtonPrimitive } from "@base-ui/react/button"
import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "@multica/ui/lib/utils"

const buttonVariants = cva(
  "group/button inline-flex shrink-0 items-center justify-center rounded-lg border border-transparent bg-clip-padding text-body font-medium whitespace-nowrap transition-[color,background-color,border-color,box-shadow,transform] outline-none select-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 active:not-aria-[haspopup]:translate-y-px disabled:pointer-events-none disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20 dark:aria-invalid:border-destructive/50 dark:aria-invalid:ring-destructive/40 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground [a]:hover:bg-primary/80",
        outline:
          "border-border bg-background hover:bg-muted hover:text-foreground aria-expanded:bg-muted aria-expanded:text-foreground dark:border-input dark:bg-input/30 dark:hover:bg-input/50",
        // Brand-filled state for a control that is currently ON (an active
        // filter, a selected toggle). Self-contained on purpose: passing
        // brand classes through `className` on top of `outline` does NOT
        // work — `outline` ships `dark:bg-input/30`, `hover:bg-muted` and
        // `aria-expanded:bg-muted`, and those win the cascade (the `dark:`
        // ones by specificity, since `dark` compiles to `&:is(.dark *)`),
        // repainting the chip neutral. That is what silently killed the
        // brand colour in dark mode — see MUL-4884.
        //
        // `brand` needs no `dark:` of its own: the --brand token already
        // flips per theme, so one set of rules is correct in both.
        // aria-expanded is pinned to the hover value so opening a popover
        // reads as hover rather than as a colour change.
        brand:
          "border-brand bg-brand text-brand-foreground hover:bg-brand/90 hover:text-brand-foreground active:bg-brand/85 aria-expanded:bg-brand/90 aria-expanded:text-brand-foreground",
        // Brand tint for "there is activity here" — present, but not
        // claiming the loud filled state. Light and dark take their own
        // opacity notches: the same alpha does not read equally against a
        // white and a near-black surface, so dark runs one notch hotter.
        brandSubtle:
          "border-brand/28 bg-brand/7 text-foreground hover:bg-brand/12 hover:text-foreground active:bg-brand/16 aria-expanded:bg-brand/12 aria-expanded:text-foreground dark:border-brand/45 dark:bg-brand/12 dark:hover:bg-brand/18 dark:active:bg-brand/24 dark:aria-expanded:bg-brand/18",
        secondary:
          "bg-secondary text-secondary-foreground hover:bg-secondary/80 aria-expanded:bg-secondary aria-expanded:text-secondary-foreground",
        ghost:
          "hover:bg-muted hover:text-foreground aria-expanded:bg-muted aria-expanded:text-foreground dark:hover:bg-muted/50",
        destructive:
          "bg-destructive/10 text-destructive hover:bg-destructive/20 focus-visible:border-destructive/40 focus-visible:ring-destructive/20 dark:bg-destructive/20 dark:hover:bg-destructive/30 dark:focus-visible:ring-destructive/40",
        link: "text-primary underline-offset-4 hover:underline",
      },
      size: {
        default:
          "h-[var(--button-height-default)] gap-[var(--button-gap-default)] px-[var(--button-padding-default)] has-data-[icon=inline-end]:pr-[max(0px,calc(var(--button-padding-default)-0.125rem))] has-data-[icon=inline-start]:pl-[max(0px,calc(var(--button-padding-default)-0.125rem))]",
        xs: "h-[var(--button-height-xs)] gap-[var(--button-gap-xs)] rounded-md px-[var(--button-padding-xs)] text-caption in-data-[slot=button-group]:rounded-lg has-data-[icon=inline-end]:pr-[max(0px,calc(var(--button-padding-xs)-0.125rem))] has-data-[icon=inline-start]:pl-[max(0px,calc(var(--button-padding-xs)-0.125rem))] [&_svg:not([class*='size-'])]:size-3",
        sm: "h-[var(--button-height-sm)] gap-[var(--button-gap-sm)] rounded-md px-[var(--button-padding-sm)] text-label in-data-[slot=button-group]:rounded-lg has-data-[icon=inline-end]:pr-[max(0px,calc(var(--button-padding-sm)-0.25rem))] has-data-[icon=inline-start]:pl-[max(0px,calc(var(--button-padding-sm)-0.25rem))] [&_svg:not([class*='size-'])]:size-3.5",
        lg: "h-[var(--button-height-lg)] gap-[var(--button-gap-lg)] px-[var(--button-padding-lg)] has-data-[icon=inline-end]:pr-[calc(var(--button-padding-lg)+0.125rem)] has-data-[icon=inline-start]:pl-[calc(var(--button-padding-lg)+0.125rem)]",
        icon: "size-[var(--button-height-default)]",
        "icon-xs":
          "size-[var(--button-height-xs)] rounded-md in-data-[slot=button-group]:rounded-lg [&_svg:not([class*='size-'])]:size-3",
        "icon-sm":
          "size-[var(--button-height-sm)] rounded-md in-data-[slot=button-group]:rounded-lg",
        "icon-lg": "size-[var(--button-height-lg)]",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
)

function Button({
  className,
  variant = "default",
  size = "default",
  ...props
}: ButtonPrimitive.Props & VariantProps<typeof buttonVariants>) {
  return (
    <ButtonPrimitive
      data-slot="button"
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  )
}

export { Button, buttonVariants }
