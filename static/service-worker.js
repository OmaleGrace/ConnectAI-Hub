/* ConnectAI Hub service worker: pre-caches the app shell, serves static files
   stale-while-revalidate, and shows a friendly page when a navigation fails offline. */
const CACHE = "connectai-shell-v5";
const SHELL = [
  "/static/manifest.webmanifest",
  "/static/icons/mark.svg", "/static/icons/icon-192.png", "/static/icons/icon-512.png"
];
const OFFLINE = `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Offline · ConnectAI Hub</title><body style="font:16px system-ui;display:grid;place-items:center;min-height:100vh;margin:0;background:#faf7f2;color:#1c1917;text-align:center"><div><h1 style="font-family:Georgia,serif">You’re offline</h1><p>ConnectAI Hub needs a connection to reach your assistants. Check your network and try again.</p><p><a href="/" style="color:#d9480f">Retry</a></p></div>`;

self.addEventListener("install", (event) => {
  // Best effort: a failed pre-cache must never block installation.
  event.waitUntil(caches.open(CACHE).then((c) => Promise.allSettled(SHELL.map((u) => c.add(u)))).then(() => self.skipWaiting()));
});
self.addEventListener("activate", (event) => {
  event.waitUntil((async () => {
    const keys = await caches.keys();
    await Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)));
    await self.clients.claim();
  })());
});
self.addEventListener("fetch", (event) => {
  const req = event.request;
  const url = new URL(req.url);
  if (req.method !== "GET" || url.origin !== self.location.origin) return;
  if (req.mode === "navigate") {
    event.respondWith(fetch(req).catch(() => new Response(OFFLINE, { status: 503, headers: { "Content-Type": "text/html; charset=utf-8" } })));
    return;
  }
  if (!url.pathname.startsWith("/static/")) return;
  const versioned = url.searchParams.has("v"); // fingerprinted by the server: the content never changes
  event.respondWith(caches.open(CACHE).then(async (cache) => {
    const cached = await cache.match(req);
    if (cached && versioned) return cached; // instant, no network request
    const network = fetch(req).then((res) => { if (res.ok) cache.put(req, res.clone()); return res; }).catch(() => cached);
    return cached || network;
  }));
});
