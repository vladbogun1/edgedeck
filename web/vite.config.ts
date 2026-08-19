import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwind from "@tailwindcss/vite";

// Сборка кладётся в internal/webui/dist и вшивается в бинарник Go (см. Dockerfile).
// При локальной разработке /api уходит на панель, поднятую отдельно.
export default defineConfig({
  plugins: [react(), tailwind()],
  build: { outDir: "dist", emptyOutDir: true },
  server: {
    port: 5173,
    proxy: { "/api": "http://localhost:4000" },
  },
});
