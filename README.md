# DocShare · MD 文档中心

简约、现代化的 **局域网 Markdown 文档预览工具**。

**三种形态，一个内核：**

| 版本 | 用途 | 文件 |
| ---- | ---- | ---- |
| 🖥️ **安装版**（推荐） | 标准安装程序：开始菜单快捷方式、卸载器，可配置**开机自启动** | `release/DocShare-Setup.exe` |
| 📦 **便携版** | 双击即用，配置、日志全在 exe 旁 `data/` | `release/DocShare.exe`（单文件） |
| 🌐 **服务器版** | 命令行部署，指定目录对外提供局域网只读预览 | `release/DocShare-Server.exe`（单文件） |

前后端分离：Go 后端提供 REST API，前端为独立静态页面（已内嵌进 exe，无需安装任何依赖）。

## 快速开始（安装版）

1. 运行 `release/DocShare-Setup.exe`，按向导完成安装（默认当前用户目录，无需管理员权限）；
2. 从开始菜单启动 DocShare，点击左下角 **⚙ 设置**：
   - **文档目录**：点击「浏览…」选择 Markdown 文件夹，**支持添加多个目录并行展示**；
   - **端口 / 局域网开关 / IP 黑名单 / 访问密码**：按需调整，保存后服务自动重启；
   - **开机自动启动**：勾选后登录 Windows 自动运行；
3. 同一局域网设备访问 `http://<本机IP>:8080` 即可浏览文档（**仅只读**，可设置访问密码）；
4. **「访问记录」**可查看最近 200 条文档浏览记录（时间/文档/来源 IP/浏览器）；
5. **最小化或关闭窗口**会隐藏到系统托盘继续提供服务（局域网不受影响），右键托盘图标可「打开 DocShare」「**复制访问地址**」或「退出」。首次隐藏会弹出系统通知引导 —— **Windows 11 的新托盘图标默认在通知区域「^」折叠区**，点开箭头即可看到，可拖出固定到常驻区；
6. **设置**面板可一键**复制访问地址**发给同事；**软件更新**支持**启动时静默检查**（发现新版本显示徽标）与一键下载安装。

> 安装版数据保存在 `%APPDATA%\DocShare\`（日志见 `app.log`）；便携版在 exe 同目录 `data/`。

## 快速开始（服务器版）

```bash
# 单目录
DocShare-Server.exe -dir D:\我的文档 -addr 0.0.0.0:8080

# 多目录(逗号分隔) + 访问密码 + IP 黑名单
DocShare-Server.exe -dir D:\文档A,D:\文档B -addr 0.0.0.0:8080 -password 123456 -blacklist 192.168.1.66,10.0.0.0/8
```

启动后控制台打印本机与局域网访问地址。

## 功能特性

- 📂 **文档树浏览** —— 多级文件夹、**多文档根目录并行展示**、侧边栏可拖拽调宽（宽度记忆）、目录变化自动刷新（Windows 文件变更监听，毫秒级感知）
- 🔄 **文档自动刷新** —— 正在阅读的文档被外部修改后自动重新渲染最新内容，阅读位置（章节锚点）自动保持；文档被删除时提示
- 🔍 **全文搜索** —— 中文/英文关键词，倒排索引 + 上下文摘要 + 正文高亮（`Ctrl+K` 聚焦）
- 📑 **悬浮大纲** —— 章节导航，可折叠；展开时正文自动让位不遮盖；点击跳转 + 滚动同步高亮
- 📝 **Markdown 渲染** —— GFM、代码高亮（**一键复制**）、**Mermaid 图表**（跟随主题）、表格、任务列表
- 📤 **文档导出** —— 单篇导出独立 HTML 文件，或打印为 PDF（`Ctrl+P`）
- 🔒 **只读浏览 + 访问密码** —— 无任何编辑入口；可启用访问密码（**连续失败自动锁定** 30 秒防暴力破解）
- 📊 **访问记录** —— 桌面端聚合查看各文档根最近 200 条记录，异步批量落盘
- 📖 **阅读记忆** —— 最近浏览列表 + **启动自动打开上次文档** + 章节锚点精确定位
- 💬 **文档批注** —— 选中正文文字即可添加批注,行内高亮锚点展示,支持**回复讨论**与删除;多人实时同步(≤3s),昵称浏览器记忆
- 🎨 **主题** —— 深色 / 浅色 / **跟随系统**（自动联动切换）
- 🪟 **系统托盘** —— 最小化/关闭后隐藏到托盘继续服务（品牌图标），右键「打开 / **复制访问地址** / 退出」
- ⌨️ **快捷键** —— `Ctrl+K` 搜索、`Ctrl+P` 打印导出、`Esc` 关闭弹窗
- 🚀 **自动更新** —— 启动静默检查 + 徽标提示 + 一键下载安装（SHA-256 校验通过后才执行）
- 🚫 **IP 黑名单** —— 精确 IP 与 CIDR 网段，命中即 403
- 🔒 **安全** —— 路径穿越/符号链接校验、XSS 消毒、严格请求大小与超时、Windows DPAPI 保护密码配置

## 构建

```powershell
# 桌面版(自动安装 Wails CLI, 含 NSIS 安装包)
.\build_desktop.ps1

# 服务器版
.\build.bat
```

需要 Go 1.25+；桌面版运行需要 WebView2 Runtime（Windows 10/11 系统自带）。发布前运行 `./tools/check-version.ps1` 检查版本一致性。

## 目录结构

```text
DocShare/
├── backend/main.go       # 服务器版入口(支持多目录/密码/黑名单)
├── desktop/              # 桌面版入口(Wails v2)
│   ├── main.go           #   单实例 + 系统托盘 + 关闭/最小化隐藏
│   ├── app.go            #   配置/服务/绑定(含自动更新检查)
│   └── wails.json        #   含品牌图标配置
├── internal/
│   ├── api/              # HTTP 接口(多根聚合/搜索/认证/黑名单) + 内嵌前端(web/)
│   ├── store/            # 目录树/文档读写/全文索引/访问记录
│   ├── config/           # 桌面端配置持久化
│   ├── autostart/        # 开机自启动(注册表)
│   └── tray/             # 系统托盘(win32)
├── docs/                 # 默认示例文档目录
├── release/              # 构建产物(安装版/便携版/服务器版)
├── .github/workflows/    # PR/Push 质量门禁 + tag 自动发布
├── tools/check-version.ps1 # 发布前版本一致性校验
└── test/                 # UI 自动化测试(puppeteer + Edge)
    ├── ui-test.js
    └── run.ps1           # 一键运行(自动起服务/随机端口/清理)
```

## API 一览

| 方法 | 路径 | 说明 |
| ---- | ---- | ---- |
| GET | `/api/tree` | 文档目录树（多根时按根分组） |
| GET | `/api/doc?path=xxx` | 读取文档 |
| GET | `/api/search?q=xxx` | 全文搜索（跨根聚合） |
| GET | `/api/annotations?path=xxx` | 文档批注列表 |
| POST | `/api/annotations` | 创建批注（选中文本 + 偏移锚点） |
| POST | `/api/annotations/{id}/reply` | 回复批注 |
| DELETE | `/api/annotations/{id}?path=xxx` | 删除批注（含回复） |
| GET | `/api/auth/status` | 访问密码状态 |
| POST | `/api/auth/login` | 访问密码登录（签发会话令牌） |
| GET | `/api/health` | 健康检查 |

桌面端通过 Wails 原生绑定（`ServerInfo` / `SaveConfig` / `ListDir` / `ListAccessLogs` / `AutoStart` / `SetAutoStart` / `CheckUpdate`）完成配置、访问记录与更新检查。

## 测试

- **Go 单测**：`go test -race ./...`（全文索引、路径安全、认证锁定、配置恢复、目录监听、多根聚合、更新校验）
- **UI 自动化**：`powershell -ExecutionPolicy Bypass -File test\run.ps1`（自动构建/启动服务器、随机端口、跑完清理；需本机 Edge）
- **CI/CD**：PR 自动执行格式、`go vet`、race 测试、npm 安全审计与 UI 冒烟测试；推送 `v*` 标签构建并发布三件套

## 技术栈

Go (net/http + Wails v2) · 原生 HTML/CSS/JS · marked · DOMPurify · highlight.js · mermaid.js · WebView2
