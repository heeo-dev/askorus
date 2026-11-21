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

// ownFile is the file at path (a plain path or a file:// address) when it
// lies in the app's cache or files folder and outside the node's folder,
// by its real path; else null.
private fun ownFile(context: Context, path: String): File? {
  val plain = if (path.startsWith("file://")) android.net.Uri.parse(path).path ?: return null else path
  val file = try {
    File(plain).canonicalFile
  } catch (e: Throwable) {
    return null
  }
  if (!file.isFile) return null
  fun inside(dir: File?): Boolean {
    val root = try {
      dir?.canonicalPath ?: return false
    } catch (e: Throwable) {
      return false
    }
    return file.path.startsWith(root + File.separator)
  }
  if (inside(File(context.filesDir, "tailnet"))) return null
  return if (inside(context.cacheDir) || inside(context.filesDir)) file else null
}

// newOwnFile is the place fetchFile may write (#14): a path in the app's
// cache or files folder, never the node's own folder, whose folder exists
// and where nothing is yet.
private fun newOwnFile(context: Context, path: String): File? {
  val plain = if (path.startsWith("file://")) android.net.Uri.parse(path).path ?: return null else path
  val given = File(plain)
  if (!given.isAbsolute || given.exists()) return null
  val parent = try {
    given.parentFile?.canonicalFile ?: return null
  } catch (e: Throwable) {
    return null
  }
  if (!parent.isDirectory) return null
  fun inside(dir: File?): Boolean {
    val root = try {
      dir?.canonicalPath ?: return false
    } catch (e: Throwable) {
      return false
    }
    return parent.path == root || parent.path.startsWith(root + File.separator)
  }
  if (inside(File(context.filesDir, "tailnet"))) return null
  return if (inside(context.cacheDir) || inside(context.filesDir)) File(parent, given.name) else null
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
    // The other devices of the person's own account, as JSON (#58).
    AsyncFunction("peers") Coroutine { -> call { Tsbridge.peers() } }

    AsyncFunction("fetch") Coroutine { method: String, url: String, headers: String, body: String, timeoutMs: Int ->
      call {
        val r = Tsbridge.fetch(method, url, headers, body, timeoutMs.toLong())
        mapOf("status" to r.status.toInt(), "headers" to r.headers, "body" to r.body)
      }
    }

    // fetch with a file of the app's own as the body (#14: a photo that
    // goes with a question). Only a file in the app's cache or files folder
    // is sent, never one of the node's own folder (its key is there); the
    // library checks that again, and sends it only to a device of the
    // person's own Tailscale account.
    AsyncFunction("sendFile") Coroutine { method: String, url: String, headers: String, path: String, timeoutMs: Int ->
      val context = appContext.reactContext?.applicationContext
        ?: throw TailnetException("tailnet_off", "the app is not ready", null)
      call {
        val file = ownFile(context, path)
          ?: throw TailnetException("tailnet_bad_request", "not a file of the app's own", null)
        val r = Tsbridge.sendFile(method, url, headers, file.path, timeoutMs.toLong())
        mapOf("status" to r.status.toInt(), "headers" to r.headers, "body" to r.body)
      }
    }

    // fetch whose answer, when it is 2xx, goes into a new file of the app's
    // own (#14: a picture an AI made); body is then empty. Only a path in
    // the app's cache or files folder where nothing is yet, never the
    // node's own folder; the library checks that again, writes over
    // nothing, keeps at most 16 MiB, and fetches only from a device of the
    // person's own Tailscale account.
    AsyncFunction("fetchFile") Coroutine { method: String, url: String, headers: String, path: String, timeoutMs: Int ->
      val context = appContext.reactContext?.applicationContext
        ?: throw TailnetException("tailnet_off", "the app is not ready", null)
      call {
        val file = newOwnFile(context, path)
          ?: throw TailnetException("tailnet_bad_request", "not a new file of the app's own", null)
        val r = Tsbridge.fetchFile(method, url, headers, file.path, timeoutMs.toLong())
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
