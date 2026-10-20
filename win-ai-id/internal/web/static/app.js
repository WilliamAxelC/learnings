// win.ai.id client script: theme toggle, haptics, service worker, SSE streaming coach, WebAuthn passkeys, web push.

// 0. Theme Toggle & Persistence
function toggleTheme() {
  const isDark = document.documentElement.getAttribute("data-theme") === "dark";
  const nextDark = !isDark;
  const metaTheme = document.querySelector('meta[name="theme-color"]');

  if (nextDark) {
    document.documentElement.setAttribute("data-theme", "dark");
    try { localStorage.setItem("win-theme", "dark"); } catch (_) {}
    if (metaTheme) metaTheme.setAttribute("content", "#0b1320");
  } else {
    document.documentElement.removeAttribute("data-theme");
    try { localStorage.setItem("win-theme", "light"); } catch (_) {}
    if (metaTheme) metaTheme.setAttribute("content", "#eaf6ff");
  }

  const buttons = document.querySelectorAll(".theme-btn, #theme-toggle");
  buttons.forEach((btn) => {
    const label = nextDark ? "Switch to light theme" : "Switch to dark theme";
    btn.setAttribute("aria-label", label);
    btn.setAttribute("title", label);
  });
}
window.toggleTheme = toggleTheme;

function initThemeToggle() {
  const isDark = document.documentElement.getAttribute("data-theme") === "dark";
  const buttons = document.querySelectorAll(".theme-btn, #theme-toggle");
  buttons.forEach((btn) => {
    const label = isDark ? "Switch to light theme" : "Switch to dark theme";
    btn.setAttribute("aria-label", label);
    btn.setAttribute("title", label);
    btn.addEventListener("click", (e) => {
      e.preventDefault();
      toggleTheme();
    });
  });
}

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", initThemeToggle);
} else {
  initThemeToggle();
}

// 1. Haptics on win completion
document.addEventListener("click", (e) => {
  const btn = e.target.closest(".check[data-win='1']");
  if (btn && navigator.vibrate) navigator.vibrate(12);
});

// 2. Service Worker registration
if ("serviceWorker" in navigator) {
  navigator.serviceWorker.register("/sw.js").catch(() => {});
}

// 3. Toggle win note form
function toggleWinNote(id) {
  const form = document.getElementById("note-form-" + id);
  if (form) {
    form.classList.toggle("hidden");
    if (!form.classList.contains("hidden")) {
      const input = form.querySelector("input");
      if (input) input.focus();
    }
  }
}

// 4. Coach SSE Streaming
async function handleCoachSubmit(e, form) {
  if (!window.ReadableStream || !window.fetch) return true; // fallback to htmx post
  e.preventDefault();

  const input = form.querySelector("input[name='message']");
  const btn = form.querySelector("button[type='submit']");
  const msg = input.value.trim();
  const msgsContainer = document.getElementById("coach-messages");

  // Show user message immediately
  if (msgsContainer) {
    const userBubble = document.createElement("p");
    userBubble.className = "msg user";
    userBubble.textContent = msg || "Plan my day.";
    msgsContainer.appendChild(userBubble);
  }

  // Create assistant placeholder
  const assistantBubble = document.createElement("p");
  assistantBubble.className = "msg assistant";
  assistantBubble.textContent = "…";
  if (msgsContainer) msgsContainer.appendChild(assistantBubble);

  if (btn) btn.disabled = true;
  input.value = "";

  try {
    const formData = new FormData();
    formData.append("message", msg);
    const resp = await fetch("/coach/stream", { method: "POST", body: formData });
    if (!resp.ok) throw new Error("stream failed");

    const reader = resp.body.getReader();
    const decoder = new TextDecoder();
    let assistantText = "";
    let buffer = "";

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split("\n\n");
      buffer = lines.pop(); // keep last unfinished piece

      for (const block of lines) {
        if (!block.trim()) continue;
        const lineParts = block.split("\n");
        let eventName = "message";
        let dataStr = "";
        for (const l of lineParts) {
          if (l.startsWith("event: ")) eventName = l.slice(7).trim();
          if (l.startsWith("data: ")) dataStr = l.slice(6).trim();
        }
        if (eventName === "token") {
          try {
            const parsed = JSON.parse(dataStr);
            if (parsed.delta) {
              if (assistantText === "") assistantBubble.textContent = "";
              assistantText += parsed.delta;
              assistantBubble.textContent = assistantText;
            }
          } catch (_) {}
        } else if (eventName === "done") {
          // Re-render app to update proposals and streak
          if (window.htmx) {
            htmx.ajax("GET", "/", { target: "#app", select: "#app", swap: "outerHTML" });
          } else {
            window.location.reload();
          }
          return false;
        }
      }
    }
    if (window.htmx) {
      htmx.ajax("GET", "/", { target: "#app", select: "#app", swap: "outerHTML" });
    }
  } catch (err) {
    console.error("Coach stream error:", err);
    if (window.htmx) {
      htmx.ajax("GET", "/", { target: "#app", select: "#app", swap: "outerHTML" });
    }
  } finally {
    if (btn) btn.disabled = false;
  }
  return false;
}

// 5. WebAuthn Passkeys Helper
function bufferToBase64URL(buffer) {
  const bytes = new Uint8Array(buffer);
  let str = "";
  for (let i = 0; i < bytes.byteLength; i++) {
    str += String.fromCharCode(bytes[i]);
  }
  return btoa(str).replace(/\+/g, "-").replace(/\//g, "_").replace(/=/g, "");
}

function base64URLToBuffer(base64URL) {
  let base64 = base64URL.replace(/-/g, "+").replace(/_/g, "/");
  while (base64.length % 4) base64 += "=";
  const raw = atob(base64);
  const buffer = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) buffer[i] = raw.charCodeAt(i);
  return buffer.buffer;
}

async function loginWithPasskey() {
  if (!window.PublicKeyCredential) {
    alert("Passkeys are not supported on this device/browser.");
    return;
  }
  try {
    const optRes = await fetch("/auth/passkey/login-begin");
    if (!optRes.ok) throw new Error("Failed to start passkey login");
    const options = await optRes.json();
    options.publicKey.challenge = base64URLToBuffer(options.publicKey.challenge);
    if (options.publicKey.allowCredentials) {
      options.publicKey.allowCredentials.forEach((c) => {
        c.id = base64URLToBuffer(c.id);
      });
    }

    const cred = await navigator.credentials.get({ publicKey: options.publicKey });
    const payload = {
      id: cred.id,
      rawId: bufferToBase64URL(cred.rawId),
      type: cred.type,
      response: {
        clientDataJSON: bufferToBase64URL(cred.response.clientDataJSON),
        authenticatorData: bufferToBase64URL(cred.response.authenticatorData),
        signature: bufferToBase64URL(cred.response.signature),
        userHandle: cred.response.userHandle ? bufferToBase64URL(cred.response.userHandle) : null,
      },
    };

    const finRes = await fetch("/auth/passkey/login-finish", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!finRes.ok) throw new Error("Passkey validation failed");
    window.location.href = "/";
  } catch (err) {
    console.error("Passkey login error:", err);
    alert("Passkey login cancelled or failed.");
  }
}

async function registerPasskey() {
  if (!window.PublicKeyCredential) {
    alert("Passkeys are not supported on this device/browser.");
    return;
  }
  try {
    const optRes = await fetch("/auth/passkey/register-begin");
    if (!optRes.ok) throw new Error("Failed to begin passkey registration");
    const options = await optRes.json();
    options.publicKey.challenge = base64URLToBuffer(options.publicKey.challenge);
    options.publicKey.user.id = base64URLToBuffer(options.publicKey.user.id);
    if (options.publicKey.excludeCredentials) {
      options.publicKey.excludeCredentials.forEach((c) => {
        c.id = base64URLToBuffer(c.id);
      });
    }

    const cred = await navigator.credentials.create({ publicKey: options.publicKey });
    const payload = {
      id: cred.id,
      rawId: bufferToBase64URL(cred.rawId),
      type: cred.type,
      response: {
        clientDataJSON: bufferToBase64URL(cred.response.clientDataJSON),
        attestationObject: bufferToBase64URL(cred.response.attestationObject),
      },
    };

    const finRes = await fetch("/auth/passkey/register-finish", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!finRes.ok) throw new Error("Failed to save passkey");
    alert("Passkey registered successfully! You can now use Face ID, Touch ID, or security key to sign in.");
  } catch (err) {
    console.error("Passkey registration error:", err);
    alert("Passkey registration cancelled or failed.");
  }
}

// 6. Web Push subscription helper
async function subscribePush() {
  if (!("serviceWorker" in navigator) || !("PushManager" in window)) {
    alert("Push notifications are not supported by this browser.");
    return;
  }
  try {
    const perm = await Notification.requestPermission();
    if (perm !== "granted") {
      alert("Notification permission was not granted.");
      return;
    }
    const reg = await navigator.serviceWorker.ready;
    let sub = await reg.pushManager.getSubscription();
    if (!sub) {
      sub = await reg.pushManager.subscribe({
        userVisibleOnly: true,
        // Default applicationServerKey or nil for browser-supported push
        applicationServerKey: null,
      });
    }
    const jsonSub = sub.toJSON();
    const res = await fetch("/push/subscribe", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        endpoint: sub.endpoint,
        keys: {
          p256dh: jsonSub.keys ? jsonSub.keys.p256dh : "",
          auth: jsonSub.keys ? jsonSub.keys.auth : "",
        },
      }),
    });
    if (res.ok) {
      alert("Push notifications enabled! You'll receive morning plans and evening nudges.");
    }
  } catch (err) {
    console.error("Push subscription error:", err);
    alert("Could not subscribe to push notifications: " + err.message);
  }
}
