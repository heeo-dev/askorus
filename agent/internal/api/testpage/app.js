// Asksemble test page. Everything is rendered with textContent; nothing
// from the server is ever parsed as HTML.
"use strict";

const $ = (id) => document.getElementById(id);
const KEY = "asksemble-test-token";

const badgeNames = { server: "폴더 서버", desktop: "데스크톱 앱", rc: "원격 켜짐" };
const stateNames = { running: "실행 중", stopped: "멈춤", terminal: "터미널", history: "기록만", unknown: "알 수 없음" };
const errorNames = {
  pair_closed: "PC에 열린 짝 맺기 창이 없습니다. PC에서 pair를 다시 실행하세요.",
  pair_bad_code: "코드가 틀렸습니다.",
  pair_busy: "다른 기기가 짝 맺는 중입니다.",
  pair_denied: "PC에서 거절했습니다.",
  pair_timeout: "PC에서 2분 안에 답이 없었습니다.",
  pair_unknown: "짝 맺기 요청이 끝났거나 사라졌습니다. PC에서 pair를 다시 실행하세요.",
  pair_not_tailnet: "Tailscale을 거쳐 접속해야 합니다.",
  wrong_account: "PC 주인과 다른 Tailscale 계정입니다.",
  owner_unknown: "PC가 주인 Tailscale 계정을 모릅니다.",
  too_many_attempts: "실패가 너무 많습니다. 10분 뒤에 다시 하세요.",
  unauthorized: "토큰이 없거나 끊겼습니다. 다시 짝 맺으세요.",
  session_not_found: "세션을 찾지 못했습니다.",
  session_busy: "이 세션에 다른 동작이 진행 중입니다.",
  session_not_background: "백그라운드 세션만 멈출 수 있습니다.",
  session_state_unknown: "PC에서 세션 상태를 읽지 못했습니다.",
  workspace_not_trusted: "PC에서 아직 신뢰하지 않은 폴더입니다. 그 폴더에서 claude를 한 번 실행해 신뢰를 허용하세요.",
  remote_link_timeout: "세션은 떴지만 원격 링크가 생기지 않았습니다.",
  launch_failed: "PC에서 claude 실행이 실패했습니다.",
  folder_not_found: "허용된 폴더가 아닙니다.",
};

const REQ = "asksemble-test-request";

// localStorage, not sessionStorage: opening a session hands off to the
// Claude app, and coming back can land in a new tab that would otherwise
// have lost the token. "토큰 지우기" removes it.
function load(k) {
  try { return localStorage.getItem(k) || ""; } catch { return ""; }
}
function store(k, v) {
  try { v ? localStorage.setItem(k, v) : localStorage.removeItem(k); } catch {}
}
const token = () => load(KEY);
const setToken = (t) => store(KEY, t);
const DEV = "asksemble-test-device";

function showWho() {
  if (load(DEV)) {
    say($("who"), "짝 맺음: " + load(DEV) + ". 토큰은 이 브라우저에 저장됩니다. 다 쓰면 '토큰 지우기'를 누르세요.");
  }
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
    if (code === "unauthorized" && token()) {
      // revoked on the PC: forget the token and show pairing again
      setToken("");
      store(DEV, "");
      showUse();
    }
    throw new Error(errorNames[code] || ("오류 " + res.status + (code ? " (" + code + ")" : "")));
  }
  return data;
}

function showUse() {
  $("pair").hidden = !!token();
  $("use").hidden = !token();
  if (token() && !$("folder").value) { loadFolders(); loadServe(); }
}

// The code arrives in the URL fragment, which is never sent to a server.
const hash = new URLSearchParams(location.hash.slice(1));
if (hash.get("code")) {
  $("code").value = hash.get("code");
  history.replaceState(null, "", location.pathname);
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// waitForPC asks the agent until the person at the PC has answered. The
// request id survives a reload, so leaving the browser does not lose it.
async function waitForPC(requestId) {
  const btn = $("pairBtn");
  btn.disabled = true;
  say($("pairMsg"), "PC에서 허용을 기다리는 중… (최대 2분, PC 창에서 y를 누르세요)");
  try {
    for (;;) {
      const r = await call("POST", "pair/status", { requestId });
      if (r.status === "approved") {
        store(REQ, "");
        setToken(r.token);
        store(DEV, r.deviceName + " (id " + r.deviceId + ")");
        showWho();
        say($("pairMsg"), "");
        showUse();
        return;
      }
      await sleep(r.pollAfterMs || 1000);
    }
  } catch (e) {
    store(REQ, "");
    say($("pairMsg"), e.message, true);
  } finally {
    btn.disabled = false;
  }
}

$("pairBtn").addEventListener("click", async () => {
  say($("pairMsg"), "보내는 중…");
  try {
    const r = await call("POST", "pair", { code: $("code").value.replace(/[-\s]/g, ""), deviceName: $("name").value });
    store(REQ, r.requestId);
    await waitForPC(r.requestId);
  } catch (e) {
    say($("pairMsg"), e.message, true);
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

// claudeLink opens a session in the Claude app. Some Android browsers
// (Samsung Internet) keep https app links in a tab, so on Android the link
// names the app package and falls back to the web page without the app.
const CLAUDE_ANDROID = "com.anthropic.claude";
function claudeLink(url) {
  if (!/Android/i.test(navigator.userAgent)) return url;
  const u = new URL(url);
  return "intent://" + u.host + u.pathname + u.search +
    "#Intent;scheme=https;package=" + CLAUDE_ANDROID +
    ";S.browser_fallback_url=" + encodeURIComponent(url) + ";end";
}

function when(iso) {
  const d = new Date(iso);
  return isNaN(d) ? "" : d.toLocaleString("ko-KR", { dateStyle: "short", timeStyle: "short" });
}

// runJob sends a slow action (open, new session). The agent answers at
// once with a job id; this asks for the result every second. A dropped
// connection while asking is retried, and the job id is kept so a reload
// picks the wait up again.
const JOB = "asksemble-test-job";
async function runJob(method, path, body, label) {
  const started = await call(method, path, body);
  store(JOB, JSON.stringify({ id: started.jobId, label }));
  return pollJob(started.jobId, started.pollAfterMs);
}

async function pollJob(id, every) {
  let misses = 0;
  try {
    for (;;) {
      await sleep(every || 1000);
      let st;
      try {
        st = await call("GET", "jobs/" + id);
      } catch (e) {
        if (e instanceof TypeError && misses++ < 30) continue; // network blip: ask again
        throw e;
      }
      misses = 0;
      if (st.status === "done") return st.result;
      if (st.status === "failed") {
        const code = st.error && st.error.code;
        throw new Error((errorNames[code] || code || "실패") + (st.error && st.error.message ? " (" + st.error.message + ")" : ""));
      }
      every = st.pollAfterMs || every;
    }
  } finally {
    store(JOB, "");
  }
}

function showResult(target, links, r) {
  if (r.hint === "use_rc_in_terminal") {
    say(target, "PC 터미널에서 열려 있는 세션입니다. 그 터미널에 /rc를 입력하세요.");
    return;
  }
  if (r.hint === "server_session") {
    say(target, "폴더 서버가 돌리는 세션입니다. Claude 앱의 PC 목록에서 이 폴더를 골라 여세요.");
    return;
  }
  const what = { none: "이미 열려 있음", resumed: "깨움", started: "시작함" }[r.action] || r.action;
  say(target, what + ": " + ((r.session && (r.session.title || r.session.name)) || ""));
  if (r.remoteUrl) links.replaceChildren(remoteLink(r.remoteUrl));
}

// A job that was still running when the page was left or reloaded.
async function resumeJob() {
  let job;
  try { job = JSON.parse(load(JOB) || "null"); } catch { job = null; }
  if (!job || !token()) return;
  say($("useMsg"), "이전 작업(" + job.label + ")의 결과를 기다리는 중…");
  try {
    const r = await pollJob(job.id, 500);
    showResult($("useMsg"), $("resumeLink"), r);
  } catch (e) {
    say($("useMsg"), e.message, true);
  }
}

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text) e.textContent = text;
  return e;
}

// remoteLink is the tap target that hands the session to the Claude app.
// Opening it right after an awaited call would lose the user gesture, so
// the person taps it.
function remoteLink(url) {
  const a = el("a", "", "Claude 앱에서 열기");
  a.href = claudeLink(url);
  if (!a.href.startsWith("intent:")) {
    a.target = "_blank"; // keep this page in its own tab
  }
  a.rel = "noopener noreferrer";
  return a;
}

function renderSession(s) {
  const li = el("li");
  li.append(el("div", "title", s.title || s.lastPrompt || "(제목 없음)"));
  const badges = (s.badges || []).map((b) => "[" + (badgeNames[b] || b) + "]").join(" ");
  li.append(el("div", "meta", [badges, stateNames[s.state] || s.state, s.activity, when(s.lastActive), s.folder].filter(Boolean).join(" · ")));
  const msg = el("div", "msg");
  const links = el("div", "row");
  const buttons = el("div", "row");
  const act = (label, cls, fn) => {
    const b = el("button", cls, label);
    b.addEventListener("click", async () => {
      for (const x of buttons.children) x.disabled = true;
      try { await fn(); } catch (e) { say(msg, e.message, true); }
      for (const x of buttons.children) x.disabled = false;
    });
    buttons.append(b);
  };
  const path = "sessions/" + encodeURIComponent(s.id);
  act("열기", "", async () => {
    say(msg, "켜는 중… (보통 10~20초, 다른 화면에 다녀와도 됩니다)");
    const started = performance.now();
    const r = await runJob("POST", path + "/open", undefined, "열기: " + (s.title || s.name || ""));
    showResult(msg, links, r);
    msg.textContent += " · " + ((performance.now() - started) / 1000).toFixed(1) + "초";
  });
  if (s.state === "running") {
    act("멈추기", "secondary", async () => {
      say(msg, "멈추는 중…");
      const r = await call("POST", path + "/stop");
      say(msg, "멈춤: " + (stateNames[r.session.state] || r.session.state));
    });
  }
  act("지우기", "secondary", async () => {
    if (!confirm("목록에서 지울까요? 대화 기록은 PC에 남습니다.")) return;
    if (!confirm("정말 지울까요? (실행 중이면 멈춘 뒤 지웁니다)")) return;
    say(msg, "지우는 중…");
    await call("DELETE", path);
    li.remove();
  });
  if (s.remoteUrl && s.remoteUrl.startsWith("https://claude.ai/code/session_") && s.state === "running") {
    links.append(remoteLink(s.remoteUrl));
  }
  li.append(buttons, links, msg);
  if (s.attachCommand) li.append(el("div", "meta", "PC: " + s.attachCommand));
  return li;
}

async function loadFolders() {
  say($("newMsg"), "");
  try {
    const r = await call("GET", "folders");
    const sel = $("folder");
    sel.replaceChildren();
    for (const f of r.folders) {
      const o = el("option", "", f.name + " (" + f.path + ")");
      o.value = f.id;
      sel.append(o);
    }
    if (!r.folders.length) say($("newMsg"), "허용된 폴더가 없습니다. PC에서 asksemble-agent folders add 경로 로 더하세요.", true);
  } catch (e) {
    say($("newMsg"), e.message, true);
  }
}
$("folderBtn").addEventListener("click", loadFolders);

// Folder servers (F10).
const serveNames = {
  off: "꺼짐", starting: "켜는 중", running: "열려 있음", failed: "꺼짐(다시 켜는 중)",
  untrusted: "PC에서 신뢰 필요", external: "다른 서버가 실행 중",
};
let servePoll = 0;

function renderServeFolder(f) {
  const li = el("li");
  li.append(el("div", "title", f.name));
  const st = f.status || { state: "off" };
  const parts = [serveNames[st.state] || st.state];
  if (st.state === "running") parts.push("세션 " + st.sessions + "/" + (st.capacity || f.server.capacity));
  if (f.server.enabled) parts.push(f.server.spawn === "worktree" ? "worktree" : "같은 폴더");
  li.append(el("div", "meta", parts.join(" · ") + " · " + f.path));
  if (st.state === "untrusted") {
    li.append(el("div", "msg err", "PC에서 이 폴더로 claude를 한 번 실행해 신뢰를 허용하세요. 1분마다 다시 확인합니다."));
  } else if (st.error && st.state !== "running") {
    li.append(el("div", "msg", st.error));
  }
  const msg = el("div", "msg");
  const b = el("button", f.server.enabled ? "secondary" : "", f.server.enabled ? "끄기" : "켜기");
  b.addEventListener("click", async () => {
    b.disabled = true;
    try {
      await call("PUT", "folders/" + encodeURIComponent(f.id) + "/server", { enabled: !f.server.enabled });
      servePoll = 10; // follow the change for a while
      await loadServe();
    } catch (e) {
      say(msg, e.message, true);
      b.disabled = false;
    }
  });
  li.append(b, msg);
  return li;
}

async function loadServe() {
  try {
    const r = await call("GET", "folders");
    $("serveList").replaceChildren(...r.folders.map(renderServeFolder));
    if (!r.folders.length) $("serveList").append(el("li", "msg", "허용된 폴더가 없습니다."));
    const moving = r.folders.some((f) => f.status && ["starting", "failed"].includes(f.status.state));
    if (servePoll > 0 || moving) {
      servePoll--;
      setTimeout(loadServe, 2000);
    }
  } catch (e) {
    $("serveList").replaceChildren(el("li", "msg err", e.message));
  }
}
$("serveBtn").addEventListener("click", loadServe);

$("newBtn").addEventListener("click", async () => {
  if (!$("folder").value) {
    say($("newMsg"), "폴더를 먼저 고르세요.", true);
    return;
  }
  const btn = $("newBtn");
  btn.disabled = true;
  say($("newMsg"), "새 세션을 켜는 중… (보통 10~20초, 다른 화면에 다녀와도 됩니다)");
  $("newLink").replaceChildren();
  try {
    const r = await runJob("POST", "sessions", { folder: $("folder").value, name: $("newName").value }, "새 세션: " + $("newName").value);
    showResult($("newMsg"), $("newLink"), r);
  } catch (e) {
    say($("newMsg"), e.message, true);
  } finally {
    btn.disabled = false;
  }
});

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
      list.append(renderSession(s));
    }
    const ms = Math.round(performance.now() - started);
    say($("useMsg"), r.sessions.length + "개, " + ms + "ms" + (r.warnings.length ? " · 경고: " + r.warnings.join(", ") : ""));
  } catch (e) {
    say($("useMsg"), e.message, true);
  }
});

$("forgetBtn").addEventListener("click", () => {
  setToken("");
  store(DEV, "");
  $("list").replaceChildren();
  $("info").hidden = true;
  showUse();
});

showUse();
showWho();
resumeJob();
if (!token() && load(REQ)) {
  waitForPC(load(REQ));
}
