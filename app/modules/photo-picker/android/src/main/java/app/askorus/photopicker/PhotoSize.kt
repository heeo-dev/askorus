// The sizes a photo goes through on its way down. Plain numbers, so the
// JVM test (PhotoSizeTest) checks them without a phone.
package app.askorus.photopicker

object PhotoSize {
  // The largest long edge a photo may ask for, whatever the caller says.
  const val MAX_EDGE = 4096

  /** The long edge asked for, kept between 64 and MAX_EDGE. */
  fun edge(asked: Int): Int = asked.coerceIn(64, MAX_EDGE)

  /** The JPEG quality asked for (1 to 100), kept between 40 and 95. */
  fun quality(asked: Int): Int = asked.coerceIn(40, 95)

  /**
   * How much the decoder may shrink a width x height picture while reading
   * it (a power of two), so that its long edge stays at least edge: the
   * rest of the way down is scaled exactly.
   */
  fun sample(width: Int, height: Int, edge: Int): Int {
    var sample = 1
    val long = maxOf(width, height)
    while (long / (sample * 2) >= edge) sample *= 2
    return sample
  }

  /**
   * The size a width x height picture is scaled to: its long edge at most
   * edge, its shape kept, never scaled up, never under one pixel.
   */
  fun target(width: Int, height: Int, edge: Int): Pair<Int, Int> {
    val long = maxOf(width, height)
    if (long <= edge) return width to height
    val k = edge.toDouble() / long
    return maxOf(1, Math.round(width * k).toInt()) to maxOf(1, Math.round(height * k).toInt())
  }
}
