; Hull NSIS installer hook — add the install directory to the per-user PATH so
; the bundled `hull` CLI works from any terminal (idempotent; skips if already
; present). PATH is left intact on uninstall (a stale entry is harmless).
!include "StrFunc.nsh"
!include "WinMessages.nsh"
${StrStr}

!macro NSIS_HOOK_POSTINSTALL
  Push $0
  Push $1
  ReadRegStr $0 HKCU "Environment" "Path"
  ${StrStr} $1 "$0" "$INSTDIR"
  StrCmp $1 "" 0 hull_path_done
    StrCmp $0 "" 0 hull_path_append
      WriteRegExpandStr HKCU "Environment" "Path" "$INSTDIR"
      Goto hull_path_notify
    hull_path_append:
      WriteRegExpandStr HKCU "Environment" "Path" "$0;$INSTDIR"
    hull_path_notify:
      SendMessage ${HWND_BROADCAST} ${WM_WININICHANGE} 0 "STR:Environment" /TIMEOUT=5000
  hull_path_done:
  Pop $1
  Pop $0
!macroend
