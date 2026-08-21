# ============================================================
#  DocShare UI 冒烟测试一键运行
#  用法:  powershell -ExecutionPolicy Bypass -File test\run.ps1
#  行为:
#    1. 若 release\DocShare-Server.exe 不存在则先构建
#    2. 随机端口启动服务器(临时数据目录, 避免污染/端口冲突)
#    3. 运行 test\ui-test.js (node + 本机 Edge + puppeteer-core)
#    4. 无论成败都清理服务器与临时目录
# ============================================================
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

# 1. 依赖: puppeteer-core
if (-not (Test-Path "$PSScriptRoot\node_modules\puppeteer-core")) {
    Write-Host "[1/4] 安装测试依赖 (puppeteer-core)..." -ForegroundColor Cyan
    Push-Location $PSScriptRoot
    npm install --no-audit --no-fund
    if ($LASTEXITCODE -ne 0) { Pop-Location; exit 1 }
    Pop-Location
}

# 2. 服务器可执行文件
$server = "$root\release\DocShare-Server.exe"
if (-not (Test-Path $server)) {
    Write-Host "[2/4] 构建 CLI 服务器..." -ForegroundColor Cyan
    go build -ldflags "-s -w" -o $server ./backend
    if ($LASTEXITCODE -ne 0) { exit 1 }
}

# 3. 随机端口 + 临时数据目录
$port = 18100 + (Get-Random -Maximum 400)
$dataDir = Join-Path $env:TEMP ("dshtest-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $dataDir | Out-Null
$proc = $null
try {
    Write-Host "[3/4] 启动测试服务器 127.0.0.1:$port ..." -ForegroundColor Cyan
    $proc = Start-Process -FilePath $server -ArgumentList @(
        "-dir", "$root\docs",
        "-addr", "127.0.0.1:$port",
        "-data", $dataDir
    ) -PassThru -WindowStyle Hidden
    # 等待健康检查通过
    $ready = $false
    for ($i = 0; $i -lt 30; $i++) {
        Start-Sleep -Milliseconds 400
        try {
            $null = Invoke-WebRequest -UseBasicParsing "http://127.0.0.1:$port/api/health" -TimeoutSec 2
            $ready = $true
            break
        } catch { }
        if ($proc.HasExited) { throw "测试服务器异常退出 (code=$($proc.ExitCode))" }
    }
    if (-not $ready) { throw "测试服务器健康检查超时" }

    # 4. 运行测试
    Write-Host "[4/4] 运行 UI 测试..." -ForegroundColor Cyan
    $env:DS_BASE = "http://127.0.0.1:$port"
    $env:DS_SERVER = $server
    Push-Location $PSScriptRoot
    node ui-test.js
    $code = $LASTEXITCODE
    Pop-Location
    Remove-Item Env:DS_BASE -ErrorAction SilentlyContinue
    Remove-Item Env:DS_SERVER -ErrorAction SilentlyContinue
    exit $code
} finally {
    if ($proc -and -not $proc.HasExited) {
        Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
    }
    Remove-Item -Recurse -Force $dataDir -ErrorAction SilentlyContinue
}
