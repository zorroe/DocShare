@echo off
chcp 65001 >nul
REM DocShare 服务器版一键启动(局域网部署, 单文件)
REM 用法: start.bat [管理令牌]
cd /d "%~dp0"
if not exist release\DocShare-Server.exe (
  echo [提示] 未找到可执行文件, 正在构建...
  call build.bat
  if errorlevel 1 exit /b 1
)
if "%~1"=="" (
  release\DocShare-Server.exe -dir docs -addr 0.0.0.0:8080
) else (
  release\DocShare-Server.exe -dir docs -addr 0.0.0.0:8080 -token %~1
)
pause
