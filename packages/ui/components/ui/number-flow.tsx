"use client"

import NumberFlowPrimitive, {
  NumberFlowGroup,
  type NumberFlowProps,
} from "@number-flow/react"

import { cn } from "../../lib/utils"

function NumberFlow({
  className,
  isolate = true,
  respectMotionPreference = true,
  ...props
}: NumberFlowProps) {
  return (
    <NumberFlowPrimitive
      className={cn("tabular-nums", className)}
      isolate={isolate}
      respectMotionPreference={respectMotionPreference}
      {...props}
    />
  )
}

type NumberFlowFormat = NonNullable<NumberFlowProps["format"]>

type CurrencyNumberFlowProps = Omit<
  NumberFlowProps,
  "format" | "prefix" | "suffix" | "value"
> & {
  value: number
  prefix?: string
  fractionThreshold?: number
}

function CurrencyNumberFlow({
  value,
  prefix = "$",
  fractionThreshold = 100,
  locales,
  "aria-label": ariaLabel,
  ...props
}: CurrencyNumberFlowProps) {
  const fractionDigits = Math.abs(value) >= fractionThreshold ? 0 : 2
  const format: NumberFlowFormat = {
    minimumFractionDigits: fractionDigits,
    maximumFractionDigits: fractionDigits,
    useGrouping: false,
  }
  const formatted = new Intl.NumberFormat(locales, format).format(value)

  return (
    <NumberFlow
      value={value}
      locales={locales}
      prefix={prefix}
      format={format}
      aria-label={ariaLabel ?? `${prefix}${formatted}`}
      {...props}
    />
  )
}

type CompactNumberFlowProps = Omit<
  NumberFlowProps,
  "format" | "suffix" | "value"
> & {
  value: number
  suffixCase?: "lower" | "upper"
}

function CompactNumberFlow({
  value,
  suffixCase = "upper",
  locales,
  "aria-label": ariaLabel,
  ...props
}: CompactNumberFlowProps) {
  const magnitude = Math.abs(value)
  const divisor =
    magnitude >= 1_000_000 ? 1_000_000 : magnitude >= 1_000 ? 1_000 : 1
  const scaledValue = value / divisor
  const fractionDigits =
    divisor > 1 && Math.abs(scaledValue % 1) >= 0.05 ? 1 : 0
  const suffix =
    divisor === 1_000_000
      ? suffixCase === "upper"
        ? "M"
        : "m"
      : divisor === 1_000
        ? suffixCase === "upper"
          ? "K"
          : "k"
        : undefined
  const format: NumberFlowFormat = {
    minimumFractionDigits: fractionDigits,
    maximumFractionDigits: fractionDigits,
    useGrouping: divisor === 1,
  }
  const formatted = new Intl.NumberFormat(locales, format).format(scaledValue)

  return (
    <NumberFlow
      value={scaledValue}
      locales={locales}
      suffix={suffix}
      format={format}
      aria-label={ariaLabel ?? `${formatted}${suffix ?? ""}`}
      {...props}
    />
  )
}

type CappedNumberFlowProps = Omit<
  NumberFlowProps,
  "format" | "suffix" | "value"
> & {
  value: number
  max?: number
}

function CappedNumberFlow({
  value,
  max = 99,
  "aria-label": ariaLabel,
  ...props
}: CappedNumberFlowProps) {
  const cappedValue = Math.min(Math.max(value, 0), max)

  return (
    <NumberFlow
      value={cappedValue}
      suffix={value > max ? "+" : undefined}
      format={{ maximumFractionDigits: 0, useGrouping: false }}
      aria-label={ariaLabel ?? String(value)}
      {...props}
    />
  )
}

export {
  CappedNumberFlow,
  CompactNumberFlow,
  CurrencyNumberFlow,
  NumberFlow,
  NumberFlowGroup,
}
export type {
  CappedNumberFlowProps,
  CompactNumberFlowProps,
  CurrencyNumberFlowProps,
  NumberFlowProps,
}
