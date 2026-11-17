// The numbers a photo is scaled by.
// Run: in app/android, gradlew :photo-picker:testDebugUnitTest
package app.askorus.photopicker

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class PhotoSizeTest {
  @Test
  fun aPhotoIsScaledToItsLongEdge() {
    assertEquals(2048 to 1536, PhotoSize.target(4000, 3000, 2048))
    assertEquals(1536 to 2048, PhotoSize.target(3000, 4000, 2048))
    assertEquals(2048 to 922, PhotoSize.target(4000, 1800, 2048))
    assertEquals(2048 to 2048, PhotoSize.target(5000, 5000, 2048))
  }

  @Test
  fun aSmallPhotoKeepsItsSize() {
    assertEquals(1080 to 2340, PhotoSize.target(1080, 2340, 2400))
    assertEquals(2048 to 100, PhotoSize.target(2048, 100, 2048))
    assertEquals(1 to 1, PhotoSize.target(1, 1, 2048))
  }

  @Test
  fun aVeryLongPhotoKeepsAtLeastOnePixel() {
    assertEquals(2048 to 1, PhotoSize.target(100000, 10, 2048))
  }

  @Test
  fun theDecoderNeverShrinksBelowTheEdge() {
    assertEquals(1, PhotoSize.sample(4000, 3000, 2048)) // half would be 2000, under the edge
    assertEquals(2, PhotoSize.sample(4096, 3072, 2048))
    assertEquals(2, PhotoSize.sample(8000, 6000, 2048)) // a quarter would be 2000
    assertEquals(4, PhotoSize.sample(9000, 12000, 2048))
    assertEquals(1, PhotoSize.sample(800, 600, 2048))
    for ((w, h) in listOf(4000 to 3000, 12000 to 9000, 2049 to 10, 50000 to 50000)) {
      val s = PhotoSize.sample(w, h, 2048)
      assertTrue("$w x $h by $s", maxOf(w, h) / s >= 2048)
    }
  }

  @Test
  fun whatTheCallerAsksIsKeptInBounds() {
    assertEquals(64, PhotoSize.edge(0))
    assertEquals(2048, PhotoSize.edge(2048))
    assertEquals(PhotoSize.MAX_EDGE, PhotoSize.edge(1_000_000))
    assertEquals(40, PhotoSize.quality(-5))
    assertEquals(80, PhotoSize.quality(80))
    assertEquals(95, PhotoSize.quality(100))
  }
}
