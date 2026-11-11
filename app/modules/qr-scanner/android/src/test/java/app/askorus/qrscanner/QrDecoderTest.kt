// Draws QR codes the way a camera frame holds them (one brightness byte a
// pixel, rows padded past the width) and reads them back.
// Run: in app/android, gradlew :qr-scanner:testDebugUnitTest
package app.askorus.qrscanner

import com.google.zxing.BarcodeFormat
import com.google.zxing.qrcode.QRCodeWriter
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

private const val LINK = "askorus://pair?v=1&host=my-pc.tail0123ab.ts.net&port=8444&code=ABCDEFGHIJKLMNOPQRSTUVWXYZ"

class QrDecoderTest {
  // A frame of width x height with the code drawn at (left, top), size px
  // wide; dark is the brightness of the code's dark squares.
  private fun frame(text: String, width: Int, height: Int, rowStride: Int, left: Int, top: Int, size: Int, dark: Int, light: Int): ByteArray {
    val code = QRCodeWriter().encode(text, BarcodeFormat.QR_CODE, size, size)
    val luma = ByteArray(rowStride * (height - 1) + width) { light.toByte() }
    for (y in 0 until size) for (x in 0 until size) {
      if (code.get(x, y)) luma[(top + y) * rowStride + left + x] = dark.toByte()
    }
    return luma
  }

  @Test
  fun readsThePairingLink() {
    val luma = frame(LINK, 1280, 720, 1280, 400, 150, 400, 20, 235)
    assertEquals(LINK, QrDecoder.decode(luma, luma.size, 1280, 1280, 720))
  }

  @Test
  fun readsAFrameWhoseRowsArePadded() {
    val luma = frame(LINK, 1280, 720, 1344, 60, 40, 500, 30, 220)
    assertEquals(LINK, QrDecoder.decode(luma, luma.size, 1344, 1280, 720))
  }

  @Test
  fun readsALightCodeOnADarkTerminal() {
    val luma = frame(LINK, 1280, 720, 1280, 300, 100, 450, 230, 15)
    assertEquals(LINK, QrDecoder.decode(luma, luma.size, 1280, 1280, 720))
  }

  @Test
  fun readsFromABufferLongerThanTheFrame() {
    val one = frame(LINK, 640, 480, 640, 100, 60, 320, 20, 235)
    val reused = one.copyOf(one.size + 5000)
    assertEquals(LINK, QrDecoder.decode(reused, one.size, 640, 640, 480))
  }

  @Test
  fun aFrameWithoutACodeIsNull() {
    val luma = ByteArray(640 * 480) { ((it * 31) % 251).toByte() }
    assertNull(QrDecoder.decode(luma, luma.size, 640, 640, 480))
  }

  @Test
  fun sizesThatDoNotFitAreNullNotACrash() {
    val luma = ByteArray(100)
    assertNull(QrDecoder.decode(luma, luma.size, 640, 640, 480))
    assertNull(QrDecoder.decode(luma, 200, 10, 10, 10))
    assertNull(QrDecoder.decode(luma, luma.size, 5, 10, 10))
    assertNull(QrDecoder.decode(luma, luma.size, 10, 0, 10))
  }
}
