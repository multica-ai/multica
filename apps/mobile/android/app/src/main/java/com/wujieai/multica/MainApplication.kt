package com.wujieai.multica

import android.app.ActivityManager
import android.os.Build

import android.util.Log
import com.igexin.sdk.IUserLoggerInterface
import com.igexin.sdk.PushManager

import android.app.Application
import android.content.res.Configuration

import com.facebook.react.PackageList
import com.facebook.react.ReactApplication
import com.facebook.react.ReactNativeApplicationEntryPoint.loadReactNative
import com.facebook.react.ReactPackage
import com.facebook.react.ReactHost
import com.facebook.react.common.ReleaseLevel
import com.facebook.react.defaults.DefaultNewArchitectureEntryPoint

import expo.modules.ApplicationLifecycleDispatcher
import expo.modules.ExpoReactHostFactory

class MainApplication : Application(), ReactApplication {

  override val reactHost: ReactHost by lazy {
    ExpoReactHostFactory.getDefaultReactHost(
      context = applicationContext,
      packageList =
        PackageList(this).packages.apply {
          add(com.wujieai.multica.push.GetuiPushPackage())
          // Packages that cannot be autolinked yet can be added manually here, for example:
          // add(MyReactNativePackage())
          add(FullscreenStatusBarPackage())
        }
    )
  }

  override fun onCreate() {
    super.onCreate()
    if (!isMainProcess()) {
      return
    }
    if (BuildConfig.DEBUG) {
      PushManager.getInstance().setDebugLogger(this, object : IUserLoggerInterface {
        override fun log(s: String?) {
          Log.i("PUSH_LOG", s ?: "")
        }
      })
    }
    DefaultNewArchitectureEntryPoint.releaseLevel = try {
      ReleaseLevel.valueOf(BuildConfig.REACT_NATIVE_RELEASE_LEVEL.uppercase())
    } catch (e: IllegalArgumentException) {
      ReleaseLevel.STABLE
    }
    loadReactNative(this)
    ApplicationLifecycleDispatcher.onApplicationCreate(this)
  }
  private fun isMainProcess(): Boolean {
    val processName = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
      Application.getProcessName()
    } else {
      val pid = android.os.Process.myPid()
      val activityManager = getSystemService(ACTIVITY_SERVICE) as? ActivityManager
      activityManager
        ?.runningAppProcesses
        ?.firstOrNull { it.pid == pid }
        ?.processName
    }
    return processName == packageName
  }


  override fun onConfigurationChanged(newConfig: Configuration) {
    super.onConfigurationChanged(newConfig)
    if (!isMainProcess()) {
      return
    }
    ApplicationLifecycleDispatcher.onConfigurationChanged(this, newConfig)
  }
}
