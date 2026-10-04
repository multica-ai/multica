/**
 * expo-haptics-shaped haptics over the MulticaHaptics TurboModule. Ported
 * screens keep their iOS call shapes (`Haptics.impactAsync(...)`); every
 * call is a silent no-op when the native module is unavailable.
 *
 * `selectionAsync` has no dedicated OHOS preset — it maps to the light
 * impact, which is the closest "tick" intensity.
 */
import { haptics } from "@/lib/native-modules";

export enum ImpactFeedbackStyle {
  Light = "light",
  Medium = "medium",
}

export enum NotificationFeedbackType {
  Success = "success",
  Warning = "warning",
  Error = "error",
}

export async function impactAsync(
  style: ImpactFeedbackStyle = ImpactFeedbackStyle.Medium,
): Promise<void> {
  if (!haptics) return;
  if (style === ImpactFeedbackStyle.Light) {
    await haptics.impactLight();
  } else {
    await haptics.impactMedium();
  }
}

export async function notificationAsync(
  type: NotificationFeedbackType = NotificationFeedbackType.Success,
): Promise<void> {
  if (!haptics) return;
  if (type === NotificationFeedbackType.Error) {
    await haptics.notificationError();
  } else {
    await haptics.notificationSuccess();
  }
}

export async function selectionAsync(): Promise<void> {
  if (!haptics) return;
  await haptics.impactLight();
}
