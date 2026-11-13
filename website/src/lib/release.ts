/**
 * 版本信息唯一数据源。
 *
 * 官网不再手写版本号：构建时直接向 GitHub 查询最新 Release，
 * 依次尝试 API → releases/latest 重定向 → 本地兜底配置。
 * 因此「打 tag → GitHub Release → 官网自动重建」是真正联动的。
 *
 * 网络层先走 Node 原生 fetch，失败时回退到系统 curl
 * （国内/代理环境下 fetch 往往不可达，而 curl 走代理是通的）。
 *
 * 可用环境变量：
 *   GITHUB_TOKEN         提高 API 速率上限（CI 里用 secrets.GITHUB_TOKEN）
 *   NP_RELEASE_SOURCE    = static 时跳过网络，强制使用兜底配置
 */
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";
import { existsSync, readFileSync } from "node:fs";
import { SITE_CONFIG } from "@/config/site";

export interface ReleaseAsset {
  /** 资产原始文件名，如 nanopanel-amd64 */
  name: string;
  /** 始终指向 latest 的下载地址 */
  url: string;
  sha256Url: string;
  /** 字节数，未知时为 null */
  size: number | null;
  sizeText: string;
}

export interface ReleaseInfo {
  /** 不带 v 的版本号，如 0.1.5 */
  version: string;
  /** 带 v 的 tag，如 v0.1.5 */
  tag: string;
  /** 发布日期 YYYY-MM-DD，未知时为空串 */
  publishedAt: string;
  releaseUrl: string;
  assets: ReleaseAsset[];
  /** api = 完整信息，redirect = 仅解析到 tag，fallback = 站点配置 */
  source: "api" | "redirect" | "fallback";
  /** 该版本对应的更新日志（HTML，已由 markdown 渲染），无则为 undefined */
  changelogHtml?: string;
}

const { repo, apiBase, fallbackVersion, fallbackDate } = SITE_CONFIG.release;
const UA = "NanoPanel-Website";

/** 三个架构的固定资产名，与 .github/workflows/release.yml 保持一致 */
export const ARCH_ASSETS = ["nanopanel-amd64", "nanopanel-arm64", "nanopanel-armv7"] as const;

function formatSize(bytes: number | null): string {
  if (!bytes || bytes <= 0) return "—";
  const mb = bytes / 1024 / 1024;
  return mb >= 1 ? `${mb.toFixed(1)} MB` : `${Math.max(1, Math.round(bytes / 1024))} KB`;
}

function buildAsset(name: string, size: number | null): ReleaseAsset {
  // 用 latest 路径而不是带 tag 的路径：即使页面被缓存，链接也不会失效。
  const url = `https://github.com/${repo}/releases/latest/download/${name}`;
  return { name, url, sha256Url: `${url}.sha256`, size, sizeText: formatSize(size) };
}

function cleanTag(tag: string): string {
  return tag.trim().replace(/^v/i, "");
}

function toDate(value: string | undefined): string {
  if (!value) return "";
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? "" : d.toISOString().slice(0, 10);
}

/** 构建机上的 curl 回退方案，返回 stdout 文本。 */
function curlText(args: string[], timeoutMs = 15000): string | null {
  try {
    return execFileSync("curl", args, {
      encoding: "utf8",
      timeout: timeoutMs,
      stdio: ["ignore", "pipe", "ignore"],
      windowsHide: true,
      maxBuffer: 4 * 1024 * 1024,
    });
  } catch {
    return null;
  }
}

async function getText(url: string, headers: Record<string, string>): Promise<string | null> {
  try {
    const res = await fetch(url, {
      headers,
      signal: AbortSignal.timeout(8000),
    });
    if (res.ok) return await res.text();
    console.warn(`[release] HTTP ${res.status}: ${url}`);
  } catch (err) {
    console.warn(`[release] fetch 失败（${(err as Error).message}），改用 curl`);
  }

  const args = ["-fsSL", "-m", "15", "-H", `User-Agent: ${UA}`];
  for (const [k, v] of Object.entries(headers)) {
    if (k.toLowerCase() === "user-agent") continue;
    args.push("-H", `${k}: ${v}`);
  }
  args.push(url);
  return curlText(args);
}

async function fromApi(): Promise<ReleaseInfo | null> {
  const headers: Record<string, string> = {
    Accept: "application/vnd.github+json",
    "User-Agent": UA,
  };
  const token = process.env.GITHUB_TOKEN;
  if (token) headers.Authorization = `Bearer ${token}`;

  const text = await getText(`${apiBase}/repos/${repo}/releases/latest`, headers);
  if (!text) return null;

  let data: {
    tag_name?: string;
    published_at?: string;
    html_url?: string;
    assets?: Array<{ name?: string; size?: number }>;
  };
  try {
    data = JSON.parse(text);
  } catch {
    console.warn("[release] API 返回内容不是 JSON");
    return null;
  }

  const tag = data.tag_name?.trim();
  if (!tag) return null;

  const sizeOf = new Map<string, number>();
  for (const a of data.assets ?? []) {
    if (a.name && typeof a.size === "number") sizeOf.set(a.name, a.size);
  }

  return {
    version: cleanTag(tag),
    tag,
    publishedAt: toDate(data.published_at),
    releaseUrl: data.html_url || `${SITE_CONFIG.social.releases}/tag/${tag}`,
    assets: ARCH_ASSETS.map((name) => buildAsset(name, sizeOf.get(name) ?? null)),
    source: "api",
  };
}

/** GitHub 对匿名 API 限流较严，退回解析 releases/latest 的 302 Location。 */
async function fromRedirect(): Promise<ReleaseInfo | null> {
  let location = "";

  try {
    const res = await fetch(`https://github.com/${repo}/releases/latest`, {
      redirect: "manual",
      headers: { "User-Agent": UA },
      signal: AbortSignal.timeout(8000),
    });
    location = res.headers.get("location") ?? "";
  } catch {
    // 交给 curl
  }

  if (!location) {
    // 注意：不要用 -o /dev/null，Windows 下 execFileSync 直连 curl.exe 时该路径无效
    const raw = curlText([
      "-sSI",
      "-m",
      "15",
      "-H",
      `User-Agent: ${UA}`,
      `https://github.com/${repo}/releases/latest`,
    ]);
    const matches = raw?.match(/^location:\s*(\S+)\s*$/gim) ?? [];
    location = matches.length ? matches[matches.length - 1].replace(/^location:\s*/i, "").trim() : "";
  }

  const match = location.match(/\/tag\/([^/?#]+)$/);
  if (!match) return null;

  const tag = decodeURIComponent(match[1]);
  return {
    version: cleanTag(tag),
    tag,
    publishedAt: "",
    releaseUrl: `${SITE_CONFIG.social.releases}/tag/${tag}`,
    assets: ARCH_ASSETS.map((name) => buildAsset(name, null)),
    source: "redirect",
  };
}

function fromConfig(): ReleaseInfo {
  return {
    version: fallbackVersion,
    tag: `v${fallbackVersion}`,
    publishedAt: fallbackDate,
    releaseUrl: SITE_CONFIG.social.releases,
    assets: ARCH_ASSETS.map((name) => buildAsset(name, null)),
    source: "fallback",
  };
}

let cached: Promise<ReleaseInfo> | null = null;

/**
 * 定位仓库根目录的 CHANGELOG.md。
 * 构建时官网与仓库同仓：website 的 cwd 是 website 目录，故优先试 ../CHANGELOG.md，
 * 再回退到从本模块位置逐级向上查找，最后试 cwd 本身。
 */
function findChangelogPath(): string | null {
  const candidates: string[] = [
    resolve(process.cwd(), "../CHANGELOG.md"),
    resolve(process.cwd(), "CHANGELOG.md"),
  ];
  let dir = dirname(fileURLToPath(import.meta.url));
  for (let i = 0; i < 8; i++) {
    candidates.push(resolve(dir, "CHANGELOG.md"));
    const parent = dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  for (const p of candidates) {
    if (existsSync(p)) return p;
  }
  return null;
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

/** 行内 markdown：链接 / 加粗 / 行内代码（先转义，再套标记，杜绝 HTML 注入）。 */
function inlineMd(s: string): string {
  let t = escapeHtml(s);
  t = t.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (_m, text: string, url: string) => {
    // url 已被 escapeHtml 处理过，可安全放入属性
    return `<a href="${url}" target="_blank" rel="noopener noreferrer">${text}</a>`;
  });
  t = t.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  t = t.replace(/`([^`]+)`/g, "<code>$1</code>");
  return t;
}

/**
 * 把 CHANGELOG 的小节（我们自己的有限子集）渲染成 HTML：
 * ### 标题 / ## 标题 / - 列表 / 段落，配合行内标记。
 * 不引入第三方 markdown 库，输出可直接交给 @tailwindcss/typography 的 prose 排版。
 */
export function renderChangelogMarkdown(md: string): string {
  const lines = md.split(/\r?\n/);
  const out: string[] = [];
  let inList = false;
  const closeList = () => {
    if (inList) {
      out.push("</ul>");
      inList = false;
    }
  };
  for (const raw of lines) {
    const line = raw.replace(/\s+$/, "");
    if (line.trim() === "") {
      closeList();
      continue;
    }
    if (/^###\s+/.test(line)) {
      closeList();
      out.push(`<h3>${inlineMd(line.replace(/^###\s+/, ""))}</h3>`);
      continue;
    }
    if (/^##\s+/.test(line)) {
      closeList();
      out.push(`<h2>${inlineMd(line.replace(/^##\s+/, ""))}</h2>`);
      continue;
    }
    if (/^[-*]\s+/.test(line)) {
      if (!inList) {
        out.push("<ul>");
        inList = true;
      }
      out.push(`<li>${inlineMd(line.replace(/^[-*]\s+/, ""))}</li>`);
      continue;
    }
    closeList();
    out.push(`<p>${inlineMd(line)}</p>`);
  }
  closeList();
  return out.join("\n");
}

/**
 * 从 CHANGELOG.md 文本中截取指定版本的小节正文（不含版本标题行）。
 * 优先精确匹配版本号；找不到时回退到「未发布」小节，再回退到第一个版本小节。
 */
function extractChangelogSection(md: string, version: string): string | null {
  const lines = md.split(/\r?\n/);
  const safeVer = version.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const verRe = new RegExp(`^##\\s+\\[?${safeVer}\\]?\\b`);

  let start = -1;
  for (let i = 0; i < lines.length; i++) {
    if (verRe.test(lines[i])) {
      start = i;
      break;
    }
  }
  if (start === -1) {
    for (let i = 0; i < lines.length; i++) {
      if (/^##\s+\[?未发布\]?/.test(lines[i])) {
        start = i;
        break;
      }
    }
  }
  if (start === -1) {
    for (let i = 0; i < lines.length; i++) {
      if (/^##\s+/.test(lines[i])) {
        start = i;
        break;
      }
    }
  }
  if (start === -1) return null;

  let end = lines.length;
  for (let i = start + 1; i < lines.length; i++) {
    if (/^##\s+/.test(lines[i])) {
      end = i;
      break;
    }
  }
  const section = lines.slice(start + 1, end).join("\n").trim();
  return section || null;
}

/** 读取仓库 CHANGELOG.md，渲染当前版本的更新日志 HTML（失败返回 undefined）。 */
function getChangelogHtml(version: string): string | undefined {
  const path = findChangelogPath();
  if (!path) {
    console.warn("[release] 未找到 CHANGELOG.md，跳过更新日志渲染");
    return undefined;
  }
  try {
    const md = readFileSync(path, "utf8");
    const section = extractChangelogSection(md, version);
    if (!section) {
      console.warn(`[release] CHANGELOG.md 中未找到版本 ${version} 的小节`);
      return undefined;
    }
    return renderChangelogMarkdown(section);
  } catch (err) {
    console.warn(`[release] 读取/渲染 CHANGELOG.md 失败: ${(err as Error).message}`);
    return undefined;
  }
}

/** 构建期解析最新 Release（同一进程内只查一次）。 */
export function getRelease(): Promise<ReleaseInfo> {
  if (cached) return cached;

  cached = (async () => {
    let r: ReleaseInfo;
    if (process.env.NP_RELEASE_SOURCE === "static") {
      console.log("[release] NP_RELEASE_SOURCE=static，跳过网络查询");
      r = fromConfig();
    } else {
      try {
        const viaApi = await fromApi();
        if (viaApi) {
          console.log(`[release] GitHub API 解析到 ${viaApi.tag}（${viaApi.assets.length} 个资产）`);
          r = viaApi;
        } else {
          console.warn("[release] GitHub API 不可用，尝试解析 releases/latest 重定向");
          const viaRedirect = await fromRedirect();
          if (viaRedirect) {
            console.log(`[release] 重定向解析到 ${viaRedirect.tag}`);
            r = viaRedirect;
          } else {
            console.warn("[release] 重定向未解析出 tag，使用站点兜底版本");
            r = fromConfig();
          }
        }
      } catch (err) {
        console.warn(`[release] GitHub 查询失败（${(err as Error).message}），使用站点兜底版本`);
        r = fromConfig();
      }
    }

    const html = getChangelogHtml(r.version);
    if (html) {
      r.changelogHtml = html;
      console.log(`[release] 已渲染 v${r.version} 更新日志`);
    }
    return r;
  })();

  return cached;
}
