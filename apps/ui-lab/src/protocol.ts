import { isLabLocale, type LabLocale } from "./locale";
import {
  isDraft,
  isColorToken,
  type ColorToken,
  buttonScales,
  type ButtonScale,
  type Draft,
  type Theme,
} from "./tokens";
export const scenes = [
  { id: "colors", label: "colors" },
  { id: "dialog", label: "dialog" },
  { id: "motion", label: "motion" },
  {
    id: "components",
    label: "components",
  },
  {
    id: "button",
    label: "button",
  },
  {
    id: "list",
    label: "list",
  },
  {
    id: "detail",
    label: "detail",
  },
] as const;
export type Scene = (typeof scenes)[number]["id"];
export type PreviewSettings = {
  type: "multica-ui-lab:preview";
  draft: Draft;
  theme: Theme;
  scene: Scene;
  buttonScale: ButtonScale;
  locale: LabLocale;
  playbackSpeed: number;
  selectedColor: ColorToken;
};
export function isPreviewSettings(value: unknown): value is PreviewSettings {
  if (!value || typeof value !== "object") return false;
  const data = value as Record<string, unknown>;
  return (
    data.type === "multica-ui-lab:preview" &&
    (data.theme === "light" || data.theme === "dark") &&
    scenes.some((scene) => scene.id === data.scene) &&
    buttonScales.some((scale) => scale === data.buttonScale) &&
    isLabLocale(data.locale) &&
    isColorToken(data.selectedColor) &&
    playbackSpeeds.some((speed) => speed === data.playbackSpeed) &&
    isDraft(data.draft)
  );
}

export const playbackSpeeds = [1, 0.5, 0.25] as const;
export const dialogActions = ["open", "close", "replay"] as const;
export type DialogCommand = {
  type: "multica-ui-lab:dialog";
  action: (typeof dialogActions)[number];
};
export function isDialogCommand(value: unknown): value is DialogCommand {
  if (!value || typeof value !== "object") return false;
  const data = value as Record<string, unknown>;
  return (
    data.type === "multica-ui-lab:dialog" &&
    dialogActions.some((action) => action === data.action)
  );
}

export type ColorSelection = {
  type: "multica-ui-lab:color-select";
  token: ColorToken;
};
export function isColorSelection(value: unknown): value is ColorSelection {
  if (!value || typeof value !== "object") return false;
  const data = value as Record<string, unknown>;
  return (
    data.type === "multica-ui-lab:color-select" && isColorToken(data.token)
  );
}
