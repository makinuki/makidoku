import { defineConfig } from "vite-plus";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { VitePWA } from "vite-plugin-pwa";

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    VitePWA({
      registerType: "prompt",
      devOptions: { enabled: false },
      workbox: {
        navigateFallbackDenylist: [/^\/api\//],
        runtimeCaching: [
          {
            // Same-origin cover and page image GETs only; every other /api/
            // request (JSON, CBZ downloads, non-GET) falls through to the
            // network untouched. Only 200 responses enter the cache so a
            // transient upstream error is never served back later.
            urlPattern: /\/api\/(manga\/[^/?]+\/cover|pages\/[^/?]+\/image)/,
            handler: "CacheFirst",
            options: {
              cacheName: "makidoku-images",
              cacheableResponse: { statuses: [200] },
              expiration: { maxEntries: 300, maxAgeSeconds: 30 * 24 * 60 * 60 },
            },
          },
        ],
      },
      manifest: {
        name: "MakiDoku",
        short_name: "MakiDoku",
        display: "standalone",
        theme_color: "#09090b",
        background_color: "#09090b",
        start_url: "/",
        scope: "/",
        orientation: "any",
        categories: ["entertainment", "books"],
        icons: [
          { src: "/pwa-192.png", sizes: "192x192", type: "image/png" },
          { src: "/pwa-512.png", sizes: "512x512", type: "image/png" },
          {
            src: "/maskable-512.png",
            sizes: "512x512",
            type: "image/png",
            purpose: "maskable",
          },
        ],
        shortcuts: [
          { name: "Library", url: "/library" },
          { name: "Updates", url: "/updates" },
          { name: "Downloads", url: "/downloads" },
        ],
      },
    }),
  ],
  build: { outDir: "dist", emptyOutDir: true },
  server: { port: 5173, proxy: { "/api": "http://127.0.0.1:8080" } },
  test: {
    environment: "jsdom",
    setupFiles: "./src/vitest.setup.ts",
    css: true,
    alias: { "virtual:pwa-register": "/src/test-stubs/pwaRegister.ts" },
  },
  lint: { ignorePatterns: ["dist/**", "node_modules/**"] },
  fmt: {
    ignorePatterns: [
      "dist/**",
      "node_modules/**",
      "vite.config.js",
      "vite.config.d.ts",
      "*.tsbuildinfo",
    ],
  },
});
