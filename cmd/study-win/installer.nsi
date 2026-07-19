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
;
; Deck packs are individual components, grouped: uncheck what you don't
; want. Non-solid compression on purpose — a skipped section then costs no
; extraction time (solid LZMA would decompress through everything anyway,
; which made installs slow). Cross-pack image borrowing pins two bundles:
; flags-by-region and world-capitals reuse world-flags' images, so the
; three ship as one component.

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
SetCompressor lzma

InstallDir "$LOCALAPPDATA\Programs\study"

Icon "study.ico"
UninstallIcon "study.ico"

!define UNINST "Software\Microsoft\Windows\CurrentVersion\Uninstall\study"
!define DECKS "$APPDATA\study\decks"

Page directory
Page components
Page instfiles
UninstPage uninstConfirm
UninstPage instfiles

; One deck pack as one optional component.
!macro DeckPack secname dir
Section "${secname}"
  SetOutPath "${DECKS}\${dir}"
  File /r "${DISTDIR}\decks\${dir}\*"
SectionEnd
!macroend

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

  ; A Start Menu folder with the app and the discoverable uninstall
  ; (Settings > Apps has it too).
  CreateDirectory "$SMPROGRAMS\study"
  CreateShortcut "$SMPROGRAMS\study\study.lnk" "$INSTDIR\study.exe"
  CreateShortcut "$SMPROGRAMS\study\Uninstall study.lnk" "$INSTDIR\uninstall.exe"
SectionEnd

SectionGroup "Languages (audio, the big ones)"
  Section "Japanese"
    SetOutPath "${DECKS}\language-packs\study-japanese.deck"
    File /r "${DISTDIR}\decks\language-packs\study-japanese.deck\*"
    SetOutPath "${DECKS}\study-japanese-numbers.deck"
    File /r "${DISTDIR}\decks\study-japanese-numbers.deck\*"
    SetOutPath "${DECKS}\study-mahjong.deck"
    File /r "${DISTDIR}\decks\study-mahjong.deck\*"
  SectionEnd
  Section "Farsi"
    SetOutPath "${DECKS}\language-packs\study-farsi.deck"
    File /r "${DISTDIR}\decks\language-packs\study-farsi.deck\*"
    SetOutPath "${DECKS}\study-farsi-numbers.deck"
    File /r "${DISTDIR}\decks\study-farsi-numbers.deck\*"
  SectionEnd
  Section "Mandarin Chinese"
    SetOutPath "${DECKS}\language-packs\study-mandarin-chinese.deck"
    File /r "${DISTDIR}\decks\language-packs\study-mandarin-chinese.deck\*"
    SetOutPath "${DECKS}\study-chinese-numbers.deck"
    File /r "${DISTDIR}\decks\study-chinese-numbers.deck\*"
    SetOutPath "${DECKS}\study-chinese-mahjong-tiles.deck"
    File /r "${DISTDIR}\decks\study-chinese-mahjong-tiles.deck\*"
    SetOutPath "${DECKS}\study-chinese-mahjong-terms.deck"
    File /r "${DISTDIR}\decks\study-chinese-mahjong-terms.deck\*"
  SectionEnd
  Section "Spanish (Colombian)"
    SetOutPath "${DECKS}\language-packs\study-colombian-spanish.deck"
    File /r "${DISTDIR}\decks\language-packs\study-colombian-spanish.deck\*"
  SectionEnd
  !insertmacro DeckPack "Spanish (Mexican)" "study-mexican-spanish.deck"
  Section "Portuguese (Brazilian)"
    SetOutPath "${DECKS}\language-packs\study-brazilian-portuguese.deck"
    File /r "${DISTDIR}\decks\language-packs\study-brazilian-portuguese.deck\*"
  SectionEnd
SectionGroupEnd

SectionGroup "Geography"
  Section "World Flags, Capitals & Regions"
    ; One component: flags-by-region and world-capitals borrow the
    ; world-flags images.
    SetOutPath "${DECKS}\study-world-flags.deck"
    File /r "${DISTDIR}\decks\study-world-flags.deck\*"
    SetOutPath "${DECKS}\study-flags-by-region.deck"
    File /r "${DISTDIR}\decks\study-flags-by-region.deck\*"
    SetOutPath "${DECKS}\study-world-capitals.deck"
    File /r "${DISTDIR}\decks\study-world-capitals.deck\*"
  SectionEnd
  !insertmacro DeckPack "Locator Maps" "study-locator-maps.deck"
  !insertmacro DeckPack "Country Silhouettes" "study-country-silhouettes.deck"
  !insertmacro DeckPack "Borders" "study-borders.deck"
  !insertmacro DeckPack "Bodies of Water" "study-waters.deck"
  !insertmacro DeckPack "World Landmarks" "study-world-landmarks.deck"
  !insertmacro DeckPack "Canada" "study-canada.deck"
SectionGroupEnd

SectionGroup "British Columbia"
  !insertmacro DeckPack "BC Driving" "study-bc-driving.deck"
  !insertmacro DeckPack "BC Birds (audio)" "study-bc-birds.deck"
SectionGroupEnd

SectionGroup "Trivia"
  !insertmacro DeckPack "Trivia Grab Bag" "study-speed-trivia.deck"
  !insertmacro DeckPack "Which Is Bigger?" "study-which-is-bigger.deck"
SectionGroupEnd

SectionGroup "More"
  !insertmacro DeckPack "US Presidents" "study-us-presidents.deck"
  !insertmacro DeckPack "Dog Breeds" "study-dog-breeds.deck"
  !insertmacro DeckPack "Animal Tracks" "study-animal-tracks.deck"
SectionGroupEnd

Section "Desktop shortcut"
  CreateShortcut "$DESKTOP\study.lnk" "$INSTDIR\study.exe"
SectionEnd

Section "Uninstall"
  Delete "$SMPROGRAMS\study\study.lnk"
  Delete "$SMPROGRAMS\study\Uninstall study.lnk"
  RMDir "$SMPROGRAMS\study"
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
