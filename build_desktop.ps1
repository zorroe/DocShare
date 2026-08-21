# ============================================================
#  DocShare 桌面版一键构建脚本
#  产物:
#    release\DocShare.exe        便携版(单文件)
#    release\DocShare-Setup.exe  安装版(NSIS, 需安装 NSIS: winget install NSIS.NSIS)
#  依赖: Go 1.22+ (https://go.dev/dl/) + WebView2 (Win10/11 自带)
# ============================================================
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

# 0. 版本一致性校验(发布前必查)
& "$root\tools\check-version.ps1"
if ($LASTEXITCODE -ne 0) { exit 1 }
# 清理构建残留(中断的 go build 可能留下 *.exe~)
Remove-Item "$root\release\*.exe~" -ErrorAction SilentlyContinue

# 1. 检查 Go
$go = Get-Command go -ErrorAction SilentlyContinue
if (-not $go) {
    $candidate = "$env:ProgramFiles\Go\bin\go.exe"
    if (Test-Path $candidate) { $env:Path = "$env:ProgramFiles\Go\bin;" + $env:Path }
    else { Write-Host "[错误] 未检测到 Go, 请先安装: https://go.dev/dl/" -ForegroundColor Red; exit 1 }
}
Write-Host "[1/4] Go 就绪: $(go version)" -ForegroundColor Cyan

# 2. 检查/安装 Wails CLI
$wails = Get-Command wails -ErrorAction SilentlyContinue
if (-not $wails) {
    $wailsExe = "$env:USERPROFILE\go\bin\wails.exe"
    if (-not (Test-Path $wailsExe)) {
        Write-Host "[2/4] 正在安装 Wails CLI (首次需要几分钟)..." -ForegroundColor Cyan
        go install github.com/wailsapp/wails/v2/cmd/wails@latest
    }
    $env:Path = "$env:USERPROFILE\go\bin;" + $env:Path
}
Write-Host "[2/4] Wails 就绪: $(wails version | Select-Object -First 1)" -ForegroundColor Cyan

# 3. 构建便携版
Write-Host "[3/4] 正在构建便携版..." -ForegroundColor Cyan
Set-Location "$root\desktop"
wails build -s -skipbindings -clean -o DocShare.exe
if ($LASTEXITCODE -ne 0) { Write-Host "[错误] 便携版构建失败" -ForegroundColor Red; exit 1 }

New-Item -ItemType Directory -Force -Path "$root\release" | Out-Null
Copy-Item "$root\desktop\build\bin\DocShare.exe" "$root\release\DocShare.exe" -Force

# 4. 构建安装版(NSIS)
Write-Host "[4/4] 正在构建安装版..." -ForegroundColor Cyan
$makensis = Get-Command makensis -ErrorAction SilentlyContinue
if (-not $makensis) {
    $nsis = "C:\Program Files (x86)\NSIS\makensis.exe"
    if (Test-Path $nsis) { $env:Path = "C:\Program Files (x86)\NSIS;" + $env:Path }
    else {
        Write-Host "[提示] 未检测到 NSIS, 跳过安装版 (winget install NSIS.NSIS 可安装)" -ForegroundColor Yellow
        exit 0
    }
}
# 使用自定义 NSIS 脚本(更新覆盖原目录 + 安装完成启动勾选)
$nsisSrc = "$root\build\windows\installer\project.nsi"
if (Test-Path $nsisSrc) {
    New-Item -ItemType Directory -Force -Path "$root\desktop\build\windows\installer" | Out-Null
    Copy-Item $nsisSrc "$root\desktop\build\windows\installer\project.nsi" -Force
    Write-Host "    使用自定义安装脚本 (project.nsi)" -ForegroundColor Cyan
}
wails build -s -skipbindings -nsis -installscope user -o DocShare.exe
if ($LASTEXITCODE -ne 0) { Write-Host "[错误] 安装版构建失败" -ForegroundColor Red; exit 1 }
$setup = Get-ChildItem "$root\desktop\build\bin\*-installer.exe" | Select-Object -First 1
if ($setup) { Copy-Item $setup.FullName "$root\release\DocShare-Setup.exe" -Force }

Write-Host ""
Write-Host "[完成]" -ForegroundColor Green
Get-ChildItem "$root\release" | ForEach-Object { Write-Host "  release\$($_.Name)  $([math]::Round($_.Length/1MB,1)) MB" -ForegroundColor Green }
