# Sentinel-Go 架构设计文档

> 对原 `IP-Sentinel`（bash + python）项目的 Go 完整重写。
> 目标：单二进制、无外部脚本依赖、逻辑清晰、可测试、可维护。
> 本文档为**动手写码前的方案确认稿**，确认后才进入编码阶段。

---

## 0. 原项目功能回顾（去营销黑话版）

原项目是一个 **VPS IP 地理位置/信誉「养号」工具**，解决 VPS 的 IP 被 Google 等服务误判到中国大陆/香港（作者称「送中」）的问题。三个核心行为：

| 模块 | 原文件 | 实际干的事 |
|------|--------|-----------|
| Google 纠偏 | `mod_google.sh` | 带随机 UA + cookie + 伪造 GPS 坐标，模拟真人访问 Google 搜索/新闻/地图，并探测当前 IP 被定位到哪个国家 |
| 信用净化 | `mod_trust.sh` | 访问维基/苹果/微软等高信誉站点，积累访客信誉 |
| IP 质量体检 | `mod_quality.sh` | 调用第三方脚本 `xykt/IPQuality` 做欺诈分/流媒体解锁检测 |
| 边缘守护 | `agent_daemon.sh` | Python HTTP 服务（默认 9527，自签 TLS），接收 Master 的远程指令 |
| 中枢 | `master/tg_master.sh` | Telegram 机器人 + SQLite，注册/管理/遥控所有 Agent |

架构：**Master（一台）↔ Telegram Bot ↔ 用户；Master → HTTPS+HMAC → 多台 Agent 的 webhook 端口**。

---

## 1. 重写决策（已与用户确认）

1. **Agent + Master 全部用 Go 重写**，编译为单二进制。
2. **全新独立系统**，不与原 bash 协议兼容 → 协议/数据库/配置格式全部重新设计得更干净。
3. **Master ↔ Telegram 沿用 getUpdates 长轮询**（无需公网域名/证书）。
4. **只做私有部署，彻底移除「官方公共网关」模式。**
   - 理由：所谓公共网关会让所有用户的 chat_id、节点 IP、下发指令全部经过第三方中转服务器，等于把整个舰队的控制权与元数据交给他人，且该模式还强制禁用用户自己的 OTA 权限。私有部署下，Bot Token / chat_id / 数据库全部在用户自己手里，无任何第三方中间人。
5. **IP 质量体检模块 1:1 完整复刻** 原 `xykt/IPQuality` 的全部检测项（见 §7），但用 Go 原生实现，不再运行时下载外部脚本。
6. **彻底去除原项目的营销黑话**（如「深海声呐」「叹息之墙」「金蝉脱壳」「送中」等），全部改为中性、准确的技术命名，日志与 UI 文案同步中性化。术语对照见附录 A。

---

## 2. 顶层设计

### 2.1 单二进制 + 子命令

```
sentinel agent    # 以 Agent 模式运行（边缘节点）
sentinel master   # 以 Master 模式运行（中枢）
sentinel install  # 交互式安装（生成配置 + 注册 systemd/cron）
sentinel version
```

一份二进制同时含 Agent 和 Master，靠子命令区分角色。好处：分发简单、OTA 只需替换一个文件。

### 2.2 目录结构

```
sentinel-go/
├── go.mod
├── cmd/
│   └── sentinel/main.go            # 入口，解析子命令
├── internal/
│   ├── config/                     # 配置加载/保存 (agent.yaml / master.yaml)
│   │   └── config.go
│   ├── logx/                       # 统一日志（结构化，可选 syslog）
│   ├── netx/                       # 出网工具：绑定 interface、v4/v6 偏好、公网 IP 探测
│   │   └── client.go
│   ├── geo/                        # 地图数据 (map.json)、region 锚点、关键词加载
│   │   ├── mapdata.go
│   │   └── regions.go
│   ├── ctrl/                       # 控制通道 (§11)：EWP 长连接封装
│   │   ├── message.go              # 应用层消息编解码 (cmd/evt JSON)
│   │   ├── client.go               # Agent 侧：拨号、重连、心跳、指令分发
│   │   └── server.go               # Master 侧：accept、在线表、下发/等待回执
│   ├── agent/
│   │   ├── agent.go                # Agent 生命周期、注册、心跳
│   │   ├── scheduler.go            # 内建定时器（替代 cron/systemd timer）
│   │   └── modules/
│   │       ├── google.go           # Google 纠偏 + 定位探测
│   │       ├── trust.go            # 信用净化
│   │       └── quality.go          # IP 质量体检
│   ├── master/
│   │   ├── master.go               # Master 生命周期
│   │   ├── store/                  # SQLite 数据层
│   │   │   ├── store.go
│   │   │   └── schema.sql
│   │   ├── telegram/               # TG Bot API 客户端 + 长轮询
│   │   │   ├── client.go
│   │   │   └── types.go
│   │   ├── ui/                     # Inline Keyboard 面板渲染
│   │   │   └── panels.go
│   │   └── handlers.go             # 回调/命令路由（经 ctrl.Server 下发指令）
│   ├── protocol/                   # Agent↔Master 共享：注册报文编解码、消息类型常量
│   │   └── register.go
│   └── install/                    # 交互式安装、systemd 注入、密钥/UUID 生成
│       └── install.go
├── data/                           # 内嵌数据（go:embed）
│   ├── map.json
│   ├── user_agents.txt
│   ├── keywords/kw_*.txt
│   └── regions/**/*.json
└── DESIGN.md
```

**关键改进点 vs 原项目**：
- 数据文件（map/UA/keywords/regions）用 `go:embed` 打进二进制，不再运行时 curl 拉取，杜绝「被墙拉不到就崩」。可保留一个可选的 OTA 数据更新机制。
- 调度不再依赖外部 cron / systemd timer，用 Go 内建 `time.Ticker` + jitter；systemd 只负责「保活这个进程」。

---

## 3. 配置格式（全新，YAML）

原项目用 `config.conf`（shell 变量）。新版用 YAML，字段语义化。

### 3.1 Agent 配置 `~/.config/sentinel/agent.yaml`（或 `/etc/sentinel/agent.yaml`）

```yaml
node:
  name: "tokyo-a1b2"        # 不可变主键（自动生成：hostname+ip hash）
  alias: "东京-1"            # 展示名，可远程改
region:
  code: "JP"                # 国家码
  name: "Japan (日本)"
  lat: 35.6762              # 基准坐标
  lon: 139.6503
  keyword_file: "kw_JP.txt"
network:
  bind_ip: ""              # 出口 IP，空=系统默认路由
  ip_pref: 4               # 4 / 6
modules:
  google: true
  trust: true
master:
  enabled: true
  mode: "private"          # private / official
  tg_token: "..."          # official 模式下为空，走网关
  tg_api_url: "https://api.telegram.org/bot<token>"
  chat_id: "123456789"     # 同时作为 HMAC 预共享密钥
  ota: true
webhook:
  port: 9527
schedule:
  interval: "20m"          # 养护巡逻间隔
  jitter: "180s"           # 随机抖动
```

### 3.2 Master 配置 `master.yaml`

```yaml
master:
  node_name: "HQ"
  version: "1.0.0"
telegram:
  token: "..."
  enable_ota: true
store:
  path: "/var/lib/sentinel/master.db"
```

---

## 4. Agent↔Master 通信协议（全面重设计 —— 反转连接 + 加密长连接）

> 详细的加密/连接架构见 §11。本节只描述**应用层协议**（在加密流之上跑什么消息）。

**关键变化 vs 原项目**：原项目是 Master 主动去连每个 Agent 的公网 webhook 端口（傻、要公网 IP、要开端口、NAT 后不可用、TLS 形同虚设）。
新版**反转连接方向**：**Agent 主动拨号连 Master**，建立一条 EWP/v2.1 加密常驻长连接；Master 通过这条已建立的连接下发指令，Agent 在同一条连接上回结果。Agent **不再需要任何公网端口或入站放行**。

### 4.1 首次注册（带外，一次性）

Agent 首次安装后仍生成一条注册凭据交给用户转发给机器人（把 Agent 的身份 UUID 登记进 Master 的白名单）。这一步是**带外**的（走 Telegram），仅用于让 Master「认识」这个新 Agent 的 UUID 并入库。

- 注册报文 = `SENTINEL-REG:` + Base64(JSON)：

```json
{
  "v": 1,
  "region": "JP",
  "node": "tokyo-a1b2",
  "alias": "东京-1",
  "uuid": "11111111-2222-3333-4444-555555555555",  // Agent 身份 (EWP PSK)，注册后进 Master 白名单
  "ota": true
}
```
- 相比原版：**删掉了 `ips` 和 `port`** —— 因为不再由 Master 反向连接 Agent，Agent 的 IP/端口对通信不再重要（IP 只作为展示信息，由 Agent 在长连接建立后上报）。

### 4.2 长连接上的应用层消息（双向）

一条 EWP SecureStream 建立后，双方交换**长度前缀 + JSON 消息**（在加密流内，明文对外不可见）。消息类型：

**Master → Agent（指令，`cmd`）**：

| cmd | 动作 |
|-----|------|
| `run`            | 触发一轮保活调度 |
| `mod.google`     | 单独触发 Google 区域纠偏 |
| `mod.trust`      | 单独触发信誉预热 |
| `mod.quality`    | 触发 IP 质量检测 |
| `report`         | 生成并回传战报 |
| `log`            | 回传最近日志切片 |
| `config.rename`  | 改别名（参数 alias） |
| `config.toggle`  | 开关模块（参数 mod/state） |
| `ota`            | 触发自我升级（需本地 `ota: true`） |

**Agent → Master（上报，`evt`）**：

| evt | 含义 |
|-----|------|
| `hello`          | 连接建立后首帧：上报 node/alias/region/ip/version/模块状态（替代原注册报文里的 IP/port 部分） |
| `heartbeat`      | 定期心跳（保活 + 在线判定） |
| `result`         | 某条指令的执行结果（对应 cmd 的回执） |
| `report`         | 主动/被动的战报 payload |
| `quality`        | IP 质量检测结果（含入趋势库所需指标） |
| `log`            | 日志切片 |

- 每条指令带一个 `id`，Agent 的 `result` 回执带同 `id`，Master 侧可等待/超时。
- **鉴权**：不再需要应用层 HMAC —— 身份与完整性已由 EWP 握手（UUID-PSK + 服务端静态密钥）+ 每帧 AEAD 保证。应用层消息是纯业务 JSON。

### 4.3 在线状态与故障转移

- Master 维护「已连接 Agent」的内存表（`uuid → 活跃 SecureStream`）。
- 连接断开 → Master 立即标记该节点**离线**，Bot 面板显示离线。
- Agent 侧断线自动重连（指数退避）。
- 原版「多宿主 IP 容灾」概念**整个删除** —— 反转连接下，Agent 从哪个 IP 出去连 Master 都行，不存在「Master 连不上 Agent 某个 IP」的问题。

### 4.4 指令执行安全约束（RCE 防护，硬性规则）

> 威胁模型：EWP 已保证「只有合法对端能发消息进来」，但**不假设消息内容一定可信**（Master 被攻破、或消息被恶意构造）。Agent 对收到的任何指令必须按「零信任内容」处理。以下为不可违反的实现约束，配套 §12 的 fuzz 测试逐条锁定。

**SR-1　指令是封闭枚举，不是自由字符串**
`cmd` 字段只允许 §4.2 表内的固定常量。分发器用 `switch` 精确匹配，命中才执行；`default` 分支**一律丢弃并记日志**，绝不回落到任何"动态执行"路径。

**SR-2　零 Shell、零动态代码**
- 全程**禁止** `sh -c` / `bash -c` / `os/exec` 拼接字符串命令。
- 确需外部进程时（仅 OTA 重启 service 等极少数场景）用 `exec.Command(fixedBin, fixedArgs...)`，参数为**编译期固定**或经严格校验的数组元素，绝不含消息原文。
- 不 `eval`、不反射调用、不下载任何可执行内容后运行。

**SR-3　访问目标全部白名单化（防 C2 投毒）**
- Google 探测 URL、trust 站点、quality 数据源 API —— **全部是编译期固定的域名常量**（`internal/*/targets.go`）。
- 消息里**没有任何字段**能指定"要访问哪个 URL / 下载什么"。region/keyword 只能**索引**内嵌数据集里的既有条目，不能携带外部 URL。
- quality 模块**绝不**复刻原版「curl 下载 xykt 脚本再执行」的行为 —— 检测逻辑全部内建 Go 代码。

**SR-4　参数强类型 + 严格白名单校验**（校验不过 = 拒绝执行）
| 参数 | 规则 |
|------|------|
| `alias` | UTF-8 合法；长度 ≤ 20 符文；字符集限 `[中文/字母/数字/-]`；剔除控制字符 |
| `mod`   | 只能 `google` 或 `trust` |
| `state` | 只能 `true` 或 `false` |
| `id`    | 有界整数/短字符串，仅用于回执配对，不进入任何执行逻辑 |

**SR-5　解析器加固（防混淆式 JSON）**
- 消息读取有**最大长度上限**（如 64 KiB），超限直接断连。
- JSON 解码启用 `Decoder.DisallowUnknownFields()`：夹带未知字段 = 拒绝。
- 字符串字段先限长再校验语义；数值字段限范围。
- **先完整解析 + 校验，后执行**（parse-then-act，绝不边解析边产生副作用）。
- 解析失败**永不 panic**，返回错误并可选择断连；单条坏消息不得影响连接上的其他消息。

**SR-6　最小权限执行**
- OTA 等敏感指令二次校验本地策略（`ota: true` 且配置允许）后才动作。
- 危险操作（OTA、卸载）在 Agent 侧同样做本地确认门槛，不因一条远程消息就不可逆地改机器。

---

## 5. Agent 养护逻辑（核心，忠实还原 + 清理）

### 5.1 调度器 `scheduler.go`
- 每 `interval`（默认 20m）触发一次；启动时读交互式终端标志跳过 jitter（对应原 `[ -t 1 ]`）。
- 进程内锁：同一时刻只跑一个养护任务（对应 flock）。
- **概率轮盘**：两模块都开时，70% Google / 30% Trust（沿用原比例，做成常量可调）。

### 5.2 Google 模块 `google.go`
还原要点：
- **Hash-Seeded 指纹**：`seed = crc32(公网IP)`，从 UA 池取固定 3 个作为本节点设备组，每次会话随机选一个。稳定不漂移。
- **平台识别**：从 UA 推断 windows/android/ios/macos/linux，不同平台走不同「动作矩阵」（含各平台专属探针 URL，如 Android 走 `connectivitycheck.gstatic.com`，Apple 走 `captive.apple.com`）。
- **坐标微抖动**：基准坐标 + 随机偏移生成「咖啡馆坐标」，动作间再二次微抖。
- **持久化 Cookie**：`cookies/google_<hash>.txt`（Go 用 `http.CookieJar` + 落盘），14 天清理。
- **同业务 Referer 链**：Search/News/Maps/Eco 各自维护 Referer，70% 概率携带。
- **会话**：5~8 个动作，动作间 sleep 45~75s。
- **定位探测（三核交叉）**：
  1. `http://www.google.com/` 的 302 Location → 解析 `gl=` 或 `google.com.hk` 等域名后缀 → 国家码
  2. `youtube.com/premium` HTML 提取 `contentRegion/countryCode/GL`
  3. `music.youtube.com` 同上
  - 裁决：任一探针返回 CN → 标记「区域异常（被判定为 CN）」；YT 命中目标区 → 「区域正常」；否则「区域漂移」。用中性现代 Chrome UA 发探针（对应原 PROBE_UA）。

### 5.3 Trust 模块 `trust.go`
- 从 region JSON 读白名单 URL（`trust_module.white_urls`），兜底用 wikipedia/apple/microsoft。
- 3~6 步随机访问，带真实浏览器头（Sec-Fetch-*、Accept-Language 等）。
- 泊松长尾 sleep（45%:8-20s / 35%:20-60s / 15%:60-180s / 5%:180-480s）。
- Hash-Seeded UA、持久化 cookie 同 Google。

### 5.4 Quality 模块 `quality.go`（1:1 完整复刻 xykt/IPQuality）

用 Go 原生实现，**不再运行时下载外部脚本**，完整复刻 xykt 的六大检测模块与 JSON 输出结构。输出保持与原 `output.json` 一致的 schema（便于对拍验证），并在此基础上生成 Telegram Markdown 战报。

内部包结构：`agent/modules/quality/`，按数据源拆分文件，每个数据源一个 `Provider`，统一聚合器并发查询、超时容错、空值降级。

**六大模块（对齐 xykt）**：

1. **基础信息 Info**（Maxmind 数据库 / GeoIP）
   - ASN、Organization、经纬度、DMS、TimeZone、City（Name/PostalCode/SubCode/Subdivisions）、Region（Code/Name）、Continent、RegisteredRegion、地理一致性 Type（Geo-consistent 等）。
2. **IP 类型 Type**
   - `Usage` / `Company` 使用类型，多源交叉：IPinfo、ipregistry、ipapi、AbuseIPDB、IP2Location。
3. **风险评分 Score**（0 为最优）
   - IP2Location、Scamalytics、ipapi(风险率%)、AbuseIPDB、IPQS、DB-IP、Cloudflare。带 key 的（AbuseIPDB/IPQS 等）在配置中填入，无 key 则显示 N/A（降级不崩）。
4. **风险因子 Factor**（多源布尔矩阵）
   - CountryCode、Proxy、Tor、VPN、Server、Abuser、Robot —— 每项跨 9 个数据源（IP2Location/ipapi/ipregistry/IPQS/Scamalytics/ipdata/IPinfo/IPWHOIS/DB-IP）。
5. **流媒体 / AI 解锁 Media**
   - TikTok、Disney+、Netflix、YouTube、AmazonPrimeVideo、Reddit（原 Spotify 已于 2026-01 被 xykt 换成 Reddit）、ChatGPT。
   - 每项含 Status（Yes/Block/…）、Region、Type（Native/DNS/…）。解锁探测逻辑参考 lmc999 RegionRestrictionCheck 的思路，用 Go HTTP 复刻。
6. **邮局 Mail**
   - Port25 出站连通性；12 家邮局连通性（Gmail/Outlook/Yahoo/Apple/QQ/Mail.ru/AOL/GMX/Mail.com/163/Sohu/Sina）；DNSBlacklist（Total/Clean/Marked/Blacklisted，400+ 黑名单库）。

**输出 JSON schema**（与 xykt `output.json` 完全一致）：`Head / Info / Type / Score / Factor / Media / Mail`。

**后续处理**：
- 组装 Markdown 战报回传 Telegram（本地 chat_id）。
- 关键指标（Scamalytics 分、YouTube 区域=Google 定位、Netflix 状态、ChatGPT 状态）回传 Master 入趋势库。
- 送中判定：YouTube Region == CN 或状态含「中国」→ 标记区域异常。

**说明**：这是重写中工作量最大的模块。数据源众多，其中部分（Maxmind 本地库、带 key 的商业 API）需配置或内嵌 db 文件；无凭证的源走免费额度并做速率/容错处理。具体每个 Provider 的实现细节在编码阶段逐个落地并对拍 xykt 输出。

### 5.5 出网抽象 `netx`
- 统一构造 `*http.Client`：绑定 `bind_ip`（`net.Dialer.LocalAddr`）、强制 v4/v6、超时、UA、cookie jar。
- 网卡存活校验（bind_ip 丢失自动降级默认路由，对应原逻辑）。
- 过滤 WARP/TUN 假公网（原项目提到，需确认判定规则）。

---

## 6. Master 逻辑

### 6.1 SQLite 表（重设计，字段与原一致但更规范）

```sql
CREATE TABLE nodes (
  chat_id     TEXT NOT NULL,
  node_name   TEXT NOT NULL,
  node_alias  TEXT,
  region      TEXT DEFAULT 'UNKNOWN',
  ips         TEXT,               -- JSON 数组
  port        INTEGER,
  last_seen   DATETIME,
  enable_google INTEGER DEFAULT 1,
  enable_trust  INTEGER DEFAULT 1,
  enable_ota    INTEGER DEFAULT 0,
  PRIMARY KEY (chat_id, node_name)
);

CREATE TABLE ip_trend_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  node_name  TEXT,
  check_time DATETIME DEFAULT CURRENT_TIMESTAMP,
  scam_score INTEGER,
  goog_status TEXT,
  nf_status   TEXT,
  gpt_status  TEXT
);
```
- 用 `modernc.org/sqlite`（纯 Go，无 CGO，交叉编译方便），开启 WAL。
- 用参数化查询（`?` 占位符）彻底消灭原项目那种 `tr -cd` 正则洗值的 SQL 注入隐患。

### 6.2 Telegram 长轮询 `telegram/`
- `getUpdates?offset&timeout=30` 循环；offset 持久化到 DB 或文件。
- 解析 message / callback_query。

### 6.3 交互面板 `ui/` + `handlers.go`
还原原版全部交互：
- `/start` `/menu`：主面板（版本、节点数、全局巡逻/简报/OTA 按钮）
- 全球雷达 → 按 region 分组（含国旗 emoji）→ 节点列表 → 单节点控制台
- 单节点控制台：触发 google/trust/quality、看趋势、拉日志、生成战报、开关模块、改名、OTA、删除
- 二次确认（OTA / 删除）
- force_reply 改名回执解析
- 注册报文解析入库
- 战报回传时的「录入趋势库」按钮回调（原 `svq|...` 明文拼接，新版改为 callback_data 里带轻量 token，Master 端凭 token 从内存/临时表取回完整指标，避免把数据塞进 callback）

### 6.4 指令下发 `dispatch/`
- 生成签名 URL、多 IP 容灾重试（对应 `generate_signed_url` + `call_agent`）。

---

## 7. 第三方依赖清单（Go）

| 用途 | 库 |
|------|-----|
| SQLite（纯 Go，免 CGO） | `modernc.org/sqlite` |
| YAML 配置 | `gopkg.in/yaml.v3` |
| GeoIP（Maxmind mmdb 读取） | `oschwald/geoip2-golang`（Quality 模块基础信息用） |
| 命令行子命令 | 标准库 `flag`（够用）或 `spf13/cobra`（§9 待定） |
| 其余（HTTP/HMAC/crypto/tls/embed/cookiejar） | 全部标准库 |

尽量少依赖，保持「单二进制、易交叉编译（CGO_ENABLED=0）」。
Quality 模块的多数据源查询全部走标准库 `net/http`。

---

## 8. 部署与运维

- `sentinel install`（Agent）：交互式（复刻原 4 级区域选择 → 模块 → 填 Master 地址与静态公钥），生成 agent.yaml + 生成本机 UUID + 打印注册报文，写 systemd unit（**只一个**：`sentinel-agent.service`，`Type=simple Restart=always`，进程内自带调度 + 常驻控制连接 + 重连）。
- `sentinel master init` / `sentinel master`（Master）：生成/加载静态密钥对，启动 EWP 服务端监听 + Telegram 长轮询。
- 非 systemd（Alpine/OpenVZ）：`@reboot` + 保活或 nohup（进程内调度，无需 cron 逐条注入）。
- OTA：下载新二进制 → 校验（大小/可执行/`sentinel version` 自检）→ 原子替换 → 重启 service。比原版「拉一堆 .sh 覆盖」安全得多。
- **防火墙**：**只 Master 需要放行**其 EWP 监听端口（如 8443）；**Agent 不再需要任何入站放行**（纯出站连接）。这是相对原版的重大运维简化。

---

## 9. 技术选型（已全部确认）

| 项 | 决策 |
|----|------|
| 配置/数据路径 | **FHS 规范**：`/etc/sentinel/`（配置）、`/var/lib/sentinel/`（DB/cookie/密钥）、`/var/log/sentinel/`（日志） |
| CLI 框架 | **cobra** |
| 数据更新 | **全部内嵌进二进制**（`go:embed`），不做远端数据更新 |
| 二进制/仓库名 | **`sentinel`** |
| 控制通道 | **反转连接 + EWP/v2.1 加密长连接**（见 §11，本次核心升级） |
| 控制通道加密 | ChaCha20-Poly1305 + X25519/ML-KEM-768 混合握手（沿用 sing-ewp，不改算法，见 §11.4） |

---

## 10. 建议的实现顺序（确认后执行）

1. 项目骨架 + `go.mod`（含 `sing-ewp` 依赖）+ `netx` + `config`（地基）
2. **控制通道（§11）**：`ctrl` 包封装 EWP 长连接（Agent 拨号侧 + Master 服务侧 + 消息编解码 + 重连），先用假业务跑通端到端加密链路。**同步落地 §4.4 安全约束 + §12 fuzz 测试**（消息解析与指令分发是最高危路径，先加固再往上盖功能）。
3. Agent：scheduler + google/trust 模块（核心保活价值；访问目标白名单常量化，见 SR-3）
4. Master：store + telegram 长轮询 + 基础 `/start` 面板
5. Master：完整交互面板 + 通过 ctrl 下发指令 + 在线状态表
6. Quality 模块（六大检测源，1:1 复刻 xykt）+ 战报 + 趋势库
7. install 子命令 + systemd/OTA + 密钥生成
8. 内嵌数据 + 交叉编译（CGO_ENABLED=0）+ 联调

---

## 11. 控制通道架构（本次核心升级）

### 11.1 动机：原版 webhook 模型的硬伤

原项目让每个 Agent 开公网端口挂自签证书 HTTPS 傻等 Master 来连，问题：
1. 每个 Agent 必须有公网 IP + 手动去云商控制台放行安全组端口；
2. NAT / CGNAT 后的机器根本无法被连入；
3. 自签证书 + `InsecureSkipVerify` = TLS 形同虚设，实际只有一层 HMAC；
4. Master 靠「最后通讯时间」猜节点死活，无实时性。

### 11.2 新模型：反转连接 + EWP/v2.1 加密长连接

```
        ┌──────────────────────── Telegram (getUpdates 长轮询) ────────┐
        ▼                                                              │
   ┌─────────┐   ① Agent 主动拨号 (出站, 无需公网端口)                 用户
   │  Master │◄──────────────────────────────────────────┐           ▲
   │ (Service│   ② EWP/v2.1 握手 (X25519+ML-KEM-768)      │           │
   │  端/被连)│   ③ 常驻 SecureStream (ChaCha20-Poly1305)   │           │
   │         │──── 指令 cmd ───────────────────────────────►  Agent   │
   │         │◄─── 上报 evt / 心跳 / 结果 ──────────────────  (Client │
   └─────────┘                                              端/拨号)  │
        │  维护 uuid→活跃 stream 在线表；断开即标记离线                   │
        └──────────────────────────────────────────────────────────────┘
```

- **Master = EWP 服务端（被连方）**：监听一个 TCP 端口（只 Master 需要一个公网可达端口，Agent 全部不需要）。持有一对**长期静态 X25519 密钥**，私钥严格权限存储（`/var/lib/sentinel/master_static.key`，0600），公钥分发给所有 Agent。用 `ServiceV21` + `AddUser(uuid)` 维护 Agent 白名单。
- **Agent = EWP 客户端（拨号方）**：内置 Master 的静态公钥 + Master 地址 + 自己的 UUID。用 `NewClientV21(uuid, masterStaticPubB64)` 拨号，`DialConn` 拿到透明加密的 `net.Conn`，在其上跑应用层消息协议（§4.2）。断线指数退避重连。

> **角色澄清（重要）**：EWP 协议里「静态密钥对属于被连接的服务端」。因此在我们「Agent 连 Master」的反转模型下，**静态私钥在 Master，静态公钥内置于 Agent**。这与最初口头设想的「私钥在 Agent」方向相反 —— 但这才是该协议 S1/S2 审计修复所要求的正确用法（防服务端假冒 + 防 PSK 泄露后离线解密）。Agent 的身份由其 **UUID（作为 PSK）** + Master 白名单鉴别。

### 11.3 承载层

EWP 是消息流，跑在任意 `MessageTransport` 上。本控制通道用**最简的 TCP + 库自带 `LengthFramer`（4 字节大端长度前缀）**即可，**不需要再套 TLS**（EWP 握手后每一字节都已 AEAD 加密认证）。

> 可选增强（后续）：若需抗 DPI/封锁，可把 TCP 换成 WebSocket/gRPC 承载（EWP 的 `MessageTransport` 抽象允许无痛替换），但一期先 TCP。

### 11.4 加密算法（沿用 sing-ewp，不改）

| 用途 | 算法 |
|------|------|
| AEAD | **ChaCha20-Poly1305**（库硬编码，不可协商；对无 AES-NI 的廉价 VPS/ARM 更快且无 timing 侧信道） |
| 经典 KEM | X25519 |
| 后量子 KEM | ML-KEM-768 |
| KDF | HKDF-SHA-256 |
| 握手外层 MAC | HMAC-SHA-256 / 截断 16 字节 |

> 注：最初口头提到的 AES-256-GCM 未采用 —— sing-ewp 明确禁止替换 AEAD 原语（其 `README` Rule 2/4 + `aead.go` 单一实现），强行替换会破坏其「唯一真相源」设计并失去 12 项审计的保证。ChaCha20-Poly1305 在本场景（大量低算力小鸡）综合更优。若确实必须 AES-GCM，需评估是否 fork 该库（不推荐）。

### 11.5 密钥与身份管理

- **Master 静态密钥对**：`sentinel master init` 时用 `GenerateServerStaticKeypair()` 生成；私钥落 `/var/lib/sentinel/master_static.key`（0600），公钥打印出来（Base64）供 Agent 安装时填入。
- **Agent UUID**：`sentinel install` 时生成（v4 UUID），写入 agent.yaml；作为 EWP PSK 身份。注册流程把它登记进 Master 白名单（`ServiceV21.AddUser`），并持久化到 nodes 表，Master 启动时全部 `AddUser` 重新加载。
- **重放保护**：EWP 自带 `ReplayCache`（±30s 时间窗 + nonce 去重），Master 侧启用。

### 11.6 依赖引入

`go.mod` 增加：
```
require github.com/justinwoo280/sing-ewp v0.2.x   // 锁定 0.2.x 最新 tag
```
（间接引入 `golang.org/x/crypto`。）该库仅提供加密流，我们的 `internal/ctrl` 包在其上实现消息协议、连接管理、重连、在线表。

### 11.7 config 增补

Agent 配置 `master` 段调整（去掉 webhook 端口概念）：
```yaml
master:
  enabled: true
  addr: "master.example.com:8443"   # Master 的 EWP 监听地址（唯一需要可达的端点）
  static_pub: "<base64 X25519 pub>" # Master 静态公钥，安装时填入
  uuid: "11111111-...-555555555555" # 本 Agent 身份 (EWP PSK)
  ota: true
reconnect:
  min_backoff: "1s"
  max_backoff: "60s"
  heartbeat: "30s"
# 原 webhook.port 字段删除
```

Master 配置增补：
```yaml
control:
  listen: ":8443"                       # EWP 服务端监听
  static_key_path: "/var/lib/sentinel/master_static.key"
```

---

## 附录 A：黑话 → 中性术语对照表

写码时统一使用右列命名（代码标识、日志、UI 文案）。

| 原黑话 | 中性技术命名 |
|--------|-------------|
| 深海声呐 / 全维探针 | IP 质量检测（quality check） |
| 叹息之墙 / 军用级签名 | EWP/v2.1 加密握手 + 每帧 AEAD（不再用应用层 HMAC） |
| 金蝉脱壳 | 自我升级（self-update / OTA） |
| 送中 / 高危送中 | 区域被判定为 CN（region flagged as CN） |
| 司令部 / 中枢 | Master（控制端） |
| 边缘哨兵 / 舰队 / 节点 | Agent（节点） |
| 区域纠偏 / 养护巡逻 | 保活任务（keepalive job） |
| 信用净化 | 信誉预热（reputation warmup） |
| 战区 / 全球雷达 | 区域分组 / 节点列表 |
| 弹匣装填 / 多宿主容灾 | 多 IP 故障转移（multi-IP failover） |
| 核按钮 | 危险操作（需二次确认） |
| 幽灵进程 / 静默重载 | 后台重启 |
| 惊群效应削峰 | 调度抖动（jitter） |

日志级别与格式统一：`时间(UTC) [LEVEL] [module] [region] message`，去掉所有 emoji 军事化措辞，保留必要的状态标识。

---

## 12. 安全模型与测试策略

### 12.1 分层威胁模型

| 层 | 威胁 | 由谁防 |
|----|------|--------|
| 传输层 | 窃听、篡改、重放、服务端假冒、离线解密 | EWP/v2.1（X25519+ML-KEM-768 握手、每帧 AEAD、ReplayCache、静态密钥绑定） |
| 接入层 | 非授权对端接入 | UUID(PSK) 白名单 + Master 静态密钥握手 |
| **内容层** | **恶意/畸形指令 → RCE、C2 投毒、崩溃** | **§4.4 SR-1~SR-6 + 本节 fuzz** |
| 执行层 | 越权改机器（OTA/卸载） | 本地策略二次校验（SR-6） |

> 内容层是本项目重写中最需要用测试锁死的部分：即便传输层完美，一旦 Agent「照单执行」不可信内容就是 RCE。所有防护以**可执行的测试**固化，而非仅靠约定。

### 12.2 Go 原生 fuzz（`go test -fuzz`）

目标：**任意字节输入下，解析+分发路径永不 panic、永不越过校验产生副作用、永不访问白名单外目标。**

| Fuzz 目标 | 输入 | 断言不变式 |
|-----------|------|-----------|
| `FuzzDecodeMessage` | 任意 `[]byte` | 要么 err，要么得到结构合法的 Message；**永不 panic**；解析成功 ⇒ 长度/字段均在界内 |
| `FuzzDispatchCommand` | 任意 JSON 指令 | 非枚举 `cmd` ⇒ 必拒绝；枚举 `cmd` ⇒ 参数必过 SR-4 校验才进入执行；分发过程无 shell、无网络副作用（用注入的假执行器断言） |
| `FuzzValidateAlias` | 任意字符串 | 通过校验的输出 ⇒ 长度≤20符文 且 仅含允许字符集 且 无控制字符 |
| `FuzzToggleParams` | 任意 mod/state | 通过 ⇒ `mod∈{google,trust}` 且 `state∈{true,false}` |
| `FuzzRegisterParse` | 任意 `SENTINEL-REG:` 报文 | 永不 panic；解析成功 ⇒ uuid 合法 v4、字段在界内 |
| `FuzzDecodeAddress`（若复用 EWP 地址） | 任意 `[]byte` | 永不 panic、越界安全 |

实现要点：
- 分发器注入一个 **fake executor 接口**（`Executor`），fuzz 时用记录型实现，断言「被调用的动作 ∈ 枚举」「传入的 URL ∈ 白名单常量集」「从未调用 shell 执行器（根本不存在该接口）」。
- 每个 fuzz 目标附带**种子语料**：正常样本 + 已知边界（超长 alias、畸形 UTF-8、深层嵌套、未知字段、超大长度前缀、空/截断、类型混淆如 `state:1` vs `"true"`）。
- CI 跑 `go test -run=Fuzz`（回归种子）+ 定期 `-fuzz=... -fuzztime=Ns`（持续探索）。

### 12.3 常规单元/属性测试（配套）

- **白名单常量测试**：断言所有对外访问 URL 均来自 `targets.go` 的固定集合；用静态检查/测试防止有人 `http.Get(变量)`。
- **parse-then-act 测试**：构造「解析中途失败」的输入，断言无任何副作用发生（无文件写、无网络、无 exec）。
- **消息长度上限测试**：超过 64 KiB 的帧被拒绝且连接安全关闭。
- **`DisallowUnknownFields` 测试**：夹带未知字段的合法外壳消息被拒绝。
- **EWP 集成测试**：Agent↔Master 端到端握手 + 指令下发 + 回执 + 断线重连 + 离线标记；篡改一字节密文 ⇒ 连接判死（对齐 EWP 的错误契约）。
- **race 检测**：`go test -race`，覆盖 ctrl 的并发发送 / 单 goroutine 接收契约（对齐 SecureStream 并发模型）。

### 12.4 硬编码红线（review 时直接打回）

1. 出现 `exec.Command("sh"/"bash", "-c", ...)` 或任何字符串拼接成命令 → 拒绝。
2. 出现 `http.Get`/请求目标 URL 来自消息字段而非白名单常量 → 拒绝。
3. 任何「下载内容后执行」的逻辑 → 拒绝。
4. 指令分发存在 `default` 回落到动态执行 → 拒绝。
5. 解析路径可 panic（未处理越界/类型断言无 ok） → 拒绝。
