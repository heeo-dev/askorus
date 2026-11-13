package app.askorus.tailnet

import android.content.Context
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import app.askorus.tailnet.tsbridge.Tsbridge
import expo.modules.kotlin.exception.CodedException
import expo.modules.kotlin.functions.Coroutine
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition
import go.Seq
import java.io.File
import java.util.concurrent.atomic.AtomicBoolean
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

// The app's own device on the person's Tailscale network (#57). Tailscale
// runs inside the app (tsbridge/, a Go library): there is no VPN, no VPN
// permission, and nothing of it reaches other apps. The app's requests to
// the PC go through fetch below and nowhere else; the library itself sends
// them only to a device of the person's own Tailscale account.
//
// Every call into the library blocks, some for as long as a request takes,
// so each runs on the IO dispatcher: one slow request never holds up the
// others or the other native modules.
//
// An error of the library starts with a code ("tailnet_login: ..."); it is
// handed on as the JavaScript error's code. Anything else is the network's.

private const val networkCode = "tailnet_network"

private class TailnetException(code: String, message: String, cause: Throwable?) : CodedException(code, message, cause)

private fun coded(e: Throwable): CodedException {
  val text = e.message ?: ""
  val code = text.substringBefore(": ", "")
  return if (code.startsWith("tailnet_")) {
    TailnetException(code, text.substringAfter(": "), e)
  } else {
    TailnetException(networkCode, text, e)
  }
}

private suspend fun <T> call(block: () -> T): T = withContext(Dispatchers.IO) {
  try {
    block()
  } catch (e: CodedException) {
    throw e
  } catch (e: Throwable) {
    throw coded(e)
  }
}

class TailnetModule : Module() {
  private val watching = AtomicBoolean(false)

  override fun definition() = ModuleDefinition {
    Name("AskorusTailnet")

    // Starts the node (at once; it connects in the background). The node's
    // key stays in a folder of the app's own, so the next start needs no
    // login. Calling it again does nothing.
    AsyncFunction("start") Coroutine { hostname: String ->
      val context = appContext.reactContext?.applicationContext
        ?: throw TailnetException("tailnet_off", "the app is not ready", null)
      call {
        Seq.setContext(context)
        Tsbridge.start(File(context.filesDir, "tailnet").absolutePath, hostname)
      }
      watchNetwork(context)
    }

    AsyncFunction("stop") Coroutine { -> call { Tsbridge.stop() } }

    // JSON: {state, authURL, name, owner, keyExpiry, ips, health}.
    AsyncFunction("status") Coroutine { -> call { Tsbridge.status() } }

    AsyncFunction("login") Coroutine { -> call { Tsbridge.login() } }

    AsyncFunction("logout") Coroutine { -> call { Tsbridge.logout() } }

    // JSON: {known, sameUser, online, name, ip} for a MagicDNS name.
    AsyncFunction("peer") Coroutine { host: String -> call { Tsbridge.peer(host) } }

    AsyncFunction("fetch") Coroutine { method: String, url: String, headers: String, body: String, timeoutMs: Int ->
      call {
        val r = Tsbridge.fetch(method, url, headers, body, timeoutMs.toLong())
        mapOf("status" to r.status.toInt(), "headers" to r.headers, "body" to r.body)
      }
    }

    AsyncFunction("recentLog") Coroutine { -> call { Tsbridge.recentLog() } }

    // Back in front after sleep or another app: the network may have
    // changed while the process was frozen.
    OnActivityEntersForeground {
      changed()
    }
  }

  private fun changed() {
    try {
      Tsbridge.networkChanged()
    } catch (e: Throwable) {
      // The node is not started: nothing to tell.
    }
  }

  // The node looks at the network only every ten minutes on Android (to
  // save battery) and counts on the app to say when it changes.
  private fun watchNetwork(context: Context) {
    if (!watching.compareAndSet(false, true)) return
    try {
      val cm = context.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
      cm.registerDefaultNetworkCallback(object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: Network) = changed()
        override fun onLost(network: Network) = changed()
        override fun onLinkPropertiesChanged(network: Network, properties: LinkProperties) = changed()
      })
    } catch (e: Throwable) {
      watching.set(false)
    }
  }
}
