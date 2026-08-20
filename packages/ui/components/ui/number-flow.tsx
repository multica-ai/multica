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

const COMPACT_UNITS = [
  { divisor: 1, upperSuffix: undefined, lowerSuffix: undefined },
  { divisor: 1_000, upperSuffix: "K", lowerSuffix: "k" },
  { divisor: 1_000_000, upperSuffix: "M", lowerSuffix: "m" },
  { divisor: 1_000_000_000, upperSuffix: "B", lowerSuffix: "b" },
  { divisor: 1_000_000_000_000, upperSuffix: "T", lowerSuffix: "t" },
] as const

function CompactNumberFlow({
  value,
  suffixCase = "upper",
  locales,
  "aria-label": ariaLabel,
  ...props
}: CompactNumberFlowProps) {
  const magnitude = Math.abs(value)
  let unitIndex = COMPACT_UNITS.findLastIndex(
    ({ divisor }) => magnitude >= divisor,
  )
  unitIndex = Math.max(unitIndex, 0)

  let unit = COMPACT_UNITS[unitIndex]!
  let scaledValue = value / unit.divisor

  if (
    Math.abs(Number(scaledValue.toFixed(1))) >= 1_000 &&
    unitIndex < COMPACT_UNITS.length - 1
  ) {
    unit = COMPACT_UNITS[unitIndex + 1]!
    scaledValue = value / unit.divisor
  }

  const roundedValue = Number(scaledValue.toFixed(1))
  const fractionDigits =
    unit.divisor > 1 && !Number.isInteger(roundedValue) ? 1 : 0
  const suffix =
    suffixCase === "upper" ? unit.upperSuffix : unit.lowerSuffix
  const format: NumberFlowFormat = {
    minimumFractionDigits: fractionDigits,
    maximumFractionDigits: fractionDigits,
    useGrouping: unit.divisor === 1,
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
