package __PACKAGE_NAME__.push

import com.facebook.react.bridge.Promise
import com.facebook.react.bridge.ReactApplicationContext
import com.facebook.react.bridge.ReactContextBaseJavaModule
import com.facebook.react.bridge.ReactMethod
import com.igexin.sdk.PushManager

class GetuiPushModule(
  private val reactContext: ReactApplicationContext
) : ReactContextBaseJavaModule(reactContext) {
  init {
    GetuiPushState.attach(reactContext)
  }

  override fun getName(): String = "GetuiPush"

  override fun invalidate() {
    GetuiPushState.detach(reactContext)
    super.invalidate()
  }

  @ReactMethod
  fun initialize(promise: Promise) {
    try {
      val appContext = reactContext.applicationContext
      PushManager.getInstance().initialize(appContext)
      val sdkClientId = PushManager.getInstance().getClientid(appContext)
      if (!sdkClientId.isNullOrBlank()) {
        GetuiPushState.setClientId(appContext, sdkClientId)
        promise.resolve(sdkClientId)
        return
      }
      promise.resolve(GetuiPushState.getClientId(appContext))
    } catch (error: Throwable) {
      promise.reject("getui_initialize_failed", error)
    }
  }

  @ReactMethod
  fun getClientId(promise: Promise) {
    try {
      val appContext = reactContext.applicationContext
      val sdkClientId = PushManager.getInstance().getClientid(appContext)
      if (!sdkClientId.isNullOrBlank()) {
        GetuiPushState.setClientId(appContext, sdkClientId)
        promise.resolve(sdkClientId)
        return
      }
      promise.resolve(GetuiPushState.getClientId(appContext))
    } catch (error: Throwable) {
      promise.reject("getui_client_id_failed", error)
    }
  }

  @ReactMethod
  fun getPendingNotificationUrl(promise: Promise) {
    try {
      promise.resolve(GetuiPushState.getPendingNotificationUrl(reactContext.applicationContext))
    } catch (error: Throwable) {
      promise.reject("getui_pending_notification_url_failed", error)
    }
  }

  @ReactMethod
  fun consumePendingNotificationUrl(promise: Promise) {
    try {
      promise.resolve(GetuiPushState.consumePendingNotificationUrl(reactContext.applicationContext))
    } catch (error: Throwable) {
      promise.reject("getui_consume_notification_url_failed", error)
    }
  }

  @ReactMethod
  fun addListener(eventName: String) {
    // Required by NativeEventEmitter.
  }

  @ReactMethod
  fun removeListeners(count: Double) {
    // Required by NativeEventEmitter.
  }
}
