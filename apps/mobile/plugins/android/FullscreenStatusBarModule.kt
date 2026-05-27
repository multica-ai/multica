package __PACKAGE_NAME__

import android.app.Activity
import android.view.WindowManager
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import com.facebook.react.bridge.ReactApplicationContext
import com.facebook.react.bridge.ReactContextBaseJavaModule
import com.facebook.react.bridge.ReactMethod

class FullscreenStatusBarModule(
  private val reactContext: ReactApplicationContext
) : ReactContextBaseJavaModule(reactContext) {
  override fun getName(): String = "FullscreenStatusBar"

  @ReactMethod
  fun hide() {
    runOnActivity { activity ->
      val controller = WindowCompat.getInsetsController(activity.window, activity.window.decorView)
      controller.hide(WindowInsetsCompat.Type.statusBars())
      @Suppress("DEPRECATION")
      activity.window.addFlags(WindowManager.LayoutParams.FLAG_FULLSCREEN)
    }
  }

  @ReactMethod
  fun show() {
    runOnActivity { activity ->
      @Suppress("DEPRECATION")
      activity.window.clearFlags(WindowManager.LayoutParams.FLAG_FULLSCREEN)
      val controller = WindowCompat.getInsetsController(activity.window, activity.window.decorView)
      controller.show(WindowInsetsCompat.Type.statusBars())
    }
  }

  private fun runOnActivity(action: (Activity) -> Unit) {
    val activity = reactContext.currentActivity ?: return
    activity.runOnUiThread { action(activity) }
  }
}
