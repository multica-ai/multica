!include LogicLib.nsh
!include StrFunc.nsh
!include WinMessages.nsh

${StrStr}

!define MULTICA_CLI_BIN "$INSTDIR\resources\app.asar.unpacked\resources\bin"

!macro AddMulticaCliToUserPath
  Push $0
  Push $1
  Push $2

  ClearErrors
  ReadRegStr $0 HKCU "Environment" "Path"
  ${If} ${Errors}
    StrCpy $0 ""
  ${EndIf}

  ${StrStr} $1 "$0" "${MULTICA_CLI_BIN}"
  ${If} $1 == ""
    ${If} $0 == ""
      StrCpy $2 "${MULTICA_CLI_BIN}"
    ${Else}
      StrCpy $2 "$0;${MULTICA_CLI_BIN}"
    ${EndIf}

    WriteRegExpandStr HKCU "Environment" "Path" "$2"
    SendMessage ${HWND_BROADCAST} ${WM_SETTINGCHANGE} 0 "STR:Environment" /TIMEOUT=5000
  ${EndIf}

  Pop $2
  Pop $1
  Pop $0
!macroend

!macro customInstall
  !insertmacro AddMulticaCliToUserPath
!macroend
