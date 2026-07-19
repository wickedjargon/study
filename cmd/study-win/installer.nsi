; study's Windows installer, built with makensis on Linux:
;
;   makensis -DDISTDIR=../../dist/study-windows -DVERSION=1.0.0 installer.nsi
;
; (make study-setup drives it with the right paths.) Per-user install, no
; admin: the exe to %LOCALAPPDATA%\Programs\study, the decks to
; %APPDATA%\study\decks where study.exe looks for them and the user can edit
; them, Start Menu + optional desktop shortcut, a .deck file association,
; and an uninstaller registered in Settings → Apps. Progress data
; (%LOCALAPPDATA%\study) is only removed on uninstall if the user says so.

!ifndef DISTDIR
  !error "pass -DDISTDIR=<folder with study.exe and decks/>"
!endif
!ifndef VERSION
  !define VERSION "1.0.0"
!endif

Name "study"
OutFile "study-setup.exe"
Unicode true
RequestExecutionLevel user
SetCompressor /SOLID lzma

InstallDir "$LOCALAPPDATA\Programs\study"

Icon "study.ico"
UninstallIcon "study.ico"

!define UNINST "Software\Microsoft\Windows\CurrentVersion\Uninstall\study"

Page directory
Page components
Page instfiles
UninstPage uninstConfirm
UninstPage instfiles

Section "study (required)"
  SectionIn RO
  SetOutPath "$INSTDIR"
  File "${DISTDIR}\study.exe"
  WriteUninstaller "$INSTDIR\uninstall.exe"

  ; Settings → Apps entry.
  WriteRegStr HKCU "${UNINST}" "DisplayName" "study"
  WriteRegStr HKCU "${UNINST}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINST}" "Publisher" "study"
  WriteRegStr HKCU "${UNINST}" "DisplayIcon" "$INSTDIR\study.exe"
  WriteRegStr HKCU "${UNINST}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKCU "${UNINST}" "InstallLocation" "$INSTDIR"
  WriteRegDWORD HKCU "${UNINST}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINST}" "NoRepair" 1

  ; Double-clicking a .deck file opens it in study.
  WriteRegStr HKCU "Software\Classes\.deck" "" "study.deckfile"
  WriteRegStr HKCU "Software\Classes\study.deckfile" "" "study deck"
  WriteRegStr HKCU "Software\Classes\study.deckfile\DefaultIcon" "" "$INSTDIR\study.exe,0"
  WriteRegStr HKCU "Software\Classes\study.deckfile\shell\open\command" "" '"$INSTDIR\study.exe" "%1"'

  CreateShortcut "$SMPROGRAMS\study.lnk" "$INSTDIR\study.exe"
SectionEnd

Section "Deck packs"
  ; The full catalog, to the user-editable location study.exe reads.
  SetOutPath "$APPDATA\study\decks"
  File /r "${DISTDIR}\decks\*"
SectionEnd

Section "Desktop shortcut"
  CreateShortcut "$DESKTOP\study.lnk" "$INSTDIR\study.exe"
SectionEnd

Section "Uninstall"
  Delete "$SMPROGRAMS\study.lnk"
  Delete "$DESKTOP\study.lnk"
  Delete "$INSTDIR\study.exe"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"
  DeleteRegKey HKCU "${UNINST}"
  DeleteRegKey HKCU "Software\Classes\study.deckfile"
  DeleteRegKey HKCU "Software\Classes\.deck"

  ; The user's decks and progress outlive the program unless they opt out.
  MessageBox MB_YESNO|MB_ICONQUESTION \
    "Also delete your decks and study progress?$\n$\n(decks: $APPDATA\study$\nprogress: $LOCALAPPDATA\study)" \
    IDNO keep
  RMDir /r "$APPDATA\study"
  RMDir /r "$LOCALAPPDATA\study"
keep:
SectionEnd
