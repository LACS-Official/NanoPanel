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
  /** 不带 v 的版本号，如 0.1.2 */
  version: string;
  /** 带 v 的 tag，如 v0.1.2 */
  tag: string;
  /** 发布日期 YYYY-MM-DD，未知时为空串 */
  publishedAt: string;
  releaseUrl: string;
  assets: ReleaseAsset[];
  /** api = 完整信息，redirect = 仅解析到 tag，fallback = 站点配置 */
  source: "api" | "redirect" | "fallback";
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

/** 构建期解析最新 Release（同一进程内只查一次）。 */
export function getRelease(): Promise<ReleaseInfo> {
  if (cached) return cached;

  cached = (async () => {
    if (process.env.NP_RELEASE_SOURCE === "static") {
      console.log("[release] NP_RELEASE_SOURCE=static，跳过网络查询");
      return fromConfig();
    }

    try {
      const viaApi = await fromApi();
      if (viaApi) {
        console.log(`[release] GitHub API 解析到 ${viaApi.tag}（${viaApi.assets.length} 个资产）`);
        return viaApi;
      }
      console.warn("[release] GitHub API 不可用，尝试解析 releases/latest 重定向");
    } catch (err) {
      console.warn(`[release] GitHub API 请求失败: ${(err as Error).message}`);
    }

    try {
      const viaRedirect = await fromRedirect();
      if (viaRedirect) {
        console.log(`[release] 重定向解析到 ${viaRedirect.tag}`);
        return viaRedirect;
      }
      console.warn("[release] 重定向未解析出 tag");
    } catch (err) {
      console.warn(`[release] 重定向请求失败: ${(err as Error).message}`);
    }

    console.warn(`[release] 使用站点兜底版本 v${fallbackVersion}（离线构建）`);
    return fromConfig();
  })();

  return cached;
}
