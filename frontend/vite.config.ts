import path from "node:path";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vitest/config";

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    watch: {
      usePolling: true,
    },
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  build: {
    target: "es2020",
    rollupOptions: {
      output: {
        manualChunks: {
          react: ["react", "react-dom", "react-router-dom"],
          query: ["@tanstack/react-query"],
          ui: ["radix-ui", "class-variance-authority", "clsx", "tailwind-merge", "lucide-react"],
          i18n: ["i18next", "react-i18next"],
        },
      },
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: "./src/tests/setup.ts",
  },
});
