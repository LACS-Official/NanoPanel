import type { APIRoute } from "astro";
import { SITE_CONFIG } from "@/config/site";
import { getRelease } from "@/lib/release";

export const GET: APIRoute = async () => {
  const release = await getRelease();
  const base = SITE_CONFIG.url.replace(/\/$/, "");
  const lastmod = release.publishedAt || new Date().toISOString().slice(0, 10);

  const pages = [
    { path: "/", priority: "1.0", changefreq: "weekly" },
    { path: "/features", priority: "0.8", changefreq: "monthly" },
    { path: "/download", priority: "0.9", changefreq: "weekly" },
    { path: "/docs", priority: "0.8", changefreq: "weekly" },
  ];

  const urls = pages
    .map(
      (p) => `  <url>
    <loc>${base}${p.path}</loc>
    <lastmod>${lastmod}</lastmod>
    <changefreq>${p.changefreq}</changefreq>
    <priority>${p.priority}</priority>
  </url>`,
    )
    .join("\n");

  const body = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
${urls}
</urlset>
`;

  return new Response(body, {
    headers: { "Content-Type": "application/xml; charset=utf-8" },
  });
};
