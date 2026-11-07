package app.asksemble.systembars

import android.graphics.Color
import android.os.Build
import android.view.Window
import android.view.WindowInsetsController
import androidx.core.view.WindowInsetsControllerCompat
import com.facebook.react.bridge.ReactApplicationContext
import com.facebook.react.interfaces.ExtraWindowEventListener
import expo.modules.kotlin.functions.Queues
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition
import java.util.Collections
import java.util.WeakHashMap

// The Android navigation bar follows the app's theme (#17, R-82).
//
// React Native sets the bar from the phone's light or dark once, as the app
// starts, and every Modal window from the phone's again; nothing sets it
// later. expo-navigation-bar could not change it on Android 12 (the S10):
// androidx's WindowInsetsControllerCompat, made from a window, changes only
// the old systemUiVisibility flag there, and Android ignores that flag once
// the window's bars have been set through setSystemBarsAppearance, which
// React Native's StatusBar does from Android 12 on. So this sets the bar the
// way React Native sets the status bar: the platform call from Android 12
// (API 31) on, the old flag below it.

// The scrims React Native's enableEdgeToEdge draws before Android 10.
private val LightNavigationBarColor = Color.argb(0xe6, 0xFF, 0xFF, 0xFF)
private val DarkNavigationBarColor = Color.argb(0x80, 0x1b, 0x1b, 0x1b)

@Suppress("DEPRECATION")
internal fun Window.setLightNavigationBar(light: Boolean) {
  if (Build.VERSION.SDK_INT > Build.VERSION_CODES.R) {
    insetsController?.setSystemBarsAppearance(
      if (light) WindowInsetsController.APPEARANCE_LIGHT_NAVIGATION_BARS else 0,
      WindowInsetsController.APPEARANCE_LIGHT_NAVIGATION_BARS,
    )
  } else if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) {
      navigationBarColor = if (light) LightNavigationBarColor else DarkNavigationBarColor
    }
    WindowInsetsControllerCompat(this, decorView).isAppearanceLightNavigationBars = light
  }
}

class SystemBarsModule : Module(), ExtraWindowEventListener {
  private val reactContext get() = appContext.reactContext as? ReactApplicationContext

  override fun definition() = ModuleDefinition {
    Name("AsksembleSystemBars")

    OnCreate {
      reactContext?.addExtraWindowEventListener(this@SystemBarsModule)
    }

    OnDestroy {
      reactContext?.removeExtraWindowEventListener(this@SystemBarsModule)
    }

    // light: dark buttons on a light bar (the app's light theme).
    AsyncFunction("setNavigationBarLight") { light: Boolean ->
      lightBar = light
      appContext.currentActivity?.window?.setLightNavigationBar(light)
      extraWindows.forEach { it.setLightNavigationBar(light) }
    }.runOnQueue(Queues.MAIN)
  }

  // A Modal window opening: React Native has just set its bar from the
  // phone's light or dark; it takes the app's.
  override fun onExtraWindowCreate(window: Window) {
    extraWindows.add(window)
    lightBar?.let { window.setLightNavigationBar(it) }
  }

  override fun onExtraWindowDestroy(window: Window) {
    extraWindows.remove(window)
  }

  companion object {
    private val extraWindows = Collections.newSetFromMap<Window>(WeakHashMap())

    @Volatile private var lightBar: Boolean? = null
  }
}
