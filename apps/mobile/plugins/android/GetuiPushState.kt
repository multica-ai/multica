package __PACKAGE_NAME__.push

import android.content.Context
import com.facebook.react.bridge.ReactApplicationContext
import com.facebook.react.modules.core.DeviceEventManagerModule

internal object GetuiPushState {
  const val EVENT_CLIENT_ID = "GetuiPushClientId"
  const val EVENT_NOTIFICATION_URL = "GetuiPushNotificationUrl"

  private const val PREFS_NAME = "multica_getui_push"
  private const val KEY_CLIENT_ID = "client_id"
  private const val KEY_PENDING_NOTIFICATION_URL = "pending_notification_url"

  private var reactContext: ReactApplicationContext? = null

  fun attach(context: ReactApplicationContext) {
    reactContext = context
    getPendingNotificationUrl(context.applicationContext)?.let { emitNotificationUrl(it) }
  }

  fun detach(context: ReactApplicationContext) {
    if (reactContext === context) {
      reactContext = null
    }
  }

  fun getClientId(context: Context): String? {
    val clientId = context
      .getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
      .getString(KEY_CLIENT_ID, null)
      ?.trim()
    return clientId?.takeIf { it.isNotEmpty() }
  }

  fun setClientId(context: Context?, clientId: String?) {
    val cleanClientId = clientId?.trim()?.takeIf { it.isNotEmpty() } ?: return
    context
      ?.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
      ?.edit()
      ?.putString(KEY_CLIENT_ID, cleanClientId)
      ?.apply()
    emitClientId(cleanClientId)
  }

  fun setNotificationUrl(context: Context?, url: String?) {
    val cleanUrl = normalizeNotificationUrl(url) ?: return
    context
      ?.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
      ?.edit()
      ?.putString(KEY_PENDING_NOTIFICATION_URL, cleanUrl)
      ?.apply()
    emitNotificationUrl(cleanUrl)
  }

  fun getPendingNotificationUrl(context: Context): String? {
    val url = context
      .getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
      .getString(KEY_PENDING_NOTIFICATION_URL, null)
    return normalizeNotificationUrl(url)
  }

  fun consumePendingNotificationUrl(context: Context): String? {
    val url = getPendingNotificationUrl(context)
    if (url != null) {
      context
        .getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        .edit()
        .remove(KEY_PENDING_NOTIFICATION_URL)
        .apply()
    }
    return url
  }

  private fun emitClientId(clientId: String) {
    reactContext
      ?.getJSModule(DeviceEventManagerModule.RCTDeviceEventEmitter::class.java)
      ?.emit(EVENT_CLIENT_ID, clientId)
  }

  private fun emitNotificationUrl(url: String) {
    reactContext
      ?.getJSModule(DeviceEventManagerModule.RCTDeviceEventEmitter::class.java)
      ?.emit(EVENT_NOTIFICATION_URL, url)
  }

  private fun normalizeNotificationUrl(url: String?): String? {
    val cleanUrl = url?.trim()?.takeIf { it.isNotEmpty() } ?: return null
    if (!cleanUrl.startsWith("wujieai-multicam://")) return null
    return cleanUrl
  }
}
