// Vrac service worker: caches the application shell so the app opens fast.
// API responses are never cached: data always comes from the server.
const CACHE = "vrac-shell-v2"

self.addEventListener("install", (event) => {
  event.waitUntil(caches.open(CACHE).then((c) => c.addAll(["/", "/manifest.webmanifest", "/icon.svg"])))
  self.skipWaiting()
})

self.addEventListener("activate", (event) => {
  event.waitUntil(caches.keys().then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)))))
  self.clients.claim()
})

self.addEventListener("fetch", (event) => {
  const url = new URL(event.request.url)
  if (event.request.method !== "GET" || url.origin !== location.origin) return
  if (/^\/(api|mcp|oauth|\.well-known)\b/.test(url.pathname)) return
  // Network first, shell from cache when offline.
  event.respondWith(
    fetch(event.request)
      .then((res) => {
        if (res.ok && (url.pathname.startsWith("/assets/") || event.request.mode === "navigate")) {
          const copy = res.clone()
          caches.open(CACHE).then((c) => c.put(event.request.mode === "navigate" ? "/" : event.request, copy))
        }
        return res
      })
      .catch(() => caches.match(event.request.mode === "navigate" ? "/" : event.request)),
  )
})
