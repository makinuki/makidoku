import { defineConfig } from "vite-plus";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: { port: 5173, proxy: { "/api": "http://127.0.0.1:8080" } },
  test: { environment: "jsdom", setupFiles: "./src/vitest.setup.ts", css: true },
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
