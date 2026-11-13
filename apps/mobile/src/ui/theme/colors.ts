// Status and brand hues are packages/ui/styles/tokens.css converted from OKLCH,
// which React Native cannot parse; keep them in step with that file.
export interface Palette {
  scheme: "light" | "dark";
  background: string;
  grouped: string;
  fill: string;
  fillStrong: string;
  label: string;
  label2: string;
  label3: string;
  separator: string;
  float: string;
  card: string;
  cardEdge: string;
  accent: string;
  onAccent: string;
  add: string;
  del: string;
  success: string;
  warning: string;
  info: string;
  destructive: string;
  priorityHigh: string;
  scrim: string;
  shadow: string;
  shadowOpacity: number;
  avatars: readonly string[];
}

export const light: Palette = {
  scheme: "light",
  background: "#ffffff",
  grouped: "#f4f4f5",
  fill: "rgba(118, 118, 128, 0.10)",
  fillStrong: "rgba(118, 118, 128, 0.16)",
  label: "#111113",
  label2: "#6e6e73",
  label3: "#a1a1a6",
  separator: "rgba(60, 60, 67, 0.12)",
  float: "#ffffff",
  card: "#ffffff",
  cardEdge: "rgba(0, 0, 0, 0.08)",
  accent: "#0070e3",
  onAccent: "#ffffff",
  add: "#1a9b4b",
  del: "#d93f3f",
  success: "#1c882d",
  warning: "#dca400",
  info: "#0072d5",
  destructive: "#e7000b",
  priorityHigh: "#ec6f00",
  scrim: "rgba(255, 255, 255, 0.45)",
  shadow: "#000000",
  shadowOpacity: 0.08,
  avatars: ["#4a9fd1", "#c48a3a", "#7f6fd6", "#3f9e8f", "#d16a8a", "#6b8f3a"],
};

export const dark: Palette = {
  scheme: "dark",
  background: "#0d0d0d",
  grouped: "#1a1a1b",
  fill: "rgba(255, 255, 255, 0.07)",
  fillStrong: "rgba(255, 255, 255, 0.12)",
  label: "#f2f2f3",
  label2: "#8e8e93",
  label3: "#5c5c61",
  separator: "rgba(255, 255, 255, 0.09)",
  float: "#1f1f20",
  card: "#161617",
  cardEdge: "rgba(255, 255, 255, 0.09)",
  accent: "#4390ee",
  onAccent: "#ffffff",
  add: "#3fbf6a",
  del: "#f06363",
  success: "#4aa651",
  warning: "#cb9400",
  info: "#0f92f7",
  destructive: "#ff6467",
  priorityHigh: "#f67f2f",
  scrim: "rgba(0, 0, 0, 0.45)",
  shadow: "#000000",
  shadowOpacity: 0.5,
  avatars: ["#3d86b3", "#a87532", "#6c5dbd", "#34877a", "#b65a77", "#5b7a31"],
};
