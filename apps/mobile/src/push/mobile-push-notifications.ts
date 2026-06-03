import {
  addApnsNotificationUrlListener,
  consumeApnsPendingNotificationUrl,
} from "./apns-push";
import {
  addGetuiNotificationUrlListener,
  consumeGetuiPendingNotificationUrl,
} from "./getui-push";

export async function consumeMobilePushPendingNotificationUrl(): Promise<string | null> {
  const [apnsUrl, getuiUrl] = await Promise.all([
    consumeApnsPendingNotificationUrl(),
    consumeGetuiPendingNotificationUrl(),
  ]);
  return apnsUrl ?? getuiUrl;
}

export function addMobilePushNotificationUrlListener(
  listener: (url: string) => void,
): () => void {
  const removeApnsListener = addApnsNotificationUrlListener(listener);
  const removeGetuiListener = addGetuiNotificationUrlListener(listener);
  return () => {
    removeApnsListener();
    removeGetuiListener();
  };
}
