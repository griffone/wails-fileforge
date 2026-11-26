Unicode true
SetCompressor /SOLID lzma
SetCompressorDictSize 64

####
## Please note: Template replacements don't work in this file. They are provided with default defines like
## mentioned underneath.
## If the keyword is not defined, "wails_tools.nsh" will populate them.
## If they are defined here, "wails_tools.nsh" will not touch them. This allows you to use this project.nsi manually
## from outside of Wails for debugging and development of the installer.
##
## For development first make a wails nsis build to populate the "wails_tools.nsh":
## > wails build --target windows/amd64 --nsis
## Then you can call makensis on this file with specifying the path to your binary:
## For a AMD64 only installer:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app.exe
## For a ARM64 only installer:
## > makensis -DARG_WAILS_ARM64_BINARY=..\..\bin\app.exe
## For a installer with both architectures:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app-amd64.exe -DARG_WAILS_ARM64_BINARY=..\..\bin\app-arm64.exe
####
## The following information is taken from the wails_tools.nsh file, but they can be overwritten here.
####
## !define INFO_PROJECTNAME    "my-project" # Default "fileforge-desktop"
## !define INFO_COMPANYNAME    "My Company" # Default "My Company"
## !define INFO_PRODUCTNAME    "My Product Name" # Default "My Product"
## !define INFO_PRODUCTVERSION "1.1.0"     # Default "0.1.0"
## !define INFO_COPYRIGHT      "(c) Now, My Company" # Default "© now, My Company"
###
## !define PRODUCT_EXECUTABLE  "Application.exe"      # Default "${INFO_PROJECTNAME}.exe"
## !define UNINST_KEY_NAME     "UninstKeyInRegistry"  # Default "${INFO_COMPANYNAME}${INFO_PRODUCTNAME}"
####
## !define REQUEST_EXECUTION_LEVEL "admin"            # Default "admin"  see also https://nsis.sourceforge.io/Docs/Chapter4.html
####
## Product metadata
####
!define INFO_COMPANYNAME    "FileForge"
!define INFO_PRODUCTNAME    "FileForge Desktop"
!define INFO_PRODUCTVERSION "1.1.0"
!define INFO_COPYRIGHT      "© 2026 FileForge"

####
## Include the wails tools
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

# Enable HiDPI support. https://nsis.sourceforge.io/Reference/ManifestDPIAware
ManifestDPIAware true

!include "MUI2.nsh"

####
## Branding
####
!define MUI_WELCOMEFINISHPAGE_BITMAP "assets\welcome.bmp"
!define MUI_HEADERIMAGE
!define MUI_HEADERIMAGE_BITMAP "assets\header.bmp"
!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"

BrandingText "${INFO_PRODUCTNAME} ${INFO_PRODUCTVERSION}"
!define MUI_ABORTWARNING

# Default install folder, restored from the previous install when present. The
# uninstall key is written in the 64-bit registry view by wails.writeUninstaller;
# .onInit re-reads it from the same view.
InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}" # Default installing folder ($PROGRAMFILES is Program Files folder).
InstallDirRegKey HKLM "${UNINST_KEY}" "InstallDir"

####
## Installer pages
####
!define MUI_WELCOMEPAGE_TITLE "Welcome to the ${INFO_PRODUCTNAME} setup"
!define MUI_WELCOMEPAGE_TEXT "FileForge Desktop converts images, PDFs, documents and videos on your own PC. No accounts, no uploads, no cloud.$\r$\n$\r$\nThis wizard installs the desktop app. The local conversion tools download on first use from Settings > Tools & Models.$\r$\n$\r$\nClick Next to continue."
!insertmacro MUI_PAGE_WELCOME

!insertmacro MUI_PAGE_COMPONENTS

!define MUI_DIRECTORYPAGE_TEXT_TOP "Choose the folder where ${INFO_PRODUCTNAME} will be installed. The app and its local image engine need about 60 MB. You can keep the suggested folder or pick another one."
!define MUI_DIRECTORYPAGE_TEXT_DESTINATION "Destination folder"
!insertmacro MUI_PAGE_DIRECTORY

!insertmacro MUI_PAGE_INSTFILES

!define MUI_FINISHPAGE_TITLE "${INFO_PRODUCTNAME} is installed"
!define MUI_FINISHPAGE_TEXT "${INFO_PRODUCTNAME} is ready to use. Your files stay on this PC and nothing is uploaded.$\r$\n$\r$\nThe first conversion that needs a local tool downloads it automatically. You can review or reinstall the tools any time in Settings > Tools & Models; the default set needs internet access once and about 3 GB of disk space.$\r$\n$\r$\nClick Finish to close this wizard."
!define MUI_FINISHPAGE_RUN "$INSTDIR\${PRODUCT_EXECUTABLE}"
!define MUI_FINISHPAGE_RUN_TEXT "Launch ${INFO_PRODUCTNAME}"
!define MUI_FINISHPAGE_NOREBOOTSUPPORT
!insertmacro MUI_PAGE_FINISH

####
## Uninstaller pages
####
!define MUI_WELCOMEPAGE_TITLE "Remove ${INFO_PRODUCTNAME}"
!define MUI_WELCOMEPAGE_TEXT "This wizard removes ${INFO_PRODUCTNAME} and the data it created for this Windows user: the downloaded tools and models, settings, job history, the app trash and the WebView2 profile.$\r$\n$\r$\nFiles you converted are not deleted.$\r$\n$\r$\nClick Next to continue."
!insertmacro MUI_UNPAGE_WELCOME

!define MUI_UNCONFIRMPAGE_TEXT_TOP "Uninstalling removes the app, local tools (FFmpeg, Tesseract, Poppler, Ghostscript, Python, Whisper, models and the background removal pack), settings, history, trash and WebView2 data for this Windows user. Converted files are not deleted."
!insertmacro MUI_UNPAGE_CONFIRM

!insertmacro MUI_UNPAGE_INSTFILES

!define MUI_FINISHPAGE_TITLE "${INFO_PRODUCTNAME} was removed"
!define MUI_FINISHPAGE_TEXT "${INFO_PRODUCTNAME} and its local tools and app data have been removed.$\r$\n$\r$\nYour converted files are still where you saved them. The Microsoft Edge WebView2 runtime is shared with other apps and stays installed.$\r$\n$\r$\nClick Finish to close this wizard."
!insertmacro MUI_UNPAGE_FINISH

!insertmacro MUI_LANGUAGE "English" # Set the Language of the installer

## The following two statements can be used to sign the installer and the uninstaller. The path to the binaries are provided in %1
#!uninstfinalize 'signtool --file "%1"'
#!finalize 'signtool --file "%1"'

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe" # Name of the installer's file.
ShowInstDetails nevershow
ShowUninstDetails nevershow

####
## Functions
####

# Ask the user to close the running app before files in $INSTDIR are replaced.
# FindWindow matches the exact window title set by the Wails window options.
Function CheckAppRunning
    # Silent installs cannot answer a prompt; let the file copy report errors.
    ${If} ${Silent}
        Return
    ${EndIf}

    appRunningInstallCheck:
        FindWindow $0 "${INFO_PRODUCTNAME}"
        ${If} $0 == 0
            Return
        ${EndIf}
        MessageBox MB_OKCANCEL|MB_ICONEXCLAMATION \
            "${INFO_PRODUCTNAME} is still running. Close it, then choose OK to continue. Choose Cancel to stop the setup." \
            IDOK appRunningInstallCheck IDCANCEL appRunningInstallAbort

    appRunningInstallAbort:
        Abort
FunctionEnd

Function un.CheckAppRunning
    ${If} ${Silent}
        Return
    ${EndIf}

    appRunningUninstallCheck:
        FindWindow $0 "${INFO_PRODUCTNAME}"
        ${If} $0 == 0
            Return
        ${EndIf}
        MessageBox MB_OKCANCEL|MB_ICONEXCLAMATION \
            "${INFO_PRODUCTNAME} is still running. Close it, then choose OK to continue. Choose Cancel to stop the uninstall." \
            IDOK appRunningUninstallCheck IDCANCEL appRunningUninstallAbort

    appRunningUninstallAbort:
        Abort
FunctionEnd

Function .onInit
    !insertmacro wails.checkArchitecture

    # InstallDirRegKey reads the 32-bit registry view at startup, while
    # wails.writeUninstaller stores InstallDir in the 64-bit view. Read the
    # remembered destination again here so the directory page starts there.
    SetRegView 64
    ReadRegStr $0 HKLM "${UNINST_KEY}" "InstallDir"
    ${If} $0 != ""
        StrCpy $INSTDIR $0
    ${EndIf}
FunctionEnd

# Remove everything the app created for the current Windows user. Converted
# files live outside these folders and are never touched.
Function un.RemoveAppData
    # The uninstaller runs elevated, but the data belongs to the profile of the
    # user who started it.
    SetShellVarContext current

    DetailPrint "Removing the managed tools, models and Python environments..."
    RMDir /r "$LOCALAPPDATA\FileForge"
    RMDir /r "$LOCALAPPDATA\fileforge"

    DetailPrint "Removing settings, job history and automation data..."
    RMDir /r "$APPDATA\fileforge"
    RMDir /r "$APPDATA\FileForge"

    DetailPrint "Removing the preview cache..."
    RMDir /r "$TEMP\fileforge-previews"

    DetailPrint "Removing the WebView2 profile for this Windows user..."
    RMDir /r "$APPDATA\${PRODUCT_EXECUTABLE}"
    RMDir /r "$LOCALAPPDATA\${PRODUCT_EXECUTABLE}"
    RMDir /r "$LOCALAPPDATA\${PRODUCT_EXECUTABLE}.WebView2"
FunctionEnd

####
## Installer sections
####
Section "FileForge Desktop" SecApp
    SectionIn RO

    Call CheckAppRunning

    # An admin install uses the all-users context: shortcuts land on the public
    # desktop and in the common Start Menu.
    !insertmacro wails.setShellContext

    DetailPrint "Checking the Microsoft Edge WebView2 runtime..."
    !insertmacro wails.webview2runtime

    DetailPrint "Copying the FileForge Desktop application files..."
    SetOutPath $INSTDIR
    !insertmacro wails.files

    # wails.files copies only the desktop exe, so the headless CLI is added
    # explicitly. It lands in $INSTDIR beside the app and shares the bundled
    # libvips DLLs; the uninstaller removes $INSTDIR recursively.
    !ifdef FILEFORGE_CLI_BINARY
        File "/oname=fileforge-cli.exe" "${FILEFORGE_CLI_BINARY}"
    !endif

    # Bundle the libvips runtime next to the executable. bimg links it at
    # startup, so this cannot be installed later by the in-app toolchain.
    !ifdef LIBVIPS_DIR
        DetailPrint "Installing the local image engine (libvips)..."
        File /r "${LIBVIPS_DIR}/*.dll"
    !endif

    DetailPrint "Registering file types and the uninstaller..."
    !insertmacro wails.associateFiles
    !insertmacro wails.writeUninstaller

    # Remember the destination the user picked. wails.writeUninstaller already
    # selected the 64-bit registry view, so this lands next to the uninstall data.
    WriteRegStr HKLM "${UNINST_KEY}" "InstallDir" "$INSTDIR"
SectionEnd

Section "Desktop shortcut" SecDesktopShortcut
    !insertmacro wails.setShellContext
    DetailPrint "Creating the desktop shortcut..."
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
SectionEnd

Section "Start Menu shortcut" SecStartMenuShortcut
    !insertmacro wails.setShellContext
    DetailPrint "Creating the Start Menu shortcut..."
    CreateShortCut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
SectionEnd

!insertmacro MUI_FUNCTION_DESCRIPTION_BEGIN
    !insertmacro MUI_DESCRIPTION_TEXT ${SecApp} "The ${INFO_PRODUCTNAME} application, its local image engine and the uninstaller."
    !insertmacro MUI_DESCRIPTION_TEXT ${SecDesktopShortcut} "Add a ${INFO_PRODUCTNAME} shortcut to the desktop."
    !insertmacro MUI_DESCRIPTION_TEXT ${SecStartMenuShortcut} "Add ${INFO_PRODUCTNAME} to the Start Menu."
!insertmacro MUI_FUNCTION_DESCRIPTION_END

####
## Uninstaller section
####
Section "Uninstall"
    Call un.CheckAppRunning

    !insertmacro wails.setShellContext

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles

    !insertmacro wails.deleteUninstaller

    Call un.RemoveAppData

    # ProgramData variant of the WebView2 data path, then the install folder.
    SetShellVarContext all
    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}"
    RMDir /r "$INSTDIR"
SectionEnd
