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

`dist/` 是纯静态文件，可直接丢给 Nginx / Caddy / 对象存储托管；
官网当前托管在 **Vercel**（域名 `npanel.lacs.cc`），仓库不负责部署，推 `website/` 变更后由 Vercel 自动构建。

## 与项目 / GitHub 的联动

- **版本号不手写**：`src/lib/release.ts` 在构建时向 GitHub 查询最新 Release
  （API → `releases/latest` 302 重定向 → `site.ts` 兜底），页面上的版本号、下载地址、资产大小都来自真实 Release。
- **安装脚本不重复**：`scripts/sync-deploy.mjs` 在 `predev` / `prebuild` 时把
  `../deploy/install.sh` 与 `uninstall.sh` 复制到 `public/`，因此
  <https://npanel.lacs.cc/install.sh> 与仓库里的脚本永远一致。
- **发布即上线**：Vercel 连接本仓库，`website/` 有变更时自动重新构建并部署。

相关环境变量：

| 变量 | 作用 |
|---|---|
| `GITHUB_TOKEN` | 提高 GitHub API 速率上限（CI 自动注入） |
| `NP_RELEASE_SOURCE=static` | 跳过网络查询，使用 `site.ts` 兜底版本（离线构建 / CI 稳定性） |

## 目录

```
src/
├── config/site.ts        # 站点全部文案、版本兜底、Umami 配置
├── lib/release.ts        # GitHub Release 解析（唯一版本数据源）
├── layouts/
│   ├── BaseLayout.astro  # head、SEO、主题脚本、Umami、全局样式
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
    ├── sitemap.xml.ts    # 站点地图
    └── 404.astro
scripts/sync-deploy.mjs   # 同步 deploy/ 下的安装脚本
public/                   # robots.txt、favicon、赞助二维码、构建时生成的 install.sh
```

## 需要手工维护的地方

- 站点域名 / 站点地址：`src/config/site.ts` 的 `url`、`astro.config.mjs` 的 `site`
- 统计：`src/config/site.ts` 的 `analytics.umami`
- `release.fallbackVersion` 仅在完全离线构建时生效，平时无需改动
