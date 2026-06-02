package __PACKAGE_NAME__.push

import android.content.Context
import com.facebook.react.bridge.ReactApplicationContext
import com.facebook.react.modules.core.DeviceEventManagerModule

internal object GetuiPushState {
  const val EVENT_CLIENT_ID = "GetuiPushClientId"

  private const val PREFS_NAME = "multica_getui_push"
  private const val KEY_CLIENT_ID = "client_id"

  private var reactContext: ReactApplicationContext? = null

  fun attach(context: ReactApplicationContext) {
    reactContext = context
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

  private fun emitClientId(clientId: String) {
    reactContext
      ?.getJSModule(DeviceEventManagerModule.RCTDeviceEventEmitter::class.java)
      ?.emit(EVENT_CLIENT_ID, clientId)
  }
}
