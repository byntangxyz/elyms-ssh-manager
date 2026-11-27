; elyms Windows x64 installer definition.
;
; Build with Inno Setup 6:
;   ISCC.exe installer\elyms.iss
;
; Input:  dist\elyms.exe (produced by scripts\build-installer.ps1)
; Output: dist\elyms-setup.exe
;
; This installer only deploys the application binary and its shortcuts.
; It never creates, reads, or modifies any user SSH configuration or keys.

#define MyAppName "elyms"
#define MyAppVersion "0.1.0"
#define MyAppPublisher "byntangxyz"
#define MyAppExeName "elyms.exe"
#define MyAppURL "https://github.com/byntangxyz/elyms-ssh-manager"

[Setup]
AppId={{5E1B4C2A-7D93-4F6B-9A18-C3D2E5B70F41}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppVerName={#MyAppName} {#MyAppVersion} by {#MyAppPublisher}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
AppUpdatesURL={#MyAppURL}
AppComments=elyms {#MyAppVersion} by {#MyAppPublisher}. Requires the Windows OpenSSH Client (ssh.exe), which is a runtime prerequisite and is not bundled.
VersionInfoVersion={#MyAppVersion}
VersionInfoCompany={#MyAppPublisher}
VersionInfoDescription={#MyAppName} {#MyAppVersion} installer by {#MyAppPublisher}
VersionInfoCopyright=Copyright (C) 2026 {#MyAppPublisher}
DefaultDirName={autopf}\{#MyAppName}
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=yes
UninstallDisplayName={#MyAppName}
UninstallDisplayIcon={app}\{#MyAppExeName}
OutputDir=..\dist
OutputBaseFilename=elyms-setup
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
ArchitecturesAllowed=x64
ArchitecturesInstallIn64BitMode=x64
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
ChangesEnvironment=yes
CloseApplications=yes
RestartApplications=no
SetupLogging=yes

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"
Name: "launchapp"; Description: "Launch {#MyAppName}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked

[Files]
Source: "..\dist\{#MyAppExeName}"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; WorkingDir: "{app}"; Comment: "{#MyAppName} {#MyAppVersion} by {#MyAppPublisher}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; WorkingDir: "{app}"; Comment: "{#MyAppName} {#MyAppVersion} by {#MyAppPublisher}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#MyAppExeName}"; WorkingDir: "{app}"; Description: "{cm:LaunchProgram,{#StringChange(MyAppName, '&', '&&')}}"; Flags: nowait postinstall skipifsilent; Tasks: launchapp

[Code]
const
  EnvironmentKey = 'Environment';
  PathValueName = 'Path';

function NormalizePathEntry(const Entry: string): string;
begin
  Result := Trim(Entry);
  while (Length(Result) > 0) and (Result[Length(Result)] = '\') do
    Delete(Result, Length(Result), 1);
end;

function PathContainsEntry(const PathValue, Entry: string): Boolean;
var
  Remaining, Item, Target: string;
  P: Integer;
begin
  Result := False;
  Target := NormalizePathEntry(Entry);
  if Target = '' then
    Exit;

  Remaining := PathValue;
  while Remaining <> '' do
  begin
    P := Pos(';', Remaining);
    if P = 0 then
    begin
      Item := Remaining;
      Remaining := '';
    end
    else
    begin
      Item := Copy(Remaining, 1, P - 1);
      Delete(Remaining, 1, P);
    end;

    if CompareText(NormalizePathEntry(Item), Target) = 0 then
    begin
      Result := True;
      Exit;
    end;
  end;
end;

function NeedsAddPath(AppDir: string): Boolean;
var
  PathValue: string;
begin
  if not RegQueryStringValue(HKCU, EnvironmentKey, PathValueName, PathValue) then
    PathValue := '';
  Result := not PathContainsEntry(PathValue, AppDir);
end;

// Append the install directory once, preserving every existing entry verbatim.
procedure AddPathEntry(AppDir: string);
var
  PathValue, Target: string;
  EndsWithSemicolon: Boolean;
begin
  if not NeedsAddPath(AppDir) then
    Exit;

  if not RegQueryStringValue(HKCU, EnvironmentKey, PathValueName, PathValue) then
    PathValue := '';

  Target := Trim(AppDir);
  while (Length(Target) > 0) and (Target[Length(Target)] = '\') do
    Delete(Target, Length(Target), 1);

  EndsWithSemicolon := (Length(PathValue) > 0) and (PathValue[Length(PathValue)] = ';');
  if (PathValue <> '') and not EndsWithSemicolon then
    PathValue := PathValue + ';';

  RegWriteExpandStringValue(HKCU, EnvironmentKey, PathValueName, PathValue + Target);
end;

procedure RemovePathEntry(AppDir: string);
var
  PathValue, NewValue, Remaining, Item, Target: string;
  P: Integer;
begin
  if not RegQueryStringValue(HKCU, EnvironmentKey, PathValueName, PathValue) then
    Exit;

  Target := NormalizePathEntry(AppDir);
  if Target = '' then
    Exit;

  NewValue := '';
  Remaining := PathValue;
  while Remaining <> '' do
  begin
    P := Pos(';', Remaining);
    if P = 0 then
    begin
      Item := Remaining;
      Remaining := '';
    end
    else
    begin
      Item := Copy(Remaining, 1, P - 1);
      Delete(Remaining, 1, P);
    end;

    // Keep every entry except an exact match for the install directory.
    if (Item <> '') and (CompareText(NormalizePathEntry(Item), Target) <> 0) then
    begin
      if NewValue <> '' then
        NewValue := NewValue + ';';
      NewValue := NewValue + Item;
    end;
  end;

  if NewValue <> PathValue then
    RegWriteExpandStringValue(HKCU, EnvironmentKey, PathValueName, NewValue);
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  // Apply the user-scoped PATH entry after files and shortcuts are in place.
  if CurStep = ssPostInstall then
    AddPathEntry(ExpandConstant('{app}'));
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usPostUninstall then
    RemovePathEntry(ExpandConstant('{app}'));
end;
