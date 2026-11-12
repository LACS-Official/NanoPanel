export const SITE_CONFIG = {
  // 基础信息
  name: "NanoPanel",
  englishName: "Nano Panel",
  description: "面向小内存 Linux 设备的单二进制轻量服务器运维面板",
  tagline: "一个二进制，管好一台服务器",
  subtitle: "为树莓派、软路由、N1 盒子与低配 VPS 而生",
  url: "https://npanel.lacs.cc",
  github: "https://github.com/LACS-Official/NanoPanel",

  // SEO
  keywords: [
    "领创工作室",
    "LAC Studio",
    "LACS",
    "NanoPanel",
    "nanopanel",
    "运维面板",
    "服务器面板",
    "Linux 面板",
    "Go 面板",
    "单二进制",
    "树莓派",
    "ARM64",
    "轻量面板",
    "自建服务器",
  ],
  author: "领创工作室",
  copyright: `© 2026 领创工作室（LAC Studio）· MIT License`,

  // 社交与开源
  social: {
    studio: "https://lacs.cc",
    github: "https://github.com/LACS-Official/NanoPanel",
    issues: "https://github.com/LACS-Official/NanoPanel/issues",
    releases: "https://github.com/LACS-Official/NanoPanel/releases",
    docs: "/docs",
  },

  /**
   * 版本数据源。官网构建时自动向 GitHub 查询最新 Release，
   * 这里的版本号仅在「完全离线 / API 与重定向都失败」时兜底。
   * 数据解析见 src/lib/release.ts。
   */
  release: {
    repo: "LACS-Official/NanoPanel",
    apiBase: "https://api.github.com",
    fallbackVersion: "0.1.4",
    fallbackDate: "2026-11-09",
  },

  // 产品信息
  product: {
    // 展示用兜底版本；页面实际使用 release.ts 解析出的真实版本
    version: "0.1.4",
    releaseDate: "2026-11-09",
    platforms: ["Linux amd64", "Linux arm64", "Linux armv7"],
    license: "MIT License",
    size: "约 12 MB",
    memory: "常驻 < 30 MB",
  },

  // 一键安装（全站唯一来源，install.sh 同步自 deploy/install.sh）
  install: {
    scriptUrl: "https://npanel.lacs.cc/install.sh",
    sourceUrl: "https://github.com/LACS-Official/NanoPanel/blob/main/deploy/install.sh",
    rawUrl:
      "https://raw.githubusercontent.com/LACS-Official/NanoPanel/main/deploy/install.sh",
    command: "curl -fsSL https://npanel.lacs.cc/install.sh | sudo bash",
    mirrorCommand:
      "export GITHUB_MIRROR=https://ghfast.top/https://github.com\ncurl -fsSL https://npanel.lacs.cc/install.sh | sudo bash",
  },

  // 站点统计（Umami）
  analytics: {
    umami: {
      enabled: true,
      scriptUrl: "https://umami.lacs.cc/script.js",
      websiteId: "952a5878-d4bd-4e17-9aea-9edb286d2089",
    },
  },

  // 导航
  navigation: [
    { name: "首页", href: "/" },
    { name: "功能特性", href: "/features" },
    { name: "文档", href: "/docs" },
    { name: "下载", href: "/download" },
  ],

  // 首页
  home: {
    hero: {
      ctaDownload: "立即下载",
      ctaGithub: "查看源码",
      note: "开源免费 · 单文件部署 · 无需数据库",
    },
    stats: [
      { value: "1", label: "个可执行文件", desc: "无依赖、无数据库、无容器" },
      { value: "<30MB", label: "常驻内存", desc: "512MB 内存设备也能流畅运行" },
      { value: "3", label: "种架构", desc: "amd64 / arm64 / armv7 全支持" },
      { value: "12", label: "类告警事件", desc: "Webhook 推送到你的手机" },
    ],
    features: [
      {
        title: "系统概览",
        desc: "CPU 实时使用率、内存与磁盘水位、温度与频率、运行概览一屏掌握。",
        icon: "dashboard",
      },
      {
        title: "进程与日志",
        desc: "进程列表带真实内存占用，日志中心按单元过滤、关键字搜索、实时跟踪。",
        icon: "list",
      },
      {
        title: "应用",
        desc: "mihomo、AdGuard Home、Tailscale、frp、CUPS… 一键安装、升级与配置。",
        icon: "apps",
      },
      {
        title: "文件管理",
        desc: "内置文件管理器，上传下载、在线编辑、新建与重命名，路径严格限制在允许的根目录内。",
        icon: "folder",
      },
      {
        title: "备份与定时任务",
        desc: "一键导出/恢复全部配置，cron 表达式式定时任务，执行结果推送通知。",
        icon: "clock",
      },
      {
        title: "流量与磁盘",
        desc: "网卡实时上下行速率曲线，磁盘挂载、格式化与 fstab 安全写入。",
        icon: "network",
      },
      {
        title: "安全加固",
        desc: "HTTPS 自签证书一键开启、登录失败锁定告警、面板日志滚动轮转。",
        icon: "shield",
      },
      {
        title: "自动更新",
        desc: "每日检查 GitHub Release，发现新版本推送通知，可选择自动下载并回滚保护式升级。",
        icon: "refresh",
      },
    ],
    quickstart: {
      title: "30 秒完成部署",
      desc: "一条命令识别架构、下载最新 Release、装好 systemd 服务并生成初始密码。",
      command:
        "curl -fsSL https://npanel.lacs.cc/install.sh | sudo bash",
    },
    why: {
      title: "为什么是单二进制",
      items: [
        {
          title: "没有运行时依赖",
          desc: "不装 Nginx、不装数据库、不拉 Docker 镜像，一个文件拷过去就能跑，卸载只需删掉它。",
        },
        {
          title: "对低配设备友好",
          desc: "前端资源全部嵌入二进制，页面走原生 DOM 渲染，512MB 内存的盒子上也不卡。",
        },
        {
          title: "升级就是换文件",
          desc: "新版本覆盖旧二进制后重启服务即可，面板内「立即升级」会自动完成备份与回滚。",
        },
      ],
    },
    cta: {
      title: "把服务器管起来，只要一个文件",
      desc: "在 GitHub 下载对应架构的二进制，或直接用一键脚本安装。",
      downloadText: "下载 NanoPanel",
      docsText: "查看部署文档",
    },
  },

  // 功能页
  featuresPage: {
    hero: {
      title: "该有的都有，不该有的都没有",
      subtitle: "为日常运维的高频操作做减法，每一个功能都为小内存设备重新设计过。",
    },
    sections: [
      {
        title: "监控：看得见才管得住",
        desc: "概览页展示 CPU 实时使用率、内存与磁盘水位、CPU 温度与频率区间，以及进程数、已安装应用、运行中服务、最高温度四项运行指标。",
        bullets: ["CPU 使用率双采样计算", "温度阈值告警（滞回防抖）", "网卡实时速率曲线", "磁盘挂载与容量水位"],
      },
      {
        title: "应用：装软件不用记命令",
        desc: "内置应用清单，安装前自动做架构、内存、磁盘预检；安装后卡片直接显示运行状态与网页入口，支持一键升级到清单新版本。",
        bullets: ["mihomo + metacubexd 管理面板", "AdGuard Home / Tailscale / frp", "CUPS 打印服务 / WebDAV / Samba", "已安装应用一键打开网页"],
      },
      {
        title: "运维：把重复劳动自动化",
        desc: "内置文件管理器、一键备份恢复、定时任务与 Webhook 通知，把日常的重复操作交给面板。",
        bullets: ["tar.gz 流式备份与恢复", "cron 三种调度表达式", "12 类事件 Webhook 推送", "面板日志 5MB × 3 轮转"],
      },
      {
        title: "安全：默认就不留后门",
        desc: "HMAC 无状态令牌、改密即轮换密钥、登录失败锁定告警；HTTPS 证书可在面板内一键生成并自动重启生效。",
        bullets: ["ECDSA P-256 自签证书（10 年）", "SAN 覆盖主机名与全部网卡 IP", "连续失败锁定 + 告警", "文件路径越界防护"],
      },
    ],
  },

  // 下载页
  downloadPage: {
    hero: {
      title: "下载 NanoPanel",
      subtitle: "选择设备架构，下载单个二进制文件即可运行。",
    },
    // 顺序与 release.ts 中 ARCH_ASSETS 一致（amd64 / arm64 / armv7）
    assets: [
      {
        id: "amd64",
        name: "x86_64 / amd64",
        arch: "x86_64",
        requirement: "普通 VPS、软路由、x86 小主机",
        file: "nanopanel-amd64",
      },
      {
        id: "arm64",
        name: "ARM64 / aarch64",
        arch: "aarch64",
        requirement: "树莓派 4/5、N1 盒子、ARM 云服务器",
        file: "nanopanel-arm64",
      },
      {
        id: "armv7",
        name: "ARMv7",
        arch: "armv7l",
        requirement: "树莓派 2/3、老式 ARM 开发板",
        file: "nanopanel-armv7",
      },
    ],
    install: {
      title: "一键安装（推荐）",
      note: "脚本会自动识别架构、从 GitHub Release 下载最新版本、校验 SHA256、安装 systemd 服务并生成初始密码。",
    },
    manual: {
      title: "手动安装",
      command:
        "wget https://github.com/LACS-Official/NanoPanel/releases/latest/download/nanopanel-amd64\nchmod +x nanopanel-amd64\nsudo install -m 0755 nanopanel-amd64 /opt/nanopanel/nanopanel\nsudo /opt/nanopanel/nanopanel -c /etc/nanopanel/config.json",
    },
  },
};
