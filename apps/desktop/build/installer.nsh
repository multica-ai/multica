!include WinMessages.nsh

!macro customInstall
  DetailPrint "Adding Multica CLI to the user PATH"
  nsExec::ExecToLog `powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "& { param([string]$$bin); $$oldPath = [Environment]::GetEnvironmentVariable('Path', 'User'); $$entries = @(); if ($$oldPath) { $$entries = $$oldPath -split ';' | Where-Object { $$_.Trim().Length -gt 0 } }; $$normalizedBin = $$bin.TrimEnd('\'); $$filtered = $$entries | Where-Object { $$normalized = $$_.Trim().TrimEnd('\'); $$normalized -ne $$normalizedBin -and $$normalized -notlike '*\Multica\resources\bin' -and $$normalized -notlike '*\Multica\resources\app.asar.unpacked\resources\bin' }; $$newPath = (@($$filtered) + $$bin) -join ';'; [Environment]::SetEnvironmentVariable('Path', $$newPath, 'User'); [Environment]::SetEnvironmentVariable('Path', $$newPath, 'Process') }" "$INSTDIR\resources\app.asar.unpacked\resources\bin"`
  DetailPrint "Restart open terminals so they can see the updated PATH"
  System::Call 'user32::SendMessageTimeout(i 0xffff, i ${WM_SETTINGCHANGE}, i 0, t "Environment", i 0, i 5000, *i .r0)'
!macroend

!macro customUnInstall
  DetailPrint "Removing Multica CLI from the user PATH"
  nsExec::ExecToLog `powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "& { param([string]$$bin); $$oldPath = [Environment]::GetEnvironmentVariable('Path', 'User'); if ($$oldPath) { $$normalizedBin = $$bin.TrimEnd('\'); $$entries = $$oldPath -split ';' | Where-Object { $$_.Trim().Length -gt 0 -and $$_.Trim().TrimEnd('\') -ne $$normalizedBin }; $$newPath = $$entries -join ';'; [Environment]::SetEnvironmentVariable('Path', $$newPath, 'User'); [Environment]::SetEnvironmentVariable('Path', $$newPath, 'Process') } }" "$INSTDIR\resources\app.asar.unpacked\resources\bin"`
  System::Call 'user32::SendMessageTimeout(i 0xffff, i ${WM_SETTINGCHANGE}, i 0, t "Environment", i 0, i 5000, *i .r0)'
!macroend
