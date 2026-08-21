@echo off
chcp 65001 >nul
REM DocShare 服务器版一键构建(单文件, 用于局域网部署)
cd /d "%~dp0"
where go >nul 2>nul
if errorlevel 1 (
  set "GOBIN=%ProgramFiles%\Go\bin"
  if exist "%GOBIN%\go.exe" ( set "PATH=%GOBIN%;%PATH%" ) else (
    echo [错误] 未检测到 Go, 请先安装: https://go.dev/dl/
    pause
    exit /b 1
  )
)
go build -o release\DocShare-Server.exe .\backend
if errorlevel 1 (
  echo [错误] 构建失败
  pause
  exit /b 1
)
echo [OK] 构建完成: release\DocShare-Server.exe
pause
