using System.ComponentModel;
using System.Diagnostics;
using System.IO;
using System.Net.Http;
using System.Text.Json;
using System.Windows;
using System.Windows.Media;
using System.Windows.Threading;
using Microsoft.Win32;
using Microsoft.Web.WebView2.Core;
using DJ4Hub.Windows.Services;

namespace DJ4Hub.Windows;

public partial class MainWindow : Window
{
    private readonly Dictionary<string, BackendProcessService> _backends = new(StringComparer.OrdinalIgnoreCase);
    private readonly SemaphoreSlim _deviceScanGate = new(1, 1);
    private readonly DispatcherTimer _deviceScanTimer = new() { Interval = TimeSpan.FromSeconds(30) };
    private readonly DispatcherTimer _healthTimer = new() { Interval = TimeSpan.FromSeconds(10) };
    private readonly CancellationTokenSource _shutdown = new();
    private readonly HashSet<BackendProcessService> _recovering = [];
    private readonly Dictionary<BackendProcessService, int> _healthFailures = [];
    private bool _checkingHealth;
    private readonly bool _startMinimized = Environment.GetCommandLineArgs().Any(arg => string.Equals(arg, "--minimized", StringComparison.OrdinalIgnoreCase));
    private BackendProcessService? _activeBackend;
    private string _primaryDeviceKey = "";
    private System.Windows.Forms.NotifyIcon? _trayIcon;
    private bool _allowClose;
    private bool _shownTrayHint;
    private bool _initialNavigationCompleted;

    public MainWindow()
    {
        InitializeComponent();
        Loaded += MainWindow_Loaded;
        Closing += MainWindow_Closing;
        _deviceScanTimer.Tick += async (_, _) => await RescanDevicesAsync(showStatus: false);
        _healthTimer.Tick += async (_, _) => await CheckDeviceHostsAsync();
        CreateTrayIcon();
    }

    private async void MainWindow_Loaded(object sender, RoutedEventArgs e)
    {
        try
        {
            SetStatus("正在启动本地服务…", "#D68A20");
            await StartDeviceHostsAsync(CancellationToken.None);
            var localUri = _activeBackend?.BaseUri ?? throw new InvalidOperationException("没有可用的设备主机。");
            StartupDetail.Text = _backends.Count > 1
                ? $"已启动 {_backends.Count} 个独立模块主机，正在加载控制台…"
                : "本地服务已就绪，正在加载安全控制台…";

            var webViewData = Path.Combine(
                Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
                "DJ4Hub",
                "WebView2");
            Directory.CreateDirectory(webViewData);
            var environment = await CoreWebView2Environment.CreateAsync(userDataFolder: webViewData);
            await Browser.EnsureCoreWebView2Async(environment);
            ConfigureBrowser();
            Browser.Source = localUri;
            _deviceScanTimer.Start();
            _healthTimer.Start();
        }
        catch (Exception ex)
        {
            ShowStartupError(ex);
        }
    }

    private async Task StartDeviceHostsAsync(CancellationToken cancellationToken)
    {
        IReadOnlyList<DeviceDescriptor> devices;
        try
        {
            devices = await BackendProcessService.DiscoverDevicesAsync(cancellationToken);
            devices = devices.Where(device => device.Available).ToArray();
        }
        catch (Exception ex)
        {
            devices = [];
            StartupDetail.Text = $"设备扫描暂不可用，尝试兼容模式：{ex.Message}";
        }

        var root = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "DJ4Hub");
        Directory.CreateDirectory(root);
        _primaryDeviceKey = ResolvePrimaryDeviceKey(root, devices);

        if (devices.Count == 0)
        {
            var fallback = new BackendProcessService(dataDirectory: root);
            fallback.ExitedUnexpectedly += Backend_ExitedUnexpectedly;
            await fallback.StartAsync(cancellationToken);
            _backends["auto"] = fallback;
            _activeBackend = fallback;
            return;
        }

        var starts = devices.Select(async device =>
        {
            var dataDirectory = string.Equals(device.Key, _primaryDeviceKey, StringComparison.OrdinalIgnoreCase)
                ? root
                : Path.Combine(root, "Devices", device.Key);
            SeedNotificationSettings(root, dataDirectory);
            var backend = new BackendProcessService(device, dataDirectory);
            backend.ExitedUnexpectedly += Backend_ExitedUnexpectedly;
            try
            {
                await backend.StartAsync(cancellationToken);
                return (device.Key, Backend: backend, Error: (Exception?)null);
            }
            catch (Exception ex)
            {
                return (device.Key, Backend: backend, Error: ex);
            }
        }).ToArray();
        var results = await Task.WhenAll(starts);
        foreach (var result in results)
        {
            _backends[result.Key] = result.Backend;
            if (result.Error is not null) _ = RecoverBackendAsync(result.Backend, false);
        }
        if (_backends.Count == 0)
        {
            throw new InvalidOperationException("找到了 DJI 4G 模块，但所有模块主机都启动失败：\n" +
                string.Join("\n", results.Select(item => item.Error?.Message).Where(message => !string.IsNullOrWhiteSpace(message))));
        }
        _activeBackend = _backends.TryGetValue(_primaryDeviceKey, out var primary) && primary.IsRunning
            ? primary : _backends.Values.FirstOrDefault(backend => backend.IsRunning) ?? _backends.Values.First();
    }

    private static string ResolvePrimaryDeviceKey(string root, IReadOnlyList<DeviceDescriptor> devices)
    {
        var path = Path.Combine(root, "multi-device.json");
        try
        {
            if (File.Exists(path))
            {
                using var document = JsonDocument.Parse(File.ReadAllText(path));
                if (document.RootElement.TryGetProperty("primary_device_key", out var key))
                {
                    var value = key.GetString();
                    if (!string.IsNullOrWhiteSpace(value) && !string.Equals(value, "auto", StringComparison.OrdinalIgnoreCase)) return value;
                }
            }
        }
        catch (JsonException)
        {
            // Replace a damaged host mapping with the current primary device.
        }
        var selected = devices.FirstOrDefault()?.Key ?? "auto";
        if (string.Equals(selected, "auto", StringComparison.OrdinalIgnoreCase)) return selected;
        PersistPrimaryDeviceKey(root, selected);
        return selected;
    }

    private static void PersistPrimaryDeviceKey(string root, string selected)
    {
        var path = Path.Combine(root, "multi-device.json");
        var temporary = path + ".tmp";
        File.WriteAllText(temporary, JsonSerializer.Serialize(new { primary_device_key = selected }));
        File.Move(temporary, path, overwrite: true);
    }

    private static void SeedNotificationSettings(string primaryRoot, string deviceRoot)
    {
        if (string.Equals(primaryRoot, deviceRoot, StringComparison.OrdinalIgnoreCase)) return;
        var source = Path.Combine(primaryRoot, "DJ4Hub", "notifications.json");
        var destination = Path.Combine(deviceRoot, "DJ4Hub", "notifications.json");
        if (!File.Exists(source) || File.Exists(destination)) return;
        Directory.CreateDirectory(Path.GetDirectoryName(destination)!);
        File.Copy(source, destination, overwrite: false);
    }

    private void ConfigureBrowser()
    {
        var core = Browser.CoreWebView2;
        core.Settings.AreDevToolsEnabled = false;
        core.Settings.AreDefaultContextMenusEnabled = false;
        core.Settings.IsStatusBarEnabled = false;
        core.Settings.IsPasswordAutosaveEnabled = false;
        core.Settings.IsGeneralAutofillEnabled = false;
        // Let WebView2 handle real Windows caption dragging and double-clicks
        // for the transparent HTML app-region, while retaining HWND rendering.
        core.Settings.IsNonClientRegionSupportEnabled = true;
        Browser.ZoomFactor = 1.15;

        core.NavigationStarting += (_, args) =>
        {
            if (!IsAllowedLocalUri(args.Uri))
            {
                args.Cancel = true;
                OpenExternal(args.Uri);
            }
        };
        core.NewWindowRequested += (_, args) =>
        {
            args.Handled = true;
            OpenExternal(args.Uri);
        };
        core.PermissionRequested += (_, args) =>
        {
            var allowMicrophone = args.PermissionKind == CoreWebView2PermissionKind.Microphone
                && IsAllowedLocalUri(args.Uri);
            args.SavesInProfile = false;
            args.State = allowMicrophone
                ? CoreWebView2PermissionState.Allow
                : CoreWebView2PermissionState.Deny;
        };
        core.DownloadStarting += (_, args) =>
        {
            if (!IsAllowedLocalUri(args.DownloadOperation.Uri))
            {
                args.Cancel = true;
            }
        };
        core.WebMessageReceived += (_, args) =>
        {
            if (IsAllowedLocalUri(args.Source)) HandleWebMessage(args.WebMessageAsJson);
        };
        core.NavigationCompleted += async (_, args) =>
        {
            if (!args.IsSuccess)
            {
                // A newer module selection may cancel an older navigation. This is not a startup failure.
                if (args.WebErrorStatus == CoreWebView2WebErrorStatus.OperationCanceled) return;
                ShowStartupError(new InvalidOperationException($"控制台加载失败：{args.WebErrorStatus}"));
                return;
            }
            Browser.Visibility = Visibility.Visible;
            StartupPanel.Visibility = Visibility.Collapsed;
            PostWindowState();
            PostDeviceList();
            SetStatus(_backends.Count > 1 ? $"已连接 {_backends.Count} 块 DJI 4G 模块" : "已连接 DJI 4G 模块 · 本机控制模式", "#2E9D67");
            var isInitialNavigation = !_initialNavigationCompleted;
            _initialNavigationCompleted = true;
            if (isInitialNavigation)
            {
                await core.ExecuteScriptAsync("document.querySelector('[data-view=\"overview\"]')?.click();");
                if (_startMinimized)
                {
                    Hide();
                }
            }
        };
    }

    private bool IsAllowedLocalUri(string candidate)
    {
        return Uri.TryCreate(candidate, UriKind.Absolute, out var uri)
            && uri.Scheme == Uri.UriSchemeHttp
            && uri.Host == "127.0.0.1"
            && _backends.Values.Any(backend => backend.BaseUri?.Port == uri.Port);
    }

    private async Task OpenConfiguredCloudPanelAsync()
    {
        try
        {
            var baseUri = _activeBackend?.BaseUri ?? throw new InvalidOperationException("请先连接设备主机。");
            using var client = new HttpClient { Timeout = TimeSpan.FromSeconds(5) };
            using var response = await client.GetAsync(new Uri(baseUri, "api/remote"));
            response.EnsureSuccessStatusCode();
            using var document = JsonDocument.Parse(await response.Content.ReadAsStringAsync());
            var address = document.RootElement.GetProperty("relay_url").GetString();
            if (!Uri.TryCreate(address, UriKind.Absolute, out var relayUri) || relayUri.Scheme != "https"
                || !string.IsNullOrEmpty(relayUri.UserInfo) || !string.IsNullOrEmpty(relayUri.Query)
                || !string.IsNullOrEmpty(relayUri.Fragment))
                throw new InvalidOperationException("请在‘云同步’中填写自己的 HTTPS 中继地址。");
            OpenExternal(relayUri.AbsoluteUri.TrimEnd('/') + "/panel/");
        }
        catch (Exception ex)
        {
            System.Windows.MessageBox.Show(this, ex.Message, "云端尚未配置", MessageBoxButton.OK, MessageBoxImage.Information);
        }
    }

    private static void OpenExternal(string? value)
    {
        if (!Uri.TryCreate(value, UriKind.Absolute, out var uri) || uri.IsLoopback)
        {
            return;
        }
        Process.Start(new ProcessStartInfo(uri.AbsoluteUri) { UseShellExecute = true });
    }

    private void HandleWebMessage(string raw)
    {
        try
        {
            using var json = JsonDocument.Parse(raw);
            var root = json.RootElement;
            if (!root.TryGetProperty("type", out var typeElement))
            {
                return;
            }
            switch (typeElement.GetString())
            {
                case "window-action" when root.TryGetProperty("action", out var actionElement):
                    switch (actionElement.GetString())
                    {
                        case "minimize": SystemCommands.MinimizeWindow(this); break;
                        case "maximize": ChromeMaximize_Click(this, new RoutedEventArgs()); break;
                        case "close": Close(); break;
                    }
                    break;
                case "window-theme" when root.TryGetProperty("theme", out var themeElement):
                    ApplyWindowTheme(themeElement.GetString() == "dark");
                    break;
                case "ui-scale" when root.TryGetProperty("value", out var scaleElement):
                    Browser.ZoomFactor = Math.Clamp(scaleElement.GetDouble(), 1.0, 1.5);
                    break;
                case "host-status" when root.TryGetProperty("text", out var textElement):
                    var state = root.TryGetProperty("state", out var stateElement) ? stateElement.GetString() : "";
                    var color = state == "ready" ? "#2E9D67" : state is "recovering_at" or "registering" ? "#D68A20" : "#7A8782";
                    SetStatus(textElement.GetString() ?? "设备状态未知", color);
                    break;
                case "select-device" when root.TryGetProperty("key", out var keyElement):
                    SwitchDevice(keyElement.GetString(), root.TryGetProperty("view", out var viewElement) ? viewElement.GetString() : null);
                    break;
                case "rescan-devices":
                    _ = RescanDevicesAsync(showStatus: true);
                    break;
            }
        }
        catch (JsonException)
        {
            // Ignore malformed messages from the embedded UI.
        }
    }

    private string ActiveDeviceKey => _backends.FirstOrDefault(item => ReferenceEquals(item.Value, _activeBackend)).Key ?? "auto";

    private void PostDeviceList()
    {
        if (Browser.CoreWebView2 is null) return;
        var devices = _backends.Select(item => new
        {
            key = item.Key,
            label = item.Value.Device?.Label ?? "DJI 4G 模块",
            port = item.Value.Device?.Port ?? "自动识别",
            active = ReferenceEquals(item.Value, _activeBackend),
        // Never sort by the selected module: every switch reloads the page and
        // would swap the cards, making a successful switch look like a snap-back.
        }).OrderByDescending(item => string.Equals(item.key, _primaryDeviceKey, StringComparison.OrdinalIgnoreCase))
            .ThenBy(item => item.key, StringComparer.OrdinalIgnoreCase).ToArray();
        Browser.CoreWebView2.PostWebMessageAsJson(JsonSerializer.Serialize(new
        {
            type = "device-list",
            active = ActiveDeviceKey,
            devices,
        }));
    }

    private void SwitchDevice(string? key, string? view = null)
    {
        if (string.IsNullOrWhiteSpace(key) || !_backends.TryGetValue(key, out var backend) || backend.BaseUri is null) return;
        _activeBackend = backend;
        SetStatus($"正在切换到 {backend.Device?.Label ?? "DJI 4G 模块"}…", "#D68A20");
        var allowedViews = new[] { "overview", "sms", "calls", "retention", "remote", "esim", "network", "settings", "at" };
        var destination = new UriBuilder(backend.BaseUri);
        if (view is not null && allowedViews.Contains(view)) destination.Query = "view=" + Uri.EscapeDataString(view);
        Browser.CoreWebView2?.Navigate(destination.Uri.AbsoluteUri);
    }

    private async Task RescanDevicesAsync(bool showStatus)
    {
        if (!await _deviceScanGate.WaitAsync(0)) return;
        try
        {
            if (showStatus) SetStatus("正在扫描新插入的 DJI 4G 模块…", "#D68A20");
            var occupiedPorts = _backends.Values
                .Select(backend => backend.Device?.Port)
                .Where(port => !string.IsNullOrWhiteSpace(port))
                .Cast<string>()
                .ToArray();
            // Routine enumeration must not open any serial handle. Only newly
            // attached, unowned interfaces receive a bounded identity probe.
            var metadata = await BackendProcessService.DiscoverDevicesAsync(_shutdown.Token, probe: false);
            var newPorts = metadata.Select(device => device.Port).Except(occupiedPorts, StringComparer.OrdinalIgnoreCase).ToArray();
            var discovered = newPorts.Length == 0 ? Array.Empty<DeviceDescriptor>()
                : (await BackendProcessService.DiscoverDevicesAsync(_shutdown.Token,
                    metadata.Select(device => device.Port).Except(newPorts, StringComparer.OrdinalIgnoreCase)))
                    .Where(device => device.Available).ToArray();
            var root = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "DJ4Hub");
            if (discovered.Length > 0 && string.Equals(_primaryDeviceKey, "auto", StringComparison.OrdinalIgnoreCase))
            {
                _primaryDeviceKey = discovered[0].Key;
                PersistPrimaryDeviceKey(root, _primaryDeviceKey);
            }
            if (discovered.Length > 0 && _backends.TryGetValue("auto", out var fallback) && fallback.Device is null)
            {
                var fallbackWasActive = ReferenceEquals(_activeBackend, fallback);
                await fallback.StopAsync();
                _backends.Remove("auto");
                if (fallbackWasActive) _activeBackend = null;
            }
            var added = 0;
            foreach (var device in discovered)
            {
                if (_backends.Values.Any(existing => string.Equals(existing.Device?.Port, device.Port, StringComparison.OrdinalIgnoreCase))) continue;
                var key = device.Key;
                if (_backends.TryGetValue(key, out var known))
                {
                    if (!known.IsRunning && !_recovering.Contains(known))
                    {
                        known.UpdateDevice(device);
                        _ = RecoverBackendAsync(known, false);
                    }
                    continue;
                }
                var dataDirectory = string.Equals(device.Key, _primaryDeviceKey, StringComparison.OrdinalIgnoreCase)
                    ? root
                    : Path.Combine(root, "Devices", device.Key);
                SeedNotificationSettings(root, dataDirectory);
                var backend = new BackendProcessService(device, dataDirectory);
                backend.ExitedUnexpectedly += Backend_ExitedUnexpectedly;
                try
                {
                    await backend.StartAsync(_shutdown.Token);
                    _backends[key] = backend;
                    _activeBackend ??= backend;
                    added++;
                }
                catch
                {
                    await backend.StopAsync();
                    _backends[key] = backend;
                    _ = RecoverBackendAsync(backend, false);
                }
            }
            PostDeviceList();
            if (_activeBackend?.BaseUri is not null && Browser.Source?.Port != _activeBackend.BaseUri.Port)
            {
                Browser.CoreWebView2?.Navigate(_activeBackend.BaseUri.AbsoluteUri);
            }
            if (showStatus)
            {
                SetStatus(added > 0 ? $"已新增 {added} 块模块，现在可同时管理 {_backends.Count} 块" : $"扫描完成，当前 {_backends.Count} 块模块", "#2E9D67");
            }
        }
        catch (Exception ex)
        {
            if (showStatus) SetStatus("扫描失败：" + ex.Message, "#C84C4C");
        }
        finally
        {
            _deviceScanGate.Release();
        }
    }

    private void Backend_ExitedUnexpectedly(object? sender, BackendExitedEventArgs e)
    {
        if (sender is not BackendProcessService backend) return;
        Dispatcher.BeginInvoke(new Action(() => _ = RecoverBackendAsync(backend, e.ExitCode == 42)));
    }

    private async Task CheckDeviceHostsAsync()
    {
        if (_checkingHealth || _shutdown.IsCancellationRequested) return;
        _checkingHealth = true;
        try
        {
            var checks = _backends.Values.Where(backend => !_recovering.Contains(backend)).ToArray();
            var results = await Task.WhenAll(checks.Select(async backend =>
                (Backend: backend, Healthy: await backend.CheckATHealthAsync(_shutdown.Token))));
            foreach (var result in results)
            {
                if (_shutdown.IsCancellationRequested) return;
                if (result.Healthy) { _healthFailures[result.Backend] = 0; continue; }
                _healthFailures.TryGetValue(result.Backend, out var failures);
                _healthFailures[result.Backend] = ++failures;
                if (failures >= 3 || !result.Backend.IsRunning)
                {
                    _ = RecoverBackendAsync(result.Backend, false);
                }
            }
        }
        catch (OperationCanceledException) { }
        finally { _checkingHealth = false; }
    }

    private async Task RecoverBackendAsync(BackendProcessService backend, bool esimSwitch)
    {
        if (_shutdown.IsCancellationRequested || !_recovering.Add(backend)) return;
        try
        {
            var token = _shutdown.Token;
            await backend.StopAsync();
            for (var attempt = 0; !token.IsCancellationRequested; attempt++)
            {
                if (ReferenceEquals(backend, _activeBackend))
                {
                    SetStatus(esimSwitch ? "eSIM 已切换，正在重新连接模块…" : "通信暂不可用，正在自动重连（不会重发短信或重拨电话）…", "#D68A20");
                }
                await Task.Delay(TimeSpan.FromSeconds(attempt == 0 ? (esimSwitch ? 3 : 1) : Math.Min(30, 3 + attempt * 3)), token);
                try
                {
                    if (backend.Device is not null)
                    {
                        await _deviceScanGate.WaitAsync(token);
                        try
                        {
                            var occupied = _backends.Values.Where(other => other.IsRunning)
                                .Select(other => other.Device?.Port).OfType<string>();
                            var devices = await BackendProcessService.DiscoverDevicesAsync(token, occupied);
                            var device = devices.FirstOrDefault(device => device.Available && device.Key == backend.Device.Key);
                            if (device is null) continue; // Never bind by a stale COM number.
                            backend.UpdateDevice(device);
                        }
                        finally { _deviceScanGate.Release(); }
                    }
                    var localUri = await backend.StartAsync(token);
                    if (backend.Device is not null && !await backend.WaitForModemReadyAsync(TimeSpan.FromSeconds(30), token))
                    {
                        await backend.StopAsync();
                        continue;
                    }
                    if (esimSwitch) await backend.WaitForSubscriberReadyAsync(TimeSpan.FromSeconds(30), token);
                    _healthFailures[backend] = 0;
                    if (ReferenceEquals(backend, _activeBackend))
                    {
                        if (Browser.CoreWebView2 is not null) Browser.CoreWebView2.Navigate(localUri.AbsoluteUri);
                        else Browser.Source = localUri;
                        PostWindowState();
                        SetStatus("模块已自动恢复，正在同步最新 SIM 资料", "#2E9D67");
                    }
                    PostDeviceList();
                    return;
                }
                catch (OperationCanceledException) when (token.IsCancellationRequested) { break; }
                catch (Exception)
                {
                    await backend.StopAsync();
                    // Retry with bounded backoff until the device/port returns.
                }
            }
        }
        catch (OperationCanceledException) { }
        finally { _recovering.Remove(backend); }
    }

    private void MainWindow_Closing(object? sender, CancelEventArgs e)
    {
        if (_allowClose)
        {
            return;
        }
        if (App.IsSystemShutdown)
        {
            _allowClose = true;
            _shutdown.Cancel();
            _healthTimer.Stop();
            _deviceScanTimer.Stop();
            _trayIcon?.Dispose();
            _trayIcon = null;
            foreach (var backend in _backends.Values)
            {
                // Do not block the WPF dispatcher during Windows shutdown.
                // Parent watchers also release each child when this process ends.
                _ = backend.StopAsync();
            }
            Browser.Dispose();
            return;
        }
        e.Cancel = true;
        Hide();
        if (!_shownTrayHint)
        {
            _shownTrayHint = true;
            _trayIcon?.ShowBalloonTip(2500, "DJ4GHUB 仍在运行", "电脑端已缩小到托盘，云端连接不会中断。", System.Windows.Forms.ToolTipIcon.Info);
        }
    }

    private void CreateTrayIcon()
    {
        var menu = new System.Windows.Forms.ContextMenuStrip();
        menu.Items.Add("打开控制台", null, (_, _) => Dispatcher.BeginInvoke(ShowFromTray));
        menu.Items.Add("打开云端控制台", null, (_, _) => Dispatcher.BeginInvoke(async () => await OpenConfiguredCloudPanelAsync()));
        menu.Items.Add(new System.Windows.Forms.ToolStripSeparator());
        var startupItem = new System.Windows.Forms.ToolStripMenuItem("开机自动启动") { Checked = IsStartupEnabled(), CheckOnClick = false };
        startupItem.Click += (_, _) => Dispatcher.BeginInvoke(() =>
        {
            try
            {
                SetStartupEnabled(!IsStartupEnabled());
                startupItem.Checked = IsStartupEnabled();
            }
            catch (Exception ex)
            {
                System.Windows.MessageBox.Show(ex.Message, "DJ4GHUB 开机启动", MessageBoxButton.OK, MessageBoxImage.Error);
            }
        });
        menu.Items.Add(startupItem);
        menu.Items.Add(new System.Windows.Forms.ToolStripSeparator());
        menu.Items.Add("退出", null, (_, _) => Dispatcher.BeginInvoke(async () => await ShutdownAsync()));

        _trayIcon = new System.Windows.Forms.NotifyIcon
        {
            Text = "老李 DJ4GHUB 设备主机",
            ContextMenuStrip = menu,
            Visible = true,
        };
        try
        {
            _trayIcon.Icon = System.Drawing.Icon.ExtractAssociatedIcon(Environment.ProcessPath!);
        }
        catch
        {
            _trayIcon.Icon = System.Drawing.SystemIcons.Application;
        }
        _trayIcon.DoubleClick += (_, _) => Dispatcher.BeginInvoke(ShowFromTray);
    }

    private void ShowFromTray()
    {
        Show();
        if (WindowState == WindowState.Minimized)
        {
            WindowState = WindowState.Normal;
        }
        Activate();
        Topmost = true;
        Topmost = false;
        Focus();
    }

    private static string StartupCommand => $"\"{Environment.ProcessPath}\" --minimized";

    private static bool IsStartupEnabled()
    {
        using var key = Registry.CurrentUser.OpenSubKey(@"Software\Microsoft\Windows\CurrentVersion\Run", writable: false);
        return string.Equals(key?.GetValue("DJ4GHUB") as string, StartupCommand, StringComparison.OrdinalIgnoreCase);
    }

    private static void SetStartupEnabled(bool enabled)
    {
        using var key = Registry.CurrentUser.CreateSubKey(@"Software\Microsoft\Windows\CurrentVersion\Run", writable: true)
            ?? throw new InvalidOperationException("无法打开 Windows 开机启动设置。");
        if (enabled)
        {
            key.SetValue("DJ4GHUB", StartupCommand, RegistryValueKind.String);
        }
        else
        {
            key.DeleteValue("DJ4GHUB", throwOnMissingValue: false);
        }
    }

    private async Task ShutdownAsync()
    {
        if (_allowClose)
        {
            return;
        }
        _shutdown.Cancel();
        _healthTimer.Stop();
        _deviceScanTimer.Stop();
        SetStatus("正在安全关闭…", "#D68A20");
        try
        {
            await Task.WhenAll(_backends.Values.Select(backend => backend.StopAsync()));
        }
        finally
        {
            _allowClose = true;
            _trayIcon!.Visible = false;
            _trayIcon.Dispose();
            _trayIcon = null;
            Browser.Dispose();
            System.Windows.Application.Current.Shutdown();
        }
    }

    private void ShowStartupError(Exception ex)
    {
        Browser.Visibility = Visibility.Hidden;
        StartupPanel.Visibility = Visibility.Visible;
        StartupProgress.IsIndeterminate = false;
        StartupProgress.Value = 0;
        StartupTitle.Text = "DJ4GHUB 暂时无法启动";
        StartupDetail.Text = ex.Message + "\n\n如果提示 WebView2 不存在，请安装 Microsoft Edge WebView2 Runtime。";
        SetStatus("启动失败", "#C84C4C");
    }

    private void SetStatus(string text, string color)
    {
        if (Browser.CoreWebView2 is not null && Browser.Visibility == Visibility.Visible)
            Browser.CoreWebView2.PostWebMessageAsJson(JsonSerializer.Serialize(new { type = "host-state", text, color }));
    }
}
