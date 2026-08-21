Unicode true

####
## DocShare 定制 NSIS 安装脚本
## 1. 更新安装: 自动检测原安装目录并覆盖(跳过目录选择页)
## 2. 安装完成: 提供「启动 DocShare」勾选框
## 其余逻辑与 Wails 默认模板一致。
####
!include "wails_tools.nsh"

# The version information for this two must consist of 4 parts
VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

ManifestDPIAware true

!include "MUI.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_FINISHPAGE_NOAUTOCLOSE
!define MUI_ABORTWARNING

# ===== 定制 2: 安装完成勾选框(启动软件) =====
!define MUI_FINISHPAGE_RUN "$INSTDIR\${PRODUCT_EXECUTABLE}"
!define MUI_FINISHPAGE_RUN_TEXT "启动 ${INFO_PRODUCTNAME}"

!insertmacro MUI_PAGE_WELCOME
# ===== 定制 1: 已安装时跳过目录选择(自动覆盖原目录) =====
!define MUI_PAGE_CUSTOMFUNCTION_PRE SkipDirectoryIfInstalled
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe"
!ifdef WAILS_INSTALL_SCOPE
  !if "${WAILS_INSTALL_SCOPE}" == "user"
    InstallDir "$LOCALAPPDATA\Programs\${INFO_PRODUCTNAME}"
  !else
    InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
  !endif
!else
  InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
!endif
ShowInstDetails show

# 已安装标记与原目录
Var PREV_INSTALLED
Var PREV_INSTALL_DIR

Function .onInit
   !insertmacro wails.checkArchitecture
   StrCpy $PREV_INSTALLED "0"
   StrCpy $PREV_INSTALL_DIR ""

   # 1) 优先读 InstallLocation(新版本安装时写入)
   ReadRegStr $R0 HKCU "${UNINST_KEY}" "InstallLocation"
   StrCmp $R0 "" try_hklm_loc
     StrCpy $PREV_INSTALL_DIR $R0
     Goto validate_dir
   try_hklm_loc:
   ReadRegStr $R0 HKLM "${UNINST_KEY}" "InstallLocation"
   StrCmp $R0 "" try_uninstall_string
     StrCpy $PREV_INSTALL_DIR $R0
     Goto validate_dir

   # 2) 旧版本未写 InstallLocation: 从 UninstallString 解析原目录
   try_uninstall_string:
   ReadRegStr $R0 HKCU "${UNINST_KEY}" "UninstallString"
   StrCmp $R0 "" try_hklm_uninst
     StrCpy $PREV_INSTALL_DIR $R0 -14 ""   # 去掉尾部 \uninstall.exe
     StrCpy $R1 $PREV_INSTALL_DIR 1 0
     StrCmp $R1 '"' 0 validate_dir
       StrCpy $PREV_INSTALL_DIR $PREV_INSTALL_DIR "" 1  # 去掉首引号
     Goto validate_dir
   try_hklm_uninst:
   ReadRegStr $R0 HKLM "${UNINST_KEY}" "UninstallString"
   StrCmp $R0 "" done
     StrCpy $PREV_INSTALL_DIR $R0 -14 ""
     StrCpy $R1 $PREV_INSTALL_DIR 1 0
     StrCmp $R1 '"' 0 validate_dir
       StrCpy $PREV_INSTALL_DIR $PREV_INSTALL_DIR "" 1
     Goto validate_dir

   validate_dir:
   # 校验原目录确实存在(避免误判)
   IfFileExists "$PREV_INSTALL_DIR\uninstall.exe" use_prev
   IfFileExists "$PREV_INSTALL_DIR\${PRODUCT_EXECUTABLE}" use_prev
   Goto done
   use_prev:
     StrCpy $INSTDIR $PREV_INSTALL_DIR
     StrCpy $PREV_INSTALLED "1"
   done:
FunctionEnd

# 已安装: 跳过目录选择页, 直接覆盖原目录
Function SkipDirectoryIfInstalled
   StrCmp $PREV_INSTALLED "1" 0 ShowPage
   Abort
   ShowPage:
FunctionEnd

Section
    !insertmacro wails.setShellContext

    !insertmacro wails.webview2runtime

    SetOutPath $INSTDIR

    !insertmacro wails.files

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    !insertmacro wails.writeUninstaller

    # ===== 定制: 记录安装位置(供后续更新检测) =====
    WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
SectionEnd

Section "uninstall"
    !insertmacro wails.setShellContext

    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}" # Remove the WebView2 DataPath

    RMDir /r $INSTDIR

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    !insertmacro wails.deleteUninstaller
SectionEnd
