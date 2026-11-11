// Reads a QR code out of one camera frame, on the phone (ZXing, plain Java).
// Kept apart from the camera so a JVM test can feed it a drawn code.
package app.askorus.qrscanner

import com.google.zxing.BinaryBitmap
import com.google.zxing.DecodeHintType
import com.google.zxing.LuminanceSource
import com.google.zxing.PlanarYUVLuminanceSource
import com.google.zxing.common.HybridBinarizer
import com.google.zxing.qrcode.QRCodeReader

object QrDecoder {
  private val hints = mapOf(DecodeHintType.TRY_HARDER to true)

  // luma: the frame's brightness plane (Y of YUV_420_888), one byte a pixel,
  // rows rowStride bytes apart (a camera may pad each row past width).
  // length: how many bytes of luma belong to this frame. The answer is the
  // code's text, or null when the frame shows no readable QR code. A code on
  // a dark terminal (light squares on black) is read too.
  fun decode(luma: ByteArray, length: Int, rowStride: Int, width: Int, height: Int): String? {
    if (width <= 0 || height <= 0 || rowStride < width) return null
    if (length > luma.size || length < rowStride.toLong() * (height - 1) + width) return null
    val source = PlanarYUVLuminanceSource(luma, rowStride, height, 0, 0, width, height, false)
    return read(source) ?: read(source.invert())
  }

  private fun read(source: LuminanceSource): String? =
    try {
      QRCodeReader().decode(BinaryBitmap(HybridBinarizer(source)), hints).text
    } catch (e: Exception) {
      // Not found, or a damaged code (checksum, format): the next frame tries again.
      null
    }
}
