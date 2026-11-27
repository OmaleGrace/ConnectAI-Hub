/* ConnectAI Hub — install as an app (PWA).
   Fixes the old behaviour where every Install button stayed hidden unless the browser fired
   "beforeinstallprompt" (it never does on iPhone/iPad, Firefox or in-app browsers), which made the
   iPhone instructions unreachable. Now:
   - Install buttons are visible everywhere unless the app is already installed.
   - Android/desktop Chrome & Edge: one tap opens the native install dialog.
   - iPhone/iPad, in-app browsers (Facebook, Instagram, WhatsApp…) and others: a clear step-by-step sheet.
   - On phones, a dismissible banner offers the install on the home and dashboard pages. */
(function () {
  "use strict";

  const ua = navigator.userAgent || "";
  const isIOS = /iPad|iPhone|iPod/.test(ua) || (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1);
  const isAndroid = /Android/i.test(ua);
  const isIOSSafari = isIOS && /Safari/.test(ua) && !/CriOS|FxiOS|EdgiOS|OPiOS|GSA\//.test(ua);
  const inAppBrowser = /FBAN|FBAV|Instagram|Snapchat|TikTok|musical_ly|MicroMessenger|LinkedInApp|Twitter|; wv\)/i.test(ua);
  const isSecure = location.protocol === "https:" || location.hostname === "localhost" || location.hostname === "127.0.0.1";
  const isStandalone = () =>
    matchMedia("(display-mode: standalone)").matches ||
    matchMedia("(display-mode: minimal-ui)").matches ||
    navigator.standalone === true;

  const store = {
    get(k) { try { return localStorage.getItem(k); } catch (_) { return null; } },
    set(k, v) { try { localStorage.setItem(k, v); } catch (_) {} }
  };
  const SNOOZE_KEY = "connectai-install-snooze";
  const DAY = 86400000;

  let deferred = null; // the browser's native install prompt, when it offers one

  // ---------- Buttons and status text ----------
  function refresh() {
    const installed = isStandalone();
    document.querySelectorAll("[data-install-app]").forEach((b) => { b.hidden = installed; b.disabled = false; });
    document.querySelectorAll("[data-install-status]").forEach((el) => {
      el.textContent = installed ? "✓ You’re using the installed app." : "";
    });
    if (installed) document.querySelector(".ci-banner")?.remove();
  }

  // ---------- Guidance sheet ----------
  function guide() {
    if (!isSecure) {
      return { title: "A secure address is needed", intro: "Browsers only allow installing apps from https:// addresses. Open the secure version of this site, then try again.", steps: [] };
    }
    if (inAppBrowser) {
      return {
        title: "Open in your browser first",
        intro: "You are inside another app’s built-in browser, which cannot install apps.",
        steps: [
          "Tap the ⋯ or ⋮ menu at the top or bottom corner.",
          isIOS ? "Choose “Open in Safari”." : "Choose “Open in Chrome” (or “Open in browser”).",
          "Come back to this page and tap Install again."
        ],
        copy: true
      };
    }
    if (isIOS) {
      return {
        title: "Add to your Home Screen",
        intro: isIOSSafari ? "On iPhone and iPad this takes three taps." : "On iPhone and iPad this takes three taps. If you do not see the option, open this page in Safari.",
        steps: [
          "Tap the Share button (a square with an arrow pointing up) in the browser toolbar.",
          "Scroll down and tap “Add to Home Screen”.",
          "Tap “Add”. ConnectAI Hub now sits on your home screen."
        ]
      };
    }
    if (isAndroid) {
      return {
        title: "Install on your Android phone",
        intro: "Use Chrome (or Samsung Internet or Edge) for the best result.",
        steps: [
          "Tap the ⋮ menu at the top right of the browser.",
          "Tap “Install app” (or “Add to Home screen”).",
          "Tap “Install”. The app appears with your other apps."
        ]
      };
    }
    return {
      title: "Install on your computer",
      intro: "Works in Chrome and Edge. In Safari on Mac use File → Add to Dock.",
      steps: [
        "Look for the install icon at the right end of the address bar, or open the browser menu.",
        "Choose “Install ConnectAI Hub”.",
        "Confirm. It opens in its own window."
      ]
    };
  }

  let sheet = null, sheetFocus = null;
  function closeSheet() {
    if (!sheet) return;
    sheet.remove(); sheet = null;
    document.removeEventListener("keydown", sheetKey, true);
    if (sheetFocus && sheetFocus.isConnected) sheetFocus.focus({ preventScroll: true });
  }
  function sheetKey(e) {
    if (!sheet) return;
    if (e.key === "Escape") { e.preventDefault(); closeSheet(); return; }
    if (e.key === "Tab") {
      const items = [...sheet.querySelectorAll("button")];
      if (!items.length) return;
      const first = items[0], last = items[items.length - 1];
      if (!sheet.contains(document.activeElement)) { e.preventDefault(); first.focus(); }
      else if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
    }
  }
  function esc(s) { const d = document.createElement("div"); d.textContent = s; return d.innerHTML; }

  function openSheet() {
    if (sheet) return;
    const g = guide();
    sheetFocus = document.activeElement;
    sheet = document.createElement("div");
    sheet.className = "ci-backdrop";
    sheet.innerHTML =
      '<div class="ci-sheet" role="dialog" aria-modal="true" aria-labelledby="ciTitle">' +
      '<div class="ci-head"><img src="/static/icons/icon-192.png" width="48" height="48" alt=""><div><h2 id="ciTitle">' + esc(g.title) + "</h2><p>" + esc(g.intro) + "</p></div></div>" +
      (g.steps.length ? '<ol class="ci-steps">' + g.steps.map((s) => "<li>" + esc(s) + "</li>").join("") + "</ol>" : "") +
      '<ul class="ci-perks"><li>Opens full screen, one tap from your home screen</li><li>Launches faster and keeps your sign-in</li><li>Uses almost no storage and updates itself</li></ul>' +
      '<div class="ci-actions">' + (g.copy ? '<button type="button" class="button button-ghost ci-copy">Copy link</button>' : "") +
      '<button type="button" class="button ci-close">Got it</button></div></div>';
    document.body.appendChild(sheet);
    sheet.addEventListener("click", (e) => { if (e.target === sheet) closeSheet(); });
    sheet.querySelector(".ci-close").addEventListener("click", closeSheet);
    const copy = sheet.querySelector(".ci-copy");
    if (copy) copy.addEventListener("click", async () => {
      try { await navigator.clipboard.writeText(location.origin + "/"); copy.textContent = "Link copied ✓"; }
      catch (_) { copy.textContent = location.origin; }
    });
    document.addEventListener("keydown", sheetKey, true);
    sheet.querySelector(".ci-close").focus();
  }

  // ---------- Install action ----------
  async function install() {
    if (isStandalone()) return;
    if (!deferred) { openSheet(); return; }
    const promptEvent = deferred;
    deferred = null; // a prompt event can only be used once
    try {
      await promptEvent.prompt();
      const choice = await promptEvent.userChoice;
      if (choice && choice.outcome === "accepted") {
        store.set(SNOOZE_KEY, String(Date.now() + 365 * DAY));
        document.querySelector(".ci-banner")?.remove();
      }
    } catch (_) {
      openSheet(); // prompt failed: show the manual steps instead of leaving the user stuck
    }
  }

  window.addEventListener("beforeinstallprompt", (e) => { e.preventDefault(); deferred = e; refresh(); });
  window.addEventListener("appinstalled", () => { deferred = null; store.set(SNOOZE_KEY, String(Date.now() + 365 * DAY)); refresh(); });
  matchMedia("(display-mode: standalone)").addEventListener?.("change", refresh);
  document.addEventListener("click", (e) => {
    const b = e.target.closest("[data-install-app]");
    if (b) { e.preventDefault(); install(); }
  });

  // ---------- Banner (phones only, home + dashboard only, snoozable) ----------
  function bannerAllowed() {
    const page = document.body.dataset.page;
    if (page !== "home" && page !== "dashboard") return false;
    if (isStandalone() || inAppBrowser || !isSecure) return false;
    if (!(matchMedia("(max-width: 820px)").matches || matchMedia("(pointer: coarse)").matches)) return false;
    return Date.now() > Number(store.get(SNOOZE_KEY) || 0);
  }
  function overlayOpen() { return !!(document.querySelector(".ct-root.open") || document.querySelector(".ct-invite") || sheet); }

  function showBanner() {
    if (document.querySelector(".ci-banner") || !bannerAllowed()) return;
    if (overlayOpen()) { // wait for the tour / invitation to finish instead of stacking on top of it
      const retry = () => setTimeout(showBanner, 1500);
      document.addEventListener("connectai:tour-close", retry, { once: true });
      document.addEventListener("connectai:invite-close", retry, { once: true });
      return;
    }
    const el = document.createElement("div");
    el.className = "ci-banner";
    el.setAttribute("role", "region");
    el.setAttribute("aria-label", "Install the app");
    el.innerHTML = '<img src="/static/icons/icon-192.png" width="40" height="40" alt="">' +
      '<div><b>Install ConnectAI Hub</b><small>' + (isIOS ? "Add it to your Home Screen" : "Full screen, one tap from your home screen") + "</small></div>" +
      '<button type="button" class="button button-small ci-banner-go">' + (isIOS ? "How" : "Install") + "</button>" +
      '<button type="button" class="ci-banner-x" aria-label="Not now">×</button>';
    document.body.appendChild(el);
    el.querySelector(".ci-banner-go").addEventListener("click", install);
    el.querySelector(".ci-banner-x").addEventListener("click", () => {
      store.set(SNOOZE_KEY, String(Date.now() + 14 * DAY));
      el.remove();
    });
  }

  refresh();
  setTimeout(showBanner, 9000);

  window.ConnectAIInstall = { open: install, isStandalone, canPromptNatively: () => !!deferred };
})();
