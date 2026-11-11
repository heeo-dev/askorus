// The app's own QR scanner view (AskorusQrScanner), used by the pairing
// screen through src/ui/QrScanner.tsx. See QrScannerView.
package app.askorus.qrscanner

import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition

class QrScannerModule : Module() {
  override fun definition() = ModuleDefinition {
    Name("AskorusQrScanner")

    View(QrScannerView::class) {
      Events("onScanned")
    }
  }
}
