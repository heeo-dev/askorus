// Asksemble test page. Everything is rendered with textContent; nothing
// from the server is ever parsed as HTML.
"use strict";

const $ = (id) => document.getElementById(id);
const KEY = "asksemble-test-token";

const stateNames = { running: "실행 중", stopped: "멈춤", terminal: "터미널", history: "기록만", unknown: "알 수 없음" };
const errorNames = {
  pair_closed: "PC에 열린 짝 맺기 창이 없습니다. PC에서 pair를 다시 실행하세요.",
  pair_bad_code: "코드가 틀렸습니다.",
  pair_busy: "다른 기기가 짝 맺는 중입니다.",
  pair_denied: "PC에서 거절했습니다.",
  pair_timeout: "PC에서 2분 안에 답이 없었습니다.",
  pair_not_tailnet: "Tailscale을 거쳐 접속해야 합니다.",
  wrong_account: "PC 주인과 다른 Tailscale 계정입니다.",
  owner_unknown: "PC가 주인 Tailscale 계정을 모릅니다.",
  too_many_attempts: "실패가 너무 많습니다. 10분 뒤에 다시 하세요.",
  unauthorized: "토큰이 없거나 끊겼습니다. 다시 짝 맺으세요.",
};

function token() {
  try { return sessionStorage.getItem(KEY) || ""; } catch { return ""; }
}
function setToken(t) {
  try { t ? sessionStorage.setItem(KEY, t) : sessionStorage.removeItem(KEY); } catch {}
}

function say(el, text, isError) {
  el.textContent = text;
  el.classList.toggle("err", !!isError);
}

async function call(method, path, body) {
  const headers = {};
  if (token()) headers["Authorization"] = "Bearer " + token();
  if (body) headers["Content-Type"] = "application/json";
  const res = await fetch("/v1/" + path, { method, headers, body: body ? JSON.stringify(body) : undefined, cache: "no-store" });
  let data = null;
  try { data = await res.json(); } catch {}
  if (!res.ok) {
    const code = data && data.error && data.error.code;
    throw new Error(errorNames[code] || ("오류 " + res.status + (code ? " (" + code + ")" : "")));
  }
  return data;
}

function showUse() {
  $("pair").hidden = !!token();
  $("use").hidden = !token();
}

// The code arrives in the URL fragment, which is never sent to a server.
const hash = new URLSearchParams(location.hash.slice(1));
if (hash.get("code")) {
  $("code").value = hash.get("code");
  history.replaceState(null, "", location.pathname);
}

$("pairBtn").addEventListener("click", async () => {
  const btn = $("pairBtn");
  btn.disabled = true;
  say($("pairMsg"), "PC에서 허용을 기다리는 중… (최대 2분)");
  try {
    const r = await call("POST", "pair", { code: $("code").value.replace(/[-\s]/g, ""), deviceName: $("name").value });
    setToken(r.token);
    say($("who"), "짝 맺음: " + r.deviceName + " (id " + r.deviceId + "). 토큰은 이 탭에만 저장됩니다.");
    say($("pairMsg"), "");
    showUse();
  } catch (e) {
    say($("pairMsg"), e.message, true);
  } finally {
    btn.disabled = false;
  }
});

$("infoBtn").addEventListener("click", async () => {
  say($("useMsg"), "");
  try {
    const r = await call("GET", "info");
    $("info").hidden = false;
    $("info").textContent = JSON.stringify(r, null, 2);
  } catch (e) {
    say($("useMsg"), e.message, true);
  }
});

function when(iso) {
  const d = new Date(iso);
  return isNaN(d) ? "" : d.toLocaleString("ko-KR", { dateStyle: "short", timeStyle: "short" });
}

$("listBtn").addEventListener("click", async () => {
  const q = new URLSearchParams();
  if ($("state").value) q.set("state", $("state").value);
  if ($("q").value.trim()) q.set("q", $("q").value.trim());
  say($("useMsg"), "불러오는 중…");
  const started = performance.now();
  try {
    const r = await call("GET", "sessions?" + q.toString());
    const list = $("list");
    list.replaceChildren();
    for (const s of r.sessions) {
      const li = document.createElement("li");
      const title = document.createElement("div");
      title.className = "title";
      title.textContent = s.title || s.lastPrompt || "(제목 없음)";
      const meta = document.createElement("div");
      meta.className = "meta";
      meta.textContent = [stateNames[s.state] || s.state, s.activity, when(s.lastActive), s.folder].filter(Boolean).join(" · ");
      li.append(title, meta);
      if (s.remoteUrl && s.remoteUrl.startsWith("https://claude.ai/code/session_")) {
        const a = document.createElement("a");
        a.href = s.remoteUrl;
        a.textContent = "Claude 앱에서 열기";
        li.append(a);
      }
      if (s.attachCommand) {
        const cmd = document.createElement("div");
        cmd.className = "meta";
        cmd.textContent = "PC: " + s.attachCommand;
        li.append(cmd);
      }
      list.append(li);
    }
    const ms = Math.round(performance.now() - started);
    say($("useMsg"), r.sessions.length + "개, " + ms + "ms" + (r.warnings.length ? " · 경고: " + r.warnings.join(", ") : ""));
  } catch (e) {
    say($("useMsg"), e.message, true);
  }
});

$("forgetBtn").addEventListener("click", () => {
  setToken("");
  $("list").replaceChildren();
  $("info").hidden = true;
  showUse();
});

showUse();
