import { type ClassValue, clsx } from "clsx";
import { extendTailwindMerge } from "tailwind-merge";

const twMerge = extendTailwindMerge({ extend: { classGroups: { "font-size": [{ text: ["caption", "body", "title-sm"] }] } } });

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
