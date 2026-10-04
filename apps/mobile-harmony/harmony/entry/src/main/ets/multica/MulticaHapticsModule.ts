/**
 * Haptic feedback via @ohos.vibrator (ohos.permission.VIBRATE is declared in
 * module.json5). Mirrors the expo-haptics calls the app makes:
 * ImpactFeedbackStyle.Light/Medium and NotificationFeedbackType.Success/Error.
 * Preset haptic effects are attempted first for richer patterns and fall back
 * to plain time-based vibration when the effect id is unsupported.
 */
import { AnyThreadTurboModule } from '@rnoh/react-native-openharmony/ts';
import type { AnyThreadTurboModuleContext } from '@rnoh/react-native-openharmony/ts';
import vibrator from '@ohos.vibrator';

type VibrateEffect = vibrator.VibrateEffect;
type VibrateAttribute = vibrator.VibrateAttribute;

const ATTR: VibrateAttribute = { usage: 'touch' };

export class MulticaHapticsModule extends AnyThreadTurboModule {
  constructor(protected ctx: AnyThreadTurboModuleContext) {
    super(ctx);
  }

  async impactLight(): Promise<boolean> {
    return this.presetOrTime('haptic.effect.soft', 8);
  }

  async impactMedium(): Promise<boolean> {
    return this.presetOrTime('haptic.effect.sharp', 20);
  }

  async notificationSuccess(): Promise<boolean> {
    return this.presetOrTime('haptic.effect.soft', 15);
  }

  async notificationError(): Promise<boolean> {
    return this.presetOrTime('haptic.effect.sharp', 35);
  }

  private async presetOrTime(effectId: string, fallbackMs: number): Promise<boolean> {
    try {
      const preset: VibrateEffect = { type: 'preset', effectId, count: 1 };
      await vibrator.startVibration(preset, ATTR);
      return true;
    } catch (e) {
      // Unknown effect id or unsupported mode — plain short vibration.
    }
    try {
      const timed: VibrateEffect = { type: 'time', duration: fallbackMs };
      await vibrator.startVibration(timed, ATTR);
      return true;
    } catch (e) {
      console.error(`[MulticaHaptics] vibrate failed: ${JSON.stringify(e)}`);
      return false;
    }
  }
}
