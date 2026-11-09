# NanoPanel 官网

NanoPanel 官方网站，基于 Astro + Tailwind CSS，纯静态构建。

## 开发

```bash
npm install
npm run dev      # http://localhost:3001
```

## 构建

```bash
npm run build    # 产物在 dist/
npm run preview  # 本地预览产物
```

`dist/` 是纯静态文件，可直接丢给 Nginx / Caddy / 对象存储托管。

## 目录

```
src/
├── config/site.ts        # 站点全部文案与配置（改这里即可换内容）
├── layouts/
│   ├── BaseLayout.astro  # head、SEO、主题脚本、全局样式
│   └── MainLayout.astro  # 页头 + 页脚
├── components/
│   ├── Header.astro      # 顶部导航
│   ├── Footer.astro      # 页脚
│   ├── PanelPreview.astro# 面板界面示意（纯 SVG）
│   └── ThemeToggle.astro # 深浅色切换
└── pages/
    ├── index.astro       # 首页
    ├── features.astro    # 功能特性
    ├── download.astro    # 下载
    ├── docs/index.astro  # 部署文档
    └── 404.astro
```

## 需要同步的地方

- 发版后更新 `src/config/site.ts` 里的 `product.version` 与 `product.releaseDate`
- 下载链接指向 `https://github.com/LACS-Official/NanoPanel/releases/latest/download/<文件名>`，资产名需与 CI 产物一致
- 站点域名在 `astro.config.mjs` 的 `site` 字段
