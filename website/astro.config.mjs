import { defineConfig } from "astro/config";
import tailwind from "@astrojs/tailwind";

// NanoPanel 官网：纯静态构建，任意静态托管/反代均可部署。
export default defineConfig({
  site: "https://npanel.lacs.cc",
  output: "static",
  integrations: [tailwind()],
  server: {
    host: "0.0.0.0",
    port: 3001,
  },
  vite: {
    define: {
      __SITE_NAME__: JSON.stringify("NanoPanel"),
    },
  },
});
