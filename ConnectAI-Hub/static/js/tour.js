/* ConnectAI Hub — guided tour.
   - Signed-in members: a spotlight tour of the real dashboard, shown once per account
     (replay any time from "Take a tour" or Settings).
   - Visitors on the home page: a small, dismissible invitation, then a short tour of the page.
   Works on phones (bottom sheet), is keyboard accessible and respects reduced motion. */
(function () {
  "use strict";

  const body = document.body;
  const uid = body.dataset.uid || "";
  const firstName = (body.dataset.name || "").trim().split(/\s+/)[0];
  const path = location.pathname;
  const KEY = uid ? "connectai-tour-v2-app-" + uid : "connectai-tour-v2-visitor";

  const store = {
    get(k) { try { return localStorage.getItem(k); } catch (_) { return null; } },
    set(k, v) { try { localStorage.setItem(k, v); } catch (_) {} }
  };
  const reduceMotion = matchMedia("(prefers-reduced-motion: reduce)").matches;
  const isMobile = () => matchMedia("(max-width: 640px)").matches;
  const isStandalone = () => matchMedia("(display-mode: standalone)").matches || navigator.standalone === true;
  const firstVisible = (selector) => {
    if (!selector) return null;
    for (const el of document.querySelectorAll(selector)) if (el.getClientRects().length) return el;
    return null;
  };
  const openInstall = () => { if (window.ConnectAIInstall) window.ConnectAIInstall.open(); };

  // ---------- Steps ----------
  const memberSteps = () => [
    {
      title: firstName ? "Welcome, " + firstName + "." : "Welcome to ConnectAI Hub.",
      text: "Here is a 60-second look around. You can skip it now and replay it any time from “Take a tour”."
    },
    {
      target: "#moduleSearch",
      title: "Find the right assistant fast",
      text: "Type a name or a task (for example “CV”, “quiz” or “spreadsheet”) and the cards below filter as you type."
    },
    {
      target: "#moduleGrid .tool-card",
      title: "Seven assistants, one job each",
      text: "Study, Career, Business, Document, Website, Support and Data. Every assistant keeps its own saved conversation, so you can pick up where you stopped."
    },
    {
      target: ".usage-plan-card",
      title: "Know your daily allowance",
      text: "This card shows how many AI requests you have used today and how many remain. It resets at midnight, server time."
    },
    {
      target: '.nav-links a[href="/builder"]',
      nav: true,
      title: "Build and reuse",
      text: "The Website builder turns a description into a live preview you can edit and download. The Prompt library keeps the prompts you use again and again."
    },
    {
      target: "[data-install-app]",
      skip: isStandalone,
      title: "Keep it on your phone",
      text: "Install ConnectAI Hub on your home screen. It opens full screen, like any other app, with no app store.",
      cta: { label: "Install the app", run: openInstall }
    },
    {
      title: "Inside every workspace",
      text: "Copy or edit any message you sent, regenerate an answer, search the chat, or export it as Markdown. Tell an assistant something lasting about you and it can remember it (you control this in Settings)."
    },
    {
      title: "You’re all set.",
      text: "A good first move: open Study AI and ask it to explain something you are learning right now.",
      cta: { label: "Open Study AI", href: "/modules/study" }
    }
  ];

  const visitorSteps = () => [
    {
      target: ".hero-actions",
      title: "Start free",
      text: "Create a free account to unlock all seven assistants. Takes under a minute."
    },
    {
      target: ".demo",
      title: "See how a chat looks",
      text: "Ask a question, get a clear answer, and keep going. Here is a Study AI example."
    },
    {
      target: "#tools .tool-card",
      title: "Seven specialists",
      text: "Study, Career, Business, Documents, Website, Support and Data. Each one is tuned for its own kind of work."
    },
    {
      target: "#how .steps",
      title: "Three simple steps",
      text: "Choose a workspace, ask or upload a file, then keep the conversation going. Everything is saved to your account."
    },
    {
      target: ".plans",
      title: "Free or Plus",
      text: "Start on the free daily allowance. Upgrade to Plus only if you need far more requests per day."
    },
    {
      target: "#install",
      skip: isStandalone,
      title: "Get it on your phone",
      text: "Add ConnectAI Hub to your home screen in a few taps. No app store needed.",
      cta: { label: "Install the app", run: openInstall }
    },
    {
      title: "Ready when you are.",
      text: "Create your free account and say hello to your first assistant.",
      cta: { label: "Create free account", href: "/register" }
    }
  ];

  // ---------- DOM ----------
  let root, spot, card, eyebrow, titleEl, textEl, ctaEl, dots, backBtn, nextBtn, skipBtn;
  let steps = [], idx = 0, target = null, lastFocus = null, navOpened = false, raf = 0;

  function build() {
    if (root) return;
    root = document.createElement("div");
    root.className = "ct-root";
    root.innerHTML =
      '<div class="ct-block"></div><div class="ct-spot" aria-hidden="true"></div>' +
      '<div class="ct-card" role="dialog" aria-modal="true" aria-labelledby="ctTitle" aria-describedby="ctText">' +
      '<small class="ct-eyebrow" id="ctEyebrow"></small>' +
      '<h2 class="ct-title" id="ctTitle" tabindex="-1"></h2>' +
      '<p class="ct-text" id="ctText" aria-live="polite"></p>' +
      '<div class="ct-cta-row"><button type="button" class="button button-ghost button-small ct-cta" hidden></button></div>' +
      '<div class="ct-dots" aria-hidden="true"></div>' +
      '<div class="ct-actions"><button type="button" class="ct-skip">Skip tour</button>' +
      '<div><button type="button" class="button button-ghost button-small ct-back">← Back</button>' +
      '<button type="button" class="button button-small ct-next">Next →</button></div></div></div>';
    document.body.appendChild(root);
    spot = root.querySelector(".ct-spot");
    card = root.querySelector(".ct-card");
    eyebrow = root.querySelector(".ct-eyebrow");
    titleEl = root.querySelector(".ct-title");
    textEl = root.querySelector(".ct-text");
    ctaEl = root.querySelector(".ct-cta");
    dots = root.querySelector(".ct-dots");
    backBtn = root.querySelector(".ct-back");
    nextBtn = root.querySelector(".ct-next");
    skipBtn = root.querySelector(".ct-skip");
    backBtn.addEventListener("click", () => go(-1));
    nextBtn.addEventListener("click", () => (idx >= steps.length - 1 ? finish() : go(1)));
    skipBtn.addEventListener("click", () => finish());
    ctaEl.addEventListener("click", () => {
      const s = steps[idx];
      if (!s || !s.cta) return;
      if (s.cta.href) { finish(); location.href = s.cta.href; return; }
      if (s.cta.run) s.cta.run();
    });
  }

  // ---------- Mobile menu helper (nav links are collapsed under ☰ on phones) ----------
  function menuToggle() { const t = document.querySelector(".menu-toggle"); return t && t.getClientRects().length ? t : null; }
  function setNav(open) {
    const toggle = menuToggle(), links = document.querySelector(".nav-links");
    if (!toggle || !links) return;
    links.classList.toggle("open", open);
    toggle.setAttribute("aria-expanded", open ? "true" : "false");
    navOpened = open;
  }

  // ---------- Layout ----------
  function place() {
    raf = 0;
    if (!root || !root.classList.contains("open")) return;
    if (!target || !target.isConnected || !target.getClientRects().length) {
      root.classList.add("center");
      spot.style.display = "none";
      card.style.left = card.style.top = "";
      return;
    }
    root.classList.remove("center");
    const r = target.getBoundingClientRect(), pad = 8;
    spot.style.display = "block";
    spot.style.left = r.left - pad + "px";
    spot.style.top = r.top - pad + "px";
    spot.style.width = r.width + pad * 2 + "px";
    spot.style.height = r.height + pad * 2 + "px";
    if (isMobile()) { card.style.left = card.style.top = ""; return; } // CSS pins it as a bottom sheet
    const cw = card.offsetWidth, ch = card.offsetHeight, vw = innerWidth, vh = innerHeight, gap = 16;
    let top = r.bottom + pad + gap;
    if (top + ch > vh - 12) top = r.top - pad - gap - ch;       // not enough room below: go above
    if (top < 12) top = Math.max(12, Math.min(vh - ch - 12, vh / 2 - ch / 2)); // neither fits: float
    let left = r.left + r.width / 2 - cw / 2;
    left = Math.max(12, Math.min(vw - cw - 12, left));
    card.style.left = left + "px";
    card.style.top = top + "px";
  }
  const schedule = () => { if (!raf) raf = requestAnimationFrame(place); };

  function resolveTarget(step) {
    if (step.nav) setNav(true); else if (navOpened) setNav(false);
    return firstVisible(step.target);
  }

  function render() {
    const s = steps[idx];
    eyebrow.textContent = "STEP " + (idx + 1) + " OF " + steps.length;
    titleEl.textContent = s.title;
    textEl.textContent = s.text;
    if (s.cta) { ctaEl.hidden = false; ctaEl.textContent = s.cta.label; } else { ctaEl.hidden = true; }
    backBtn.disabled = idx === 0;
    nextBtn.textContent = idx === steps.length - 1 ? "Done ✓" : "Next →";
    skipBtn.hidden = idx === steps.length - 1;
    dots.innerHTML = steps.map((_, i) => '<i class="' + (i === idx ? "on" : i < idx ? "done" : "") + '"></i>').join("");
    target = resolveTarget(s);
    if (target) {
      target.style.scrollMarginTop = isMobile() ? "92px" : "120px";
      target.scrollIntoView({ block: isMobile() ? "start" : "center", behavior: reduceMotion ? "auto" : "smooth" });
    }
    place();
    setTimeout(place, 350); // after smooth scrolling settles
    titleEl.focus({ preventScroll: true });
  }

  function go(delta) {
    let next = idx + delta;
    while (steps[next] && steps[next].skip && steps[next].skip()) next += delta;
    if (next < 0 || next >= steps.length) return;
    idx = next;
    render();
  }

  // ---------- Open / close ----------
  function start(list) {
    build();
    steps = list.filter((s, i) => !(s.skip && s.skip() && i !== 0));
    idx = 0;
    lastFocus = document.activeElement;
    root.classList.add("open");
    document.addEventListener("keydown", onKey, true);
    addEventListener("resize", schedule);
    addEventListener("scroll", schedule, { passive: true });
    render();
  }

  function finish() {
    store.set(KEY, "done");
    close();
  }

  function close() {
    if (!root) return;
    root.classList.remove("open", "center");
    if (target) target.style.scrollMarginTop = "";
    target = null;
    if (navOpened) setNav(false);
    document.removeEventListener("keydown", onKey, true);
    removeEventListener("resize", schedule);
    removeEventListener("scroll", schedule);
    if (lastFocus && lastFocus.focus && lastFocus.isConnected) lastFocus.focus({ preventScroll: true });
    document.dispatchEvent(new CustomEvent("connectai:tour-close"));
  }

  function onKey(e) {
    if (!root.classList.contains("open")) return;
    if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); finish(); return; }
    if (e.key === "ArrowRight") { e.preventDefault(); if (idx < steps.length - 1) go(1); return; }
    if (e.key === "ArrowLeft") { e.preventDefault(); go(-1); return; }
    if (e.key === "Tab") { // keep focus inside the tour card
      const items = [...card.querySelectorAll("button:not([hidden]):not([disabled])")];
      if (!items.length) return;
      const first = items[0], last = items[items.length - 1];
      if (!card.contains(document.activeElement)) { e.preventDefault(); first.focus(); }
      else if (e.shiftKey && (document.activeElement === first || document.activeElement === titleEl)) { e.preventDefault(); last.focus(); }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
    }
  }

  // ---------- Visitor invitation (never a blocking pop-up) ----------
  function showInvite() {
    if (document.querySelector(".ct-invite")) return;
    const box = document.createElement("div");
    box.className = "ct-invite";
    box.setAttribute("role", "region");
    box.setAttribute("aria-label", "Site tour");
    box.innerHTML = '<span aria-hidden="true">👋</span><div><b>New here?</b><small>Take a 30-second tour.</small></div>' +
      '<button type="button" class="button button-small ct-invite-go">Show me</button>' +
      '<button type="button" class="ct-invite-x" aria-label="Dismiss">×</button>';
    document.body.appendChild(box);
    const remove = (remember) => {
      if (remember) store.set(KEY, "skipped");
      box.remove();
      document.dispatchEvent(new CustomEvent("connectai:invite-close"));
    };
    box.querySelector(".ct-invite-go").addEventListener("click", () => { remove(false); start(visitorSteps()); });
    box.querySelector(".ct-invite-x").addEventListener("click", () => remove(true));
  }

  // ---------- Wiring ----------
  document.querySelectorAll("[data-tour-start]").forEach((btn) => btn.addEventListener("click", () => {
    if (uid && path !== "/dashboard") { location.href = "/dashboard?tour=1"; return; }
    if (!uid && path !== "/") { location.href = "/?tour=1"; return; }
    document.querySelector(".ct-invite")?.remove();
    start(uid ? memberSteps() : visitorSteps());
  }));

  const params = new URLSearchParams(location.search);
  const forced = params.get("tour") === "1";
  if (forced) { params.delete("tour"); const q = params.toString(); history.replaceState(null, "", path + (q ? "?" + q : "") + location.hash); }

  const alreadySeen = !!store.get(KEY);
  if (uid && path === "/dashboard" && (forced || !alreadySeen)) {
    setTimeout(() => start(memberSteps()), 700);
  } else if (!uid && path === "/") {
    if (forced) setTimeout(() => start(visitorSteps()), 400);
    else if (!alreadySeen) setTimeout(showInvite, 2500);
  }
})();
