package app.askorus.tailnetcheck

import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition
import java.net.DatagramSocket
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.NetworkInterface
import java.net.UnknownHostException

// What the phone's own network says about a PC's name (#42). Before the app
// sends a pairing code it checks that the name is a device inside the
// person's tailnet: a pairing link someone else sent could name a server
// that Tailscale Funnel puts on the internet, and that server would "allow"
// the pairing itself and get the questions (R-94).
//
// JavaScript's fetch does not tell which address it connected to, so this
// asks the system: the addresses the name resolves to, and for each one the
// phone's own address and interface on the route to it. Connecting a UDP
// socket sends nothing; it only makes the system pick the route. The app
// decides from the answer (src/connections/tailnetCheck.ts). Needs no
// permission beyond INTERNET.

private fun plain(address: InetAddress): String = (address.hostAddress ?: "").substringBefore('%')

class TailnetCheckModule : Module() {
  override fun definition() = ModuleDefinition {
    Name("AskorusTailnetCheck")

    // Runs on the modules' background queue: name resolution may take seconds.
    AsyncFunction("lookup") { host: String, port: Int ->
      val addresses: Array<InetAddress> = try {
        InetAddress.getAllByName(host)
      } catch (e: UnknownHostException) {
        emptyArray()
      }
      addresses.map { address ->
        var local = ""
        var via = ""
        try {
          DatagramSocket().use { socket ->
            socket.connect(InetSocketAddress(address, port))
            val from = socket.localAddress
            if (from != null && !from.isAnyLocalAddress) {
              local = plain(from)
              via = NetworkInterface.getByInetAddress(from)?.name ?: ""
            }
          }
        } catch (e: Exception) {
          // No route, or the interface cannot be read: left empty.
        }
        mapOf("address" to plain(address), "local" to local, "via" to via)
      }
    }
  }
}
