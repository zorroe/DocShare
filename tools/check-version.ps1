# ============================================================
#  版本一致性检查: desktop/app.go 的 appVersion 必须与
#  desktop/wails.json 与前端静态资源版本一致(发布前校验)。
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

$index = Get-Content (Join-Path $root "internal\api\web\index.html") -Raw
$assetVersions = @([regex]::Matches($index, '\?v=([0-9]+\.[0-9]+\.[0-9]+)') |
    ForEach-Object { $_.Groups[1].Value } | Select-Object -Unique)
if ($assetVersions.Count -ne 1 -or $assetVersions[0] -ne $goVer) {
    Write-Host "[错误] 前端资源版本与应用不一致: $($assetVersions -join ', ')" -ForegroundColor Red
    exit 1
}

$appJS = Get-Content (Join-Path $root "internal\api\web\js\app.js") -Raw
if ($appJS -notmatch [regex]::Escape("mermaid.min.js?v=$goVer")) {
    Write-Host "[错误] Mermaid 按需加载版本与应用不一致" -ForegroundColor Red
    exit 1
}
Write-Host "[OK] 版本一致: v$goVer" -ForegroundColor Green
exit 0
