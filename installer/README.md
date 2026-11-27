# elyms Windows installer

This directory contains the [Inno Setup](https://jrsoftware.org/isinfo.php) definition used to
package the Windows x64 build of **elyms** by **byntangxyz**.

## Requirements

- Windows 10 or Windows 11 (x64)
- Go 1.25.0 or newer on the build machine
- **Inno Setup 6** — provides the `ISCC.exe` command-line compiler
  - Download: <https://jrsoftware.org/isdl.php>
  - Default install location: `C:\Program Files (x86)\Inno Setup 6\ISCC.exe`

Windows OpenSSH (`ssh.exe`) is a **runtime** prerequisite for the installed application. It is
**not** bundled by this installer and must be enabled separately by the user (Settings →
Apps → Optional features → OpenSSH Client). The installer never touches `%USERPROFILE%\.ssh`,
SSH configuration, or private keys.

## Build

From the repository root:

```powershell
pwsh -File scripts\build-installer.ps1
```

The script runs `go test ./...`, builds `dist\elyms.exe` (Windows amd64, `-trimpath`,
`-ldflags "-s -w"`), and then compiles the installer with `ISCC.exe`.

To compile only the installer by hand, after `dist\elyms.exe` already exists:

```powershell
& "C:\Program Files (x86)\Inno Setup 6\ISCC.exe" installer\elyms.iss
```

## Output

| Artifact | Path |
| --- | --- |
| Application binary | `dist\elyms.exe` |
| Installer | `dist\elyms-setup.exe` |

The installer output path is set by `OutputDir`/`OutputBaseFilename` in `elyms.iss`.

### Missing external tool

`scripts\build-installer.ps1` fails fast with
`error: Inno Setup 6 compiler (ISCC.exe) not found` when Inno Setup 6 is not installed.
`ISCC.exe` is **not** a Go dependency and is not present on every build machine, so a checkout
without it can still produce the application binary but not `dist\elyms-setup.exe`:

```powershell
$env:GOTOOLCHAIN='local'; $env:GOOS='windows'; $env:GOARCH='amd64'; $env:CGO_ENABLED='0'
go build -trimpath -ldflags '-s -w' -o dist\elyms.exe ./cmd/elyms
```

Install Inno Setup 6 (or add `ISCC.exe` to `PATH`), then re-run the script to produce the
installer. A failed script run leaves the previously built `dist\elyms.exe` untouched.

## What the installer does

- Installs `elyms.exe` into `{autopf}\elyms` (per-user if not elevated, `Program Files` otherwise).
- Creates a Start Menu shortcut and an optional Desktop shortcut.
- Appends the install directory to the **current user's** `PATH` (`HKCU\Environment\Path`)
  only when it is not already present, so re-running the installer does not duplicate the entry.
- Registers an uninstaller that removes only that exact install directory from `PATH`.
- Ships no `.ssh` paths or file operations; existing hosts, config, and keys are left untouched.

After installation, open a **new** terminal so the updated `PATH` is picked up, then run:

```powershell
elyms
```

## Version

The installer version comes from the single `#define MyAppVersion` constant at the top of
`elyms.iss`. Update that value when releasing a new build; the app metadata, `VersionInfoVersion`,
and the installer file name derive from it.

## Manual verification

Installing, uninstalling, shortcut behavior, and live SSH connections still require manual testing
on a real Windows machine:

1. Run `dist\elyms-setup.exe` and complete the wizard.
2. Confirm the Start Menu (and Desktop, if selected) shortcut opens elyms.
3. Open a new terminal and confirm `elyms` resolves on `PATH`.
4. Uninstall via Settings → Apps, then confirm `elyms` is gone from `PATH` and unrelated
   `PATH` entries remain intact.
