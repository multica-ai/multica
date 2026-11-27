import type { IssueTableFilters, PropertyFilterValue } from "../types";

/** Parse the finite strconv.ParseFloat grammar without rewriting a query member. */
export function parseGraphPropertyNumber(value: string): number | undefined {
  const decimal = /^[+-]?(?:[0-9](?:_?[0-9])*(?:\.(?:[0-9](?:_?[0-9])*)?)?|\.[0-9](?:_?[0-9])*)(?:[eE][+-]?[0-9](?:_?[0-9])*)?$/;
  if (decimal.test(value)) {
    const number = Number(value.replace(/_/g, ""));
    return Number.isFinite(number) ? number : undefined;
  }
  const hex = /^([+-]?)0[xX]_?((?:[0-9a-fA-F](?:_?[0-9a-fA-F])*)(?:\.(?:[0-9a-fA-F](?:_?[0-9a-fA-F])*)?)?|\.[0-9a-fA-F](?:_?[0-9a-fA-F])*)[pP]([+-]?[0-9](?:_?[0-9])*)$/.exec(value);
  if (!hex) return undefined;
  const mantissa = hex[2]!.replace(/_/g, ""), fractionLength = mantissa.includes(".") ? mantissa.length - mantissa.indexOf(".") - 1 : 0;
  const significand = BigInt(`0x${mantissa.replace(".", "")}`), sign = hex[1] === "-" ? -1 : 1;
  if (significand === 0n) return sign * 0;
  const exponent = Number(hex[3]!.replace(/_/g, "")) - fractionLength * 4;
  const bits = significand.toString(2).length, power = exponent + bits - 1;
  if (power > 1023) return undefined;
  if (power < -1075) return sign * 0;
  const shift = power < -1022 ? -exponent - 1074 : Math.max(0, bits - 53);
  const rounded = roundBinary(significand, shift);
  const number = sign * Number(rounded) * 2 ** (exponent + shift);
  return Number.isFinite(number) ? number : undefined;
}

export function validGraphPropertyValue(value: PropertyFilterValue): boolean {
  if (typeof value === "string") return value !== "";
  if (!value || typeof value.value !== "string" || value.value === "") return false;
  if (value.op === "contains") return [...value.value].length <= 2000;
  if (["gt", "gte", "lt", "lte"].includes(value.op)) return parseGraphPropertyNumber(value.value) !== undefined;
  if (value.op === "before" || value.op === "after") {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(value.value)) return false;
    const date = new Date(`${value.value}T00:00:00Z`);
    return Number.isFinite(date.getTime()) && date.toISOString().slice(0, 10) === value.value;
  }
  return false;
}

/** Bound the same equality expansions and operator members as the property compiler. */
export function validGraphPropertyFilters(properties: IssueTableFilters["properties"]): boolean {
  const definitions = Object.values(properties ?? {});
  if (definitions.length > 20) return false;
  let alternatives = 0;
  for (const values of definitions) {
    if (values.length > 50 || !values.every(validGraphPropertyValue)) return false;
    let unset = false;
    for (const value of values) {
      if (typeof value !== "string") alternatives++;
      else if (value === "__none__") { if (!unset) alternatives++; unset = true; }
      else alternatives += 2 + Number(value === "true" || value === "false" || parseGraphPropertyNumber(value) !== undefined);
    }
  }
  return alternatives <= 256;
}

// Round binary significands to nearest, ties to even, before exponent scaling.
function roundBinary(value: bigint, shift: number): bigint {
  if (shift <= 0) return value << BigInt(-shift);
  const remainder = value & ((1n << BigInt(shift)) - 1n), halfway = 1n << BigInt(shift - 1);
  const rounded = value >> BigInt(shift);
  return remainder > halfway || remainder === halfway && (rounded & 1n) === 1n ? rounded + 1n : rounded;
}
