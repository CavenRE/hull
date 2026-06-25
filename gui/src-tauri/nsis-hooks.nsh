; Hull NSIS installer hooks.
;  - POSTINSTALL:   add the install dir to the per-user PATH (idempotent) so the
;                   bundled `hull` CLI works from any terminal.
;  - PREUNINSTALL:  stop hull-gui/hulld so their files aren't locked (the usual
;                   cause of "unable to uninstall").
;  - POSTUNINSTALL: remove the install dir from PATH again (no stale entry).
!include "StrFunc.nsh"
!include "WinMessages.nsh"
${StrStr}
${UnStrRep}

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

!macro NSIS_HOOK_PREUNINSTALL
  ; Free file locks before removal — otherwise the uninstall stalls.
  nsExec::Exec 'taskkill /F /IM hull-gui.exe'
  nsExec::Exec 'taskkill /F /IM hulld.exe'
  Sleep 600
!macroend

!macro NSIS_HOOK_POSTUNINSTALL
  ; Strip the install dir from the per-user PATH (all three positions).
  Push $0
  Push $1
  ReadRegStr $0 HKCU "Environment" "Path"
  ${UnStrRep} $1 "$0" ";$INSTDIR" ""
  ${UnStrRep} $1 "$1" "$INSTDIR;" ""
  ${UnStrRep} $1 "$1" "$INSTDIR" ""
  WriteRegExpandStr HKCU "Environment" "Path" "$1"
  SendMessage ${HWND_BROADCAST} ${WM_WININICHANGE} 0 "STR:Environment" /TIMEOUT=5000
  Pop $1
  Pop $0
!macroend
