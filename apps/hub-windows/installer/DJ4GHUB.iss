#ifndef RepoRoot
  #error RepoRoot must point to the repository root
#endif
#define AppVersion "0.6.7"

[Setup]
AppId={{8E8A13B6-437C-4F92-AD79-AF4B12978465}
AppName=DJ4GHUB · 老李 Oldlee
AppVersion={#AppVersion}
AppPublisher=老李 Oldlee
AppPublisherURL=https://x.com/oldleeoo
AppSupportURL=https://github.com/Oldleeo/DJ4GHUB/issues
AppUpdatesURL=https://github.com/Oldleeo/DJ4GHUB/releases
DefaultDirName={localappdata}\Programs\DJ4GHUB
DefaultGroupName=DJ4GHUB
PrivilegesRequired=lowest
ArchitecturesAllowed=x64os
ArchitecturesInstallIn64BitMode=x64os
MinVersion=10.0.17763
AppMutex=Local\DJ4Hub.Windows
CloseApplications=no
RestartApplications=no
DisableWelcomePage=yes
DisableProgramGroupPage=yes
DisableDirPage=auto
LicenseFile={#RepoRoot}\LICENSE
InfoBeforeFile={#RepoRoot}\dist\windows\prerequisites\INSTALL-INFO.txt
SetupIconFile={#RepoRoot}\apps\hub-windows\src\DJ4Hub.Windows\Assets\dj-4g-hub-icon.ico
UninstallDisplayIcon={app}\DJ4Hub.Windows.exe
OutputDir={#RepoRoot}\dist\releases
OutputBaseFilename=DJ4GHUB-Windows-x64-{#AppVersion}-Setup
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
WizardSizePercent=110
SetupLogging=yes

[Messages]
ButtonBack=< 上一步
ButtonNext=下一步 >
ButtonInstall=安装
ButtonCancel=取消
ButtonFinish=完成
WizardLicense=使用许可
LicenseLabel=安装前请阅读以下许可。
LicenseLabel3=必须接受本许可才能继续安装。项目为非商业许可；保留全部上游版权。
LicenseAccepted=我接受许可条款
LicenseNotAccepted=我不接受许可条款
WizardInfoBefore=安装说明
InfoBeforeLabel=本地直接使用，云同步可选。
InfoBeforeClickLabel=请阅读说明，然后点击“下一步”。
WizardSelectDir=安装位置
SelectDirDesc=将 DJ4GHUB 安装到哪里？
WizardReady=准备安装
ReadyLabel1=点击“安装”开始。无需另外部署本地后台。
WizardInstalling=正在安装
InstallingLabel=正在安装 DJ4GHUB，请稍候…
WizardFinished=安装完成
FinishedLabel=DJ4GHUB 已安装到电脑。点击“完成”即可启动。

[Files]
Source: "{#RepoRoot}\dist\windows\portable\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#RepoRoot}\dist\windows\prerequisites\MicrosoftEdgeWebview2Setup.exe"; Flags: dontcopy

[Icons]
Name: "{autodesktop}\DJ4GHUB"; Filename: "{app}\DJ4Hub.Windows.exe"; WorkingDir: "{app}"
Name: "{group}\DJ4GHUB"; Filename: "{app}\DJ4Hub.Windows.exe"; WorkingDir: "{app}"
Name: "{group}\使用指南"; Filename: "https://github.com/Oldleeo/DJ4GHUB/blob/main/docs/QUICK_START.md"

[Run]
Filename: "{app}\DJ4Hub.Windows.exe"; Description: "启动 DJ4GHUB"; Flags: nowait postinstall skipifsilent

[Code]
const
  WebViewKey = 'Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}';

function ValidRuntimeVersion(Root: Integer): Boolean;
var
  Version: String;
begin
  Result := RegQueryStringValue(Root, WebViewKey, 'pv', Version) and
    (Version <> '') and (Version <> '0.0.0.0');
end;

function HasWebView2: Boolean;
begin
  Result := ValidRuntimeVersion(HKLM32) or ValidRuntimeVersion(HKCU32) or
    ValidRuntimeVersion(HKCU64);
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  ExitCode: Integer;
begin
  Result := '';
  if HasWebView2 then exit;
  WizardForm.StatusLabel.Caption := '正在安装 Microsoft WebView2（首次安装需要联网）…';
  ExtractTemporaryFile('MicrosoftEdgeWebview2Setup.exe');
  if not Exec(ExpandConstant('{tmp}\MicrosoftEdgeWebview2Setup.exe'),
      '/silent /install', '', SW_HIDE, ewWaitUntilTerminated, ExitCode) then begin
    Result := '无法启动 Microsoft WebView2 安装程序。请检查网络后重试。';
    exit;
  end;
  Log(Format('WebView2 bootstrapper exit code: %d', [ExitCode]));
  if not HasWebView2 then
    Result := '未检测到 WebView2 Runtime。请联网后重试，或从 Microsoft 官方网站安装 WebView2。';
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  StartupCommand: String;
begin
  if CurUninstallStep = usPostUninstall then begin
    { Remove auto-start only when it belongs to this installation. Never erase user data. }
    if RegQueryStringValue(HKCU, 'Software\Microsoft\Windows\CurrentVersion\Run',
        'DJ4GHUB', StartupCommand) and
        (Pos('"' + ExpandConstant('{app}\DJ4Hub.Windows.exe') + '"', StartupCommand) = 1) then
      RegDeleteValue(HKCU, 'Software\Microsoft\Windows\CurrentVersion\Run', 'DJ4GHUB');
  end;
end;
