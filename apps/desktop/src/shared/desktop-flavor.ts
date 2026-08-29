export type DesktopFlavor = "multica" | "lifeos";

export interface DesktopIdentity {
  appName: string;
  appUserModelId: string;
  protocol: string;
}

export function resolveDesktopFlavor(
  packagedAppName: string,
  explicitLifeOSMode?: string,
): DesktopFlavor {
  if (explicitLifeOSMode === "true") return "lifeos";
  return packagedAppName.trim().toLowerCase() === "lifeos"
    ? "lifeos"
    : "multica";
}

export function desktopIdentity(
  flavor: DesktopFlavor,
  isDev: boolean,
  devSuffix?: string,
): DesktopIdentity {
  if (flavor === "lifeos") {
    const appName = isDev
      ? devSuffix
        ? `LifeOS Canary ${devSuffix}`
        : "LifeOS Canary"
      : "LifeOS";
    return {
      appName,
      appUserModelId: isDev ? "ai.lifeos.desktop.dev" : "ai.lifeos.desktop",
      protocol: "lifeos",
    };
  }

  const appName = isDev
    ? devSuffix
      ? `Multica Canary ${devSuffix}`
      : "Multica Canary"
    : "Multica";
  return {
    appName,
    appUserModelId: isDev ? "ai.multica.desktop.dev" : "ai.multica.desktop",
    protocol: "multica",
  };
}
