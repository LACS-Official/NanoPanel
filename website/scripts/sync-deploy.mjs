/**
 * 把仓库里的部署脚本同步到官网 public/ 目录，
 * 让 https://npanel.lacs.cc/install.sh 与 deploy/install.sh 永远保持一致。
 * 由 npm 的 predev / prebuild 钩子自动执行，无需手工维护第二份脚本。
 */
import { copyFileSync, existsSync, mkdirSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const webRoot = resolve(here, "..");
const repoRoot = resolve(webRoot, "..");
const deployDir = join(repoRoot, "deploy");
const publicDir = join(webRoot, "public");

const files = ["install.sh", "uninstall.sh"];

mkdirSync(publicDir, { recursive: true });

for (const name of files) {
  const src = join(deployDir, name);
  const dest = join(publicDir, name);
  if (!existsSync(src)) {
    if (existsSync(dest)) {
      console.warn(`[sync] 未找到 ${src}，保留已有的 public/${name}`);
      continue;
    }
    throw new Error(`[sync] 缺少源文件 ${src}`);
  }
  copyFileSync(src, dest);
  const bytes = readFileSync(dest).byteLength;
  console.log(`[sync] deploy/${name} → public/${name} (${bytes} B)`);
}
