# ============================================================
#  版本一致性检查: desktop/app.go 的 appVersion 必须与
#  desktop/wails.json 的 info.productVersion 一致(发布前校验)。
# ============================================================
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot

$appGo = Get-Content (Join-Path $root "desktop\app.go") -Raw
$m = [regex]::Match($appGo, 'const appVersion = "([^"]+)"')
if (-not $m.Success) { Write-Host "[错误] desktop/app.go 中未找到 appVersion" -ForegroundColor Red; exit 1 }

$wails = Get-Content (Join-Path $root "desktop\wails.json") -Raw | ConvertFrom-Json
$goVer = $m.Groups[1].Value
$wsVer = $wails.info.productVersion
if ($goVer -ne $wsVer) {
    Write-Host "[错误] 版本号不一致: app.go=$goVer  wails.json=$wsVer" -ForegroundColor Red
    exit 1
}
Write-Host "[OK] 版本一致: v$goVer" -ForegroundColor Green
