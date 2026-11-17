// Photos for a question (#14, AskorusPhotoPicker), used through
// src/ask/photoPick.ts (and src/ask/photoSave.ts for savePicture, which
// puts a picture the app made into the phone's own pictures). Two ways in: Android's own photo picker (pick: the
// app sees only what the person picked, and needs no permission for it) and
// the phone's camera app (capture). Either way each picture is read once,
// turned upright, scaled down so its long edge is at most what the caller
// asked for, and written as a new JPEG into the app's cache. The new file
// has none of the original's details (where and when it was taken, the
// phone's model): only the pixels are carried over. The original is never
// changed, and nothing is sent anywhere from here.
package app.askorus.photopicker

import android.app.Activity
import android.content.ClipData
import android.content.ContentValues
import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Matrix
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.provider.MediaStore
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts.PickMultipleVisualMedia
import androidx.activity.result.contract.ActivityResultContracts.PickVisualMedia
import androidx.core.content.FileProvider
import androidx.exifinterface.media.ExifInterface
import expo.modules.kotlin.activityresult.AppContextActivityResultContract
import expo.modules.kotlin.activityresult.AppContextActivityResultLauncher
import expo.modules.kotlin.exception.CodedException
import expo.modules.kotlin.functions.Coroutine
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition
import java.io.File
import java.io.FileInputStream
import java.io.InputStream
import java.io.Serializable
import java.util.UUID
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

// The folders in the app's cache: where the camera app writes its picture
// (res/xml/askorus_photo_paths.xml names the same folder), and where the
// scaled-down copies wait until the app takes them.
private const val captureFolder = "photo-capture"
private const val pendingFolder = "photos-pending"
private const val providerSuffix = ".AskorusPhotoProvider"

// The most pictures one pick may bring back, whatever the caller asks.
private const val maxPick = 10

private class PhotoException(code: String, message: String, cause: Throwable? = null) : CodedException(code, message, cause)

internal data class PickInput(val limit: Int) : Serializable

// Opens the photo picker for pictures only and gives back what was picked,
// in the order picked, at most limit of them. Nothing when it was closed.
internal class PickContract : AppContextActivityResultContract<PickInput, List<Uri>> {
  override fun createIntent(context: Context, input: PickInput): Intent {
    val request = PickVisualMediaRequest.Builder().setMediaType(PickVisualMedia.ImageOnly).build()
    return if (input.limit > 1) {
      PickMultipleVisualMedia(input.limit).createIntent(context, request)
    } else {
      PickVisualMedia().createIntent(context, request)
    }
  }

  override fun parseResult(input: PickInput, resultCode: Int, intent: Intent?): List<Uri> {
    if (resultCode != Activity.RESULT_OK || intent == null) return emptyList()
    val picked = LinkedHashSet<Uri>()
    intent.data?.let { picked.add(it) }
    intent.clipData?.let { clip ->
      for (i in 0 until clip.itemCount) clip.getItemAt(i).uri?.let { picked.add(it) }
    }
    return picked.take(input.limit)
  }
}

internal data class CaptureInput(val uri: String) : Serializable

// Opens the phone's camera app, which writes the picture to uri (a file of
// the app's own cache, handed over for this one picture). True when a
// picture was taken.
internal class CaptureContract : AppContextActivityResultContract<CaptureInput, Boolean> {
  override fun createIntent(context: Context, input: CaptureInput): Intent {
    val uri = Uri.parse(input.uri)
    return Intent(MediaStore.ACTION_IMAGE_CAPTURE).apply {
      putExtra(MediaStore.EXTRA_OUTPUT, uri)
      clipData = ClipData.newRawUri("", uri)
      addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_WRITE_URI_PERMISSION)
    }
  }

  override fun parseResult(input: CaptureInput, resultCode: Int, intent: Intent?): Boolean = resultCode == Activity.RESULT_OK
}

class PhotoPickerModule : Module() {
  private lateinit var pickLauncher: AppContextActivityResultLauncher<PickInput, List<Uri>>
  private lateinit var captureLauncher: AppContextActivityResultLauncher<CaptureInput, Boolean>

  private val context: Context
    get() = appContext.reactContext?.applicationContext
      ?: throw PhotoException("photo_not_ready", "the app is not ready")

  override fun definition() = ModuleDefinition {
    Name("AskorusPhotoPicker")

    // pick(limit, edge, quality): the pictures the person picked, each as
    // {uri, width, height, bytes} of its scaled-down copy. [] when the
    // picker was closed; a picture that cannot be read is left out.
    AsyncFunction("pick") Coroutine { limit: Int, edge: Int, quality: Int ->
      val ctx = context
      val picked = try {
        pickLauncher.launch(PickInput(limit.coerceIn(1, maxPick)))
      } catch (e: Throwable) {
        throw PhotoException("photo_no_picker", "the photo picker could not be opened", e)
      }
      withContext(Dispatchers.IO) { picked.mapNotNull { prepare(ctx, it, edge, quality) } }
    }

    // capture(edge, quality): one picture from the phone's camera app, as
    // pick gives one; null when no picture was taken. The camera
    // permission must be allowed first (the app asks for it in JS).
    AsyncFunction("capture") Coroutine { edge: Int, quality: Int ->
      val ctx = context
      val folder = File(ctx.cacheDir, captureFolder).apply { mkdirs() }
      val shot = File(folder, "shot-${UUID.randomUUID()}.jpg")
      try {
        val uri = FileProvider.getUriForFile(ctx, ctx.packageName + providerSuffix, shot)
        val taken = try {
          captureLauncher.launch(CaptureInput(uri.toString()))
        } catch (e: SecurityException) {
          throw PhotoException("photo_camera_denied", "the camera is not allowed", e)
        } catch (e: Throwable) {
          throw PhotoException("photo_no_camera", "no camera app could be opened", e)
        }
        if (!taken || shot.length() == 0L) {
          null
        } else {
          withContext(Dispatchers.IO) { prepare(ctx, Uri.fromFile(shot), edge, quality) }
        }
      } finally {
        // The camera app's own file never stays: only the copy does.
        shot.delete()
      }
    }

    // savePicture(path, name): puts a picture of the app's own (a PNG or
    // JPEG in its cache or files) into the phone's pictures, in
    // Pictures/Askorus, under name. Android 10 and later, where MediaStore
    // takes a new picture without any permission; before that the app
    // would need the storage permission, which it does not ask for.
    AsyncFunction("savePicture") Coroutine { path: String, name: String ->
      val ctx = context
      withContext(Dispatchers.IO) { savePicture(ctx, path, name) }
    }

    RegisterActivityContracts {
      pickLauncher = registerForActivityResult(PickContract())
      captureLauncher = registerForActivityResult(CaptureContract())
    }
  }
}

// ownPicture is the .png or .jpg at path (a plain path or a file://
// address) when it lies in the app's cache or files folder, by its real
// path; else null.
private fun ownPicture(context: Context, path: String): File? {
  val plain = if (path.startsWith("file://")) Uri.parse(path).path ?: return null else path
  val file = try {
    File(plain).canonicalFile
  } catch (e: Throwable) {
    return null
  }
  val name = file.name.lowercase()
  if (!file.isFile || !(name.endsWith(".png") || name.endsWith(".jpg"))) return null
  for (root in listOf(context.cacheDir, context.filesDir)) {
    val base = try {
      root.canonicalPath
    } catch (e: Throwable) {
      continue
    }
    if (file.path.startsWith(base + File.separator)) return file
  }
  return null
}

private fun savePicture(context: Context, path: String, name: String) {
  if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) {
    throw PhotoException("photo_save_unsupported", "saving a picture needs Android 10 or later")
  }
  val file = ownPicture(context, path) ?: throw PhotoException("photo_save_failed", "not a picture of the app's own")
  val png = file.name.lowercase().endsWith(".png")
  // The name is the app's own; anything else in it becomes an underscore.
  val safe = name.replace(Regex("[^A-Za-z0-9._-]"), "_").take(80).ifEmpty { if (png) "askorus.png" else "askorus.jpg" }
  val resolver = context.contentResolver
  val values = ContentValues().apply {
    put(MediaStore.Images.Media.DISPLAY_NAME, safe)
    put(MediaStore.Images.Media.MIME_TYPE, if (png) "image/png" else "image/jpeg")
    put(MediaStore.Images.Media.RELATIVE_PATH, Environment.DIRECTORY_PICTURES + "/Askorus")
    put(MediaStore.Images.Media.IS_PENDING, 1)
  }
  val target = resolver.insert(MediaStore.Images.Media.getContentUri(MediaStore.VOLUME_EXTERNAL_PRIMARY), values)
    ?: throw PhotoException("photo_save_failed", "the phone's pictures took no new picture")
  try {
    val out = resolver.openOutputStream(target) ?: throw PhotoException("photo_save_failed", "the new picture cannot be written")
    out.use { o -> file.inputStream().use { it.copyTo(o) } }
    resolver.update(target, ContentValues().apply { put(MediaStore.Images.Media.IS_PENDING, 0) }, null, null)
  } catch (e: Throwable) {
    // Half a picture never stays.
    try {
      resolver.delete(target, null, null)
    } catch (ignored: Throwable) {
    }
    if (e is PhotoException) throw e
    throw PhotoException("photo_save_failed", "the picture could not be saved", e)
  }
}

private fun open(context: Context, source: Uri): InputStream? =
  if (source.scheme == "file") FileInputStream(File(requireNotNull(source.path))) else context.contentResolver.openInputStream(source)

// prepare reads the picture at source and writes its upright, scaled-down
// JPEG copy into the cache. It gives the copy as the map JS gets, or null
// when the picture cannot be read (not a picture, too large for memory).
private fun prepare(context: Context, source: Uri, askedEdge: Int, askedQuality: Int): Map<String, Any>? {
  val edge = PhotoSize.edge(askedEdge)
  val quality = PhotoSize.quality(askedQuality)
  var decoded: Bitmap? = null
  var upright: Bitmap? = null
  var flat: Bitmap? = null
  var out: File? = null
  try {
    val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
    open(context, source)?.use { BitmapFactory.decodeStream(it, null, bounds) }
    if (bounds.outWidth <= 0 || bounds.outHeight <= 0) return null

    val orientation = open(context, source)?.use {
      ExifInterface(it).getAttributeInt(ExifInterface.TAG_ORIENTATION, ExifInterface.ORIENTATION_NORMAL)
    } ?: ExifInterface.ORIENTATION_NORMAL

    val options = BitmapFactory.Options().apply { inSampleSize = PhotoSize.sample(bounds.outWidth, bounds.outHeight, edge) }
    decoded = open(context, source)?.use { BitmapFactory.decodeStream(it, null, options) } ?: return null

    val (width, height) = PhotoSize.target(decoded.width, decoded.height, edge)
    val matrix = Matrix()
    matrix.postScale(width.toFloat() / decoded.width, height.toFloat() / decoded.height)
    when (orientation) {
      ExifInterface.ORIENTATION_ROTATE_90 -> matrix.postRotate(90f)
      ExifInterface.ORIENTATION_ROTATE_180 -> matrix.postRotate(180f)
      ExifInterface.ORIENTATION_ROTATE_270 -> matrix.postRotate(270f)
      ExifInterface.ORIENTATION_FLIP_HORIZONTAL -> matrix.postScale(-1f, 1f)
      ExifInterface.ORIENTATION_FLIP_VERTICAL -> matrix.postScale(1f, -1f)
      ExifInterface.ORIENTATION_TRANSPOSE -> {
        matrix.postRotate(90f)
        matrix.postScale(-1f, 1f)
      }
      ExifInterface.ORIENTATION_TRANSVERSE -> {
        matrix.postRotate(270f)
        matrix.postScale(-1f, 1f)
      }
    }
    upright = Bitmap.createBitmap(decoded, 0, 0, decoded.width, decoded.height, matrix, true)

    // JPEG has no see-through pixels: they would come out black. A picture
    // that has them (a PNG cut-out) is laid on white first.
    var result: Bitmap = upright
    if (upright.hasAlpha()) {
      flat = Bitmap.createBitmap(upright.width, upright.height, Bitmap.Config.ARGB_8888)
      Canvas(flat).apply {
        drawColor(Color.WHITE)
        drawBitmap(upright, 0f, 0f, null)
      }
      result = flat
    }

    val folder = File(context.cacheDir, pendingFolder).apply { mkdirs() }
    val file = File(folder, "${UUID.randomUUID()}.jpg")
    out = file
    val written = file.outputStream().use { result.compress(Bitmap.CompressFormat.JPEG, quality, it) }
    if (!written || file.length() == 0L) {
      file.delete()
      return null
    }
    out = null
    return mapOf(
      "uri" to Uri.fromFile(file).toString(),
      "width" to result.width,
      "height" to result.height,
      "bytes" to file.length().toInt()
    )
  } catch (e: Throwable) {
    // A picture that cannot be read, or one too large for the phone's
    // memory (OutOfMemoryError is no Exception), is left out.
    return null
  } finally {
    out?.delete()
    flat?.recycle()
    if (upright !== decoded) upright?.recycle()
    decoded?.recycle()
  }
}
