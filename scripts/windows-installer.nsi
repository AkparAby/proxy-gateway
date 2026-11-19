# -*- coding: utf-8 -*-
Unicode true
RequestExecutionLevel user
SetCompressor /SOLID zlib
!include "MUI2.nsh"
!include "x64.nsh"
!define REGKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\ProxyGateway"
Name "Proxy Gateway"
OutFile "${OUTPUT}"
InstallDir "$LOCALAPPDATA\Programs\Proxy Gateway"
InstallDirRegKey HKCU "${REGKEY}" "InstallLocation"
VIProductVersion "${NUMVERSION}.0"
VIAddVersionKey "ProductName" "Proxy Gateway"
VIAddVersionKey "FileDescription" "Proxy Gateway Installer"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "LegalCopyright" "Copyright (c) AkparAby"
!define MUI_ICON "${ICON}"
!define MUI_UNICON "${ICON}"
!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_RUN "$INSTDIR\proxy-gateway.exe"
!define MUI_FINISHPAGE_RUN_NOTCHECKED
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "${PAYLOAD}\LICENSE"
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_UNPAGE_FINISH
!insertmacro MUI_LANGUAGE "SimpChinese"
!insertmacro MUI_LANGUAGE "English"
LangString MainSection ${LANG_SIMPCHINESE} "Proxy Gateway（必需）"
LangString MainSection ${LANG_ENGLISH} "Proxy Gateway (required)"
LangString ShortcutSection ${LANG_SIMPCHINESE} "桌面快捷方式"
LangString ShortcutSection ${LANG_ENGLISH} "Desktop shortcut"
LangString MaintenanceFailed ${LANG_SIMPCHINESE} "无法恢复接入设置或停止此安装目录中的程序，已中止操作。请先在网关中恢复接入并退出，然后重试。详情："
LangString MaintenanceFailed ${LANG_ENGLISH} "Cannot restore settings or stop this installation. Operation aborted. Restore integrations and exit the gateway, then retry. Details: "

Function .onInit
  SetShellVarContext current
  SetRegView 64
  ${IfNot} ${RunningX64}
    MessageBox MB_ICONSTOP "Proxy Gateway requires 64-bit Windows."
    SetErrorLevel 1
    Quit
  ${EndIf}
FunctionEnd

Function un.onInit
  SetShellVarContext current
  SetRegView 64
FunctionEnd

!macro Maintain
  InitPluginsDir
  SetOutPath "$PLUGINSDIR"
  File /oname=installer-maintenance.ps1 "${PAYLOAD}\scripts\installer-maintenance.ps1"
  nsExec::ExecToStack '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\installer-maintenance.ps1" -Root "$INSTDIR"'
  Pop $0
  Pop $1
  ${If} $0 != 0
    DetailPrint "$1"
    MessageBox MB_ICONSTOP "$(MaintenanceFailed)$\r$\n$1" /SD IDOK
    SetErrorLevel 1
    Abort
  ${EndIf}
!macroend

Section "$(MainSection)" Main
  SectionIn RO
  !insertmacro Maintain
  SetOverwrite on
  !include "${INSTALL_FILES}"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  CreateDirectory "$SMPROGRAMS\Proxy Gateway"
  CreateShortcut "$SMPROGRAMS\Proxy Gateway\Proxy Gateway.lnk" "$INSTDIR\proxy-gateway.exe"
  CreateShortcut "$SMPROGRAMS\Proxy Gateway\Restore Proxy.lnk" "$INSTDIR\Restore-Proxy.cmd"
  WriteRegStr HKCU "${REGKEY}" "DisplayName" "Proxy Gateway"
  WriteRegStr HKCU "${REGKEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${REGKEY}" "Publisher" "AkparAby"
  WriteRegStr HKCU "${REGKEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${REGKEY}" "DisplayIcon" "$INSTDIR\proxy-gateway.exe"
  WriteRegStr HKCU "${REGKEY}" "UninstallString" '$\"$INSTDIR\Uninstall.exe$\"'
  WriteRegStr HKCU "${REGKEY}" "QuietUninstallString" '$\"$INSTDIR\Uninstall.exe$\" /S'
  WriteRegStr HKCU "${REGKEY}" "URLInfoAbout" "https://github.com/AkparAby/proxy-gateway"
  WriteRegDWORD HKCU "${REGKEY}" "NoModify" 1
  WriteRegDWORD HKCU "${REGKEY}" "NoRepair" 1
SectionEnd

Section /o "$(ShortcutSection)" DesktopShortcut
  CreateShortcut "$DESKTOP\Proxy Gateway.lnk" "$INSTDIR\proxy-gateway.exe"
SectionEnd

Section "Uninstall"
  !insertmacro Maintain
  !include "${UNINSTALL_FILES}"
  Delete "$INSTDIR\Uninstall.exe"
  Delete "$DESKTOP\Proxy Gateway.lnk"
  Delete "$SMPROGRAMS\Proxy Gateway\Proxy Gateway.lnk"
  Delete "$SMPROGRAMS\Proxy Gateway\Restore Proxy.lnk"
  RMDir "$SMPROGRAMS\Proxy Gateway"
  DeleteRegKey HKCU "${REGKEY}"
  ; Non-recursive removal deliberately preserves user configuration, state and logs.
  RMDir "$INSTDIR\scripts"
  RMDir "$INSTDIR\licenses"
  RMDir "$INSTDIR\runtime"
  RMDir "$INSTDIR"
SectionEnd
