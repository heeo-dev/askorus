// The camera with a QR reader behind it: shows the back camera and sends
// onScanned({ data }) for every QR code it reads. Frames are looked at on
// the phone and dropped; nothing is saved or sent anywhere (#32).
// The view starts the camera when it is put on screen and the app has the
// camera permission (the screen asks first), and stops it when taken off.
package app.askorus.qrscanner

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.graphics.Color
import android.os.SystemClock
import android.util.Log
import android.util.Size
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.ImageProxy
import androidx.camera.core.Preview
import androidx.camera.core.resolutionselector.ResolutionSelector
import androidx.camera.core.resolutionselector.ResolutionStrategy
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.core.content.ContextCompat
import androidx.lifecycle.LifecycleOwner
import expo.modules.kotlin.AppContext
import expo.modules.kotlin.viewevent.EventDispatcher
import expo.modules.kotlin.views.ExpoView
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors

private const val TAG = "AskorusQrScanner"

// The same code is in every frame while the phone points at it: tell the
// screen at most this often.
private const val REPEAT_MS = 700L

class QrScannerView(context: Context, appContext: AppContext) : ExpoView(context, appContext) {
  // PreviewView adds its surface later and asks for a layout, which React
  // Native does not answer for a native child: let Android lay this view out.
  override val shouldUseAndroidLayout = true

  private val onScanned by EventDispatcher()
  private val preview = PreviewView(context).apply { scaleType = PreviewView.ScaleType.FILL_CENTER }
  private var provider: ProcessCameraProvider? = null
  private var worker: ExecutorService? = null
  private var frame = ByteArray(0)
  private var lastSent = 0L

  init {
    setBackgroundColor(Color.BLACK)
    addView(preview, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.MATCH_PARENT))
  }

  override fun onAttachedToWindow() {
    super.onAttachedToWindow()
    start()
  }

  override fun onDetachedFromWindow() {
    stop()
    super.onDetachedFromWindow()
  }

  private fun start() {
    val owner = appContext.currentActivity as? LifecycleOwner ?: return
    if (ContextCompat.checkSelfPermission(context, Manifest.permission.CAMERA) != PackageManager.PERMISSION_GRANTED) return
    val thread = Executors.newSingleThreadExecutor()
    worker = thread
    val future = ProcessCameraProvider.getInstance(context)
    future.addListener({
      if (!isAttachedToWindow || worker !== thread) return@addListener
      try {
        val cameras = future.get()
        val show = Preview.Builder().build().also { it.surfaceProvider = preview.surfaceProvider }
        // More pixels than the default 640x480, so a small code on a large
        // monitor still has enough of them at an arm's length.
        val sizes = ResolutionSelector.Builder()
          .setResolutionStrategy(ResolutionStrategy(Size(1280, 720), ResolutionStrategy.FALLBACK_RULE_CLOSEST_HIGHER_THEN_LOWER))
          .build()
        val read = ImageAnalysis.Builder()
          .setResolutionSelector(sizes)
          .setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST)
          .build()
        read.setAnalyzer(thread, ::look)
        cameras.unbindAll()
        cameras.bindToLifecycle(owner, CameraSelector.DEFAULT_BACK_CAMERA, show, read)
        provider = cameras
      } catch (e: Exception) {
        Log.w(TAG, "the camera did not start", e)
      }
    }, ContextCompat.getMainExecutor(context))
  }

  private fun stop() {
    provider?.unbindAll()
    provider = null
    worker?.shutdown()
    worker = null
  }

  // Runs on the worker thread, one frame at a time.
  private fun look(image: ImageProxy) {
    try {
      val plane = image.planes[0]
      val bytes = plane.buffer
      val length = bytes.remaining()
      if (frame.size < length) frame = ByteArray(length)
      bytes.get(frame, 0, length)
      val text = QrDecoder.decode(frame, length, plane.rowStride, image.width, image.height) ?: return
      val now = SystemClock.elapsedRealtime()
      if (now - lastSent < REPEAT_MS) return
      lastSent = now
      post { if (isAttachedToWindow) onScanned(mapOf("data" to text)) }
    } catch (e: Exception) {
      Log.w(TAG, "a frame could not be read", e)
    } finally {
      image.close()
    }
  }
}
