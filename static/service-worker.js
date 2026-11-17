/* ConnectAI Hub PWA worker. Keep installation lightweight: do not wait for a
   large cache.addAll() request before the browser can finish installing. */
const CACHE_NAME = "connectai-shell-v23";
const STATIC_ASSETS = [
  "/static/css/app.css", "/static/css/auth.css", "/static/css/chat.css",
  "/static/js/app.js", "/static/manifest.webmanifest",
  "/static/icons/icon-192.png", "/static/icons/icon-512.png"
];
self.addEventListener("install", (event) => {
  // Activate immediately. Static files are cached lazily after successful fetches.
  event.waitUntil(caches.open(CACHE_NAME).then(() => self.skipWaiting()));
});
self.addEventListener("activate", (event) => {
  event.waitUntil((async () => {
    const keys = await caches.keys();
    await Promise.all(keys.filter((key) => key.startsWith("connectai-static-") && key !== CACHE_NAME).map((key) => caches.delete(key)));
    await self.clients.claim();
  })());
});
self.addEventListener("fetch", (event) => {
  const request = event.request;
  const url = new URL(request.url);
  if (request.method !== "GET" || url.origin !== self.location.origin || !url.pathname.startsWith("/static/")) return;
  event.respondWith((async () => {
    const cached = await caches.match(request);
    if (cached) return cached;
    try {
      const response = await fetch(request);
      if (response.ok) { const copy = response.clone(); caches.open(CACHE_NAME).then((cache) => cache.put(request, copy)).catch(() => {}); }
      return response;
    } catch (_) { return new Response("This resource is unavailable offline.", {status: 503, headers: {"Content-Type": "text/plain"}}); }
  })());
});
