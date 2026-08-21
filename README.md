# DocShare · MD 文档中心

简约、现代化的 **Markdown 文档预览与协作编辑**工具，支持局域网访问。

**三种形态，一个内核：**

| 版本 | 用途 | 文件 |
| ---- | ---- | ---- |
| 🖥️ **安装版**（推荐） | 标准安装程序：开始菜单快捷方式、卸载器，可配置**开机自启动** | `release/DocShare-Setup.exe` |
| 📦 **便携版** | 双击即用，目录、日志全在 exe 旁 `data/` | `release/DocShare.exe`（单文件） |
| 🌐 **服务器版** | 命令行部署，指定目录对外提供局域网预览 + 审批 API | `release/DocShare-Server.exe`（单文件） |

前后端分离：Go 后端提供 REST API，前端为独立静态页面（已内嵌进 exe，无需安装任何依赖）。

## 快速开始（安装版）

1. 运行 `release/DocShare-Setup.exe`，按向导完成安装（默认当前用户目录，无需管理员权限）；
2. 从开始菜单启动 DocShare，点击左下角 **⚙ 设置**：
   - **文档目录**：点击「浏览…」选择你的 Markdown 文件夹；
   - **端口 / 局域网开关 / IP 黑名单**：按需调整，保存后服务自动重启；
   - **开机自动启动**：勾选后登录 Windows 自动运行；
3. 同一局域网设备访问 `http://<本机IP>:8080` 即可浏览文档（**仅只读**）；
4. **「访问记录」**可查看最近 500 条文档浏览记录（时间/文档/来源 IP/浏览器）；
5. **最小化或关闭窗口**会隐藏到系统托盘继续提供服务（局域网不受影响），右键托盘图标可「打开 DocShare」或「退出」。首次隐藏会弹出系统通知引导 —— **Windows 11 的新托盘图标默认在通知区域「^」折叠区**，点开箭头即可看到，可拖出固定到常驻区。

> 安装版数据保存在 `%APPDATA%\DocShare\`（日志见 `app.log`）；便携版在 exe 同目录 `data/`。

## 快速开始（服务器版）

```bash
DocShare-Server.exe -dir D:\我的文档 -addr 0.0.0.0:8080 -token 你的管理令牌
```

启动后控制台打印局域网地址；审批接口需携带令牌（`Authorization: Bearer <令牌>`）。

## 功能特性

- 📂 文档树浏览 —— 多级文件夹、实时搜索（`Ctrl+K`）、**侧边栏可拖拽调宽**（宽度记忆）、深/浅双主题
- 📑 **悬浮大纲** —— 右侧悬浮章节大纲，可折叠为「目录」按钮；**展开时正文自动让位不遮盖**；点击跳转 + 滚动高亮
- 📝 Markdown 渲染 —— GFM、代码高亮、表格、任务列表（marked + DOMPurify + highlight.js，已本地化，断网可用）
- 🔒 只读浏览 —— 文档仅可查看，无任何编辑/修改入口
- 📊 **访问记录** —— 桌面端查看最近 500 条文档浏览记录（时间/文档/来源 IP/浏览器），落盘持久化
- 🪟 **系统托盘** —— 最小化或关闭窗口后隐藏到托盘继续服务；**首次隐藏弹出系统通知引导**（Win11 新图标默认在通知区域「^」折叠区，可拖出固定）；右键图标「打开 / 退出」
- 🚀 **开机自启动** —— 设置面板一键配置（写入当前用户注册表启动项）
- 🚫 **IP 黑名单** —— 支持精确 IP 与 CIDR 网段，命中即 403，保存后立即生效
- 💾 **配置记忆** —— MD 目录/端口/黑名单等配置持久化，重启软件自动恢复
- 🪟 **单实例** —— 重复启动时提示并退出，避免端口冲突
- 🔒 安全 —— 路径穿越防护（含符号链接校验）、XSS 消毒

## 构建

```powershell
# 桌面版(自动安装 Wails CLI)
.\build_desktop.ps1

# 服务器版
.\build.bat
```

需要 Go 1.22+；桌面版运行需要 WebView2 Runtime（Windows 10/11 系统自带）。

## 目录结构

```text
DocShare/
├── backend/main.go       # 服务器版入口
├── desktop/              # 桌面版入口(Wails v2)
│   ├── main.go           #   窗口与内嵌资源
│   ├── app.go            #   配置/服务/审批绑定方法
│   └── wails.json
├── internal/
│   ├── api/              # HTTP 接口 + 内嵌前端资源(web/)
│   ├── store/            # 目录树/文档读写/申请存档/备份
│   └── config/           # 桌面端配置持久化
├── docs/                 # 默认示例文档目录
├── release/              # 构建产物(两个单文件 exe)
└── test/ui-test.js       # 前端自动化冒烟测试(puppeteer)
```

## API 一览

| 方法 | 路径 | 说明 |
| ---- | ---- | ---- |
| GET | `/api/tree` | 文档目录树 |
| GET | `/api/doc?path=xxx` | 读取文档 |
| GET | `/api/health` | 健康检查 |

桌面端通过 Wails 原生绑定（`ServerInfo` / `SaveConfig` / `ListDir` / `ListAccessLogs` / `AutoStart` / `SetAutoStart`）完成配置与访问记录查看。

## 技术栈

Go (net/http + Wails v2) · 原生 HTML/CSS/JS · marked · DOMPurify · highlight.js · WebView2
