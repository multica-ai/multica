package __PACKAGE_NAME__.push

import android.content.Context
import com.igexin.sdk.GTIntentService
import com.igexin.sdk.message.GTCmdMessage
import com.igexin.sdk.message.GTNotificationMessage
import com.igexin.sdk.message.GTTransmitMessage

class GetuiIntentService : GTIntentService() {
  override fun onReceiveServicePid(context: Context?, pid: Int) {
  }

  override fun onReceiveClientId(context: Context?, clientid: String?) {
    GetuiPushState.setClientId(context, clientid)
  }

  override fun onReceiveMessageData(context: Context?, msg: GTTransmitMessage?) {
  }

  override fun onReceiveOnlineState(context: Context?, online: Boolean) {
  }

  override fun onReceiveCommandResult(context: Context?, cmdMessage: GTCmdMessage?) {
  }

  override fun onNotificationMessageArrived(context: Context?, msg: GTNotificationMessage?) {
  }

  override fun onNotificationMessageClicked(context: Context?, msg: GTNotificationMessage?) {
    GetuiPushState.setNotificationUrl(context, extractNotificationUrl(msg))
  }

  private fun extractNotificationUrl(msg: GTNotificationMessage?): String? {
    val candidates = listOf(
      msg?.payload,
      msg?.url,
      msg?.intentUri,
      msg?.content,
    )
    return candidates.firstOrNull { it?.trim()?.startsWith("wujieai-multicam://") == true }
  }
}
