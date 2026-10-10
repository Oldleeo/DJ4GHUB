using System.Diagnostics;
using System.IO;
using System.Net;
using System.Net.Http;
using System.Net.Sockets;
using System.Reflection;
using System.Runtime.InteropServices;
using System.Text.Json;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Interop;
using System.Windows.Shell;
using DJ4Hub.Windows;
using DJ4Hub.Windows.Services;
using Microsoft.Web.WebView2.Core;
using Microsoft.Web.WebView2.Wpf;

// Exercise the production WPF/WebView2 message bridge, not a JS-only mock.
// Demo hosts only: no discovery, serial handles, real identities or data directories.
internal static class Program
{
    private const BindingFlags Private = BindingFlags.Instance | BindingFlags.NonPublic;
    private static readonly List<Process> Hosts = [];
    private static MainWindow? Window;
    private static string Root = "";

    [STAThread]
    private static int Main(string[] args)
    {
        var backend = Path.GetFullPath(args.Last());
        Root = Path.GetFullPath(Path.Combine(AppContext.BaseDirectory, "qa-" + Guid.NewGuid().ToString("N")));
        Directory.CreateDirectory(Root);
        var application = new Application { ShutdownMode = ShutdownMode.OnExplicitShutdown };
        // Only the required XAML brush; do not invoke App.OnStartup or its real-instance mutex.
        application.Resources["BrandBrush"] = new System.Windows.Media.SolidColorBrush(System.Windows.Media.Color.FromRgb(23,107,91));
        var result = 1;
        application.Dispatcher.InvokeAsync(async () =>
        {
            try { await Run(backend); result = 0; }
            catch (Exception error) { Console.Error.WriteLine(error); }
            finally
            {
                if (Window is not null)
                {
                    Field("_allowClose").SetValue(Window, true);
                    Window.Close();
                }
                foreach (var host in Hosts)
                {
                    try { if (!host.HasExited) host.Kill(); host.WaitForExit(5000); }
                    finally { host.Dispose(); }
                }
                application.Shutdown();
            }
        });
        application.Run();
        return result;
    }

    private static FieldInfo Field(string name) => typeof(MainWindow).GetField(name, Private)!;
    private static void Invoke(string method) => typeof(MainWindow).GetMethod(method, Private)!.Invoke(Window, null);
    private static void Check(bool condition, string text)
    {
        if (!condition) throw new InvalidOperationException("FAIL " + text);
        Console.WriteLine("PASS " + text);
    }

    private static async Task<Uri> StartDemo(string backend, string name)
    {
        using var listener = new TcpListener(IPAddress.Loopback, 0);
        listener.Start();
        var port = ((IPEndPoint)listener.LocalEndpoint).Port;
        listener.Stop();
        var start = new ProcessStartInfo(backend) { UseShellExecute = false, CreateNoWindow = true };
        foreach (var arg in new[] { "-demo", "-listen", $"127.0.0.1:{port}", "-data-dir", Path.Combine(Root, name) }) start.ArgumentList.Add(arg);
        var host = Process.Start(start)!;
        Hosts.Add(host);
        var uri = new Uri($"http://127.0.0.1:{port}/");
        using var http = new HttpClient { Timeout = TimeSpan.FromSeconds(1) };
        for (var attempt = 0; attempt < 60; attempt++)
        {
            if (host.HasExited) throw new Exception("Demo exited");
            try { if ((await http.GetAsync(new Uri(uri, "api/health"))).IsSuccessStatusCode) return uri; }
            catch (HttpRequestException) { }
            catch (TaskCanceledException) { }
            await Task.Delay(100);
        }
        throw new TimeoutException("Demo did not start");
    }

    private static async Task Run(string backend)
    {
        var first = await StartDemo(backend, "first");
        var second = await StartDemo(backend, "second");
        Window = new MainWindow { ShowInTaskbar = false, Opacity = 0 };
        // Never run the real startup discovery. Install isolated services before loading.
        var loaded = typeof(MainWindow).GetMethod("MainWindow_Loaded", Private)!;
        Window.Loaded -= (RoutedEventHandler)Delegate.CreateDelegate(typeof(RoutedEventHandler), Window, loaded);
        (Field("_trayIcon").GetValue(Window) as IDisposable)?.Dispose();
        Field("_trayIcon").SetValue(Window, null);
        var hosts = (Dictionary<string, BackendProcessService>)Field("_backends").GetValue(Window)!;
        foreach (var item in new[] { (Key: "module-a", Uri: first), (Key: "module-b", Uri: second) })
        {
            var descriptor = new DeviceDescriptor(item.Key, item.Key, "DJI 4G 模块 · " + (item.Key == "module-a" ? "1111" : "2222"), "", true, null);
            var service = new BackendProcessService(descriptor, Path.Combine(Root, item.Key));
            typeof(BackendProcessService).GetProperty("BaseUri")!.SetValue(service, item.Uri);
            hosts.Add(item.Key, service);
        }
        Field("_activeBackend").SetValue(Window, hosts["module-a"]);
        Window.Show();
        var browser = (WebView2)Window.FindName("Browser");
        var environment = await CoreWebView2Environment.CreateAsync(userDataFolder: Path.Combine(Root, "WebView2"));
        await browser.EnsureCoreWebView2Async(environment);
        var core = browser.CoreWebView2;
        core.AddWebResourceRequestedFilter("*", CoreWebView2WebResourceContext.All);
        core.WebResourceRequested += (_, e) =>
        {
            if (!Uri.TryCreate(e.Request.Uri, UriKind.Absolute, out var uri) || !uri.IsLoopback || e.Request.Method != "GET")
                e.Response = core.Environment.CreateWebResourceResponse(null, 403, "QA blocks external access and mutations", "");
        };
        Invoke("ConfigureBrowser");
        core.Navigate(first.AbsoluteUri);
        await Ready(core, first.Port, "module-a");
        var baseline = await core.ExecuteScriptAsync("[...document.querySelectorAll('.workspace-module')].map(n=>n.dataset.deviceKey)");
        Check(JsonSerializer.Deserialize<string[]>(baseline)!.Length == 2, "both demo modules rendered by production bridge");
        for (var round = 0; round < 3; round++)
        {
            foreach (var item in new[] { (Key: "module-b", Uri: second), (Key: "module-a", Uri: first) })
            {
                await core.ExecuteScriptAsync($"document.querySelector('.workspace-module[data-device-key=\"{item.Key}\"]').click()");
                await Ready(core, item.Uri.Port, item.Key);
                Check(ReferenceEquals(Field("_activeBackend").GetValue(Window), hosts[item.Key]), "native selected backend follows click " + item.Key);
                var order = await core.ExecuteScriptAsync("[...document.querySelectorAll('.workspace-module')].map(n=>n.dataset.deviceKey)");
                Check(order == baseline, "card positions remain fixed after selecting " + item.Key);
                for (var poll = 0; poll < 3; poll++) Invoke("PostDeviceList");
                await Task.Delay(150);
                await Ready(core, item.Uri.Port, item.Key);
            }
        }
        await core.ExecuteScriptAsync("document.querySelector('[data-view=\"sms\"]').click()");
        await core.ExecuteScriptAsync("document.querySelector('.workspace-module[data-device-key=\"module-b\"]').click()");
        await Ready(core, second.Port, "module-b");
        Check(await core.ExecuteScriptAsync("activePageView") == "\"sms\"", "module switch retains current page");
        var reloaded = new TaskCompletionSource<bool>();
        void OnReload(object? sender, CoreWebView2NavigationCompletedEventArgs e)
        {
            if (e.IsSuccess) reloaded.TrySetResult(true);
        }
        core.NavigationCompleted += OnReload;
        core.Reload();
        try { await reloaded.Task.WaitAsync(TimeSpan.FromSeconds(15)); }
        finally { core.NavigationCompleted -= OnReload; }
        await Ready(core, second.Port, "module-b");
        Check(await core.ExecuteScriptAsync("activePageView") == "\"sms\"", "reload retains selected module and page");
        await core.ExecuteScriptAsync("['module-a','module-b','module-a'].forEach(key=>window.chrome.webview.postMessage({type:'select-device',key,view:'sms'}))");
        await Ready(core, first.Port, "module-a");
        await Task.Delay(500);
        Check(new Uri(core.Source).Port == first.Port && ReferenceEquals(Field("_activeBackend").GetValue(Window), hosts["module-a"]), "rapid A/B/A selection settles on the last module");
        Check(((FrameworkElement)Window.FindName("StartupPanel")).Visibility == Visibility.Collapsed, "cancelled stale navigation does not show a startup error");
        Check(await core.ExecuteScriptAsync("[...document.querySelectorAll('.workspace-module')].map(n=>n.dataset.deviceKey)") == baseline, "rapid selection preserves card positions");
        await CheckChrome(core, first.Port);
        await core.ExecuteScriptAsync("loadStatus()");await Task.Delay(200);
        Check(await core.ExecuteScriptAsync("document.querySelector('#header-device-state span')?.textContent.includes('模块正常')") == "true", "native status messages preserve markup needed by subsequent health polling");
        await core.ExecuteScriptAsync("document.querySelector('#header-device-state span').textContent='测试长状态文字，设备正在同步资料。'.repeat(12)");
        Window.Width=Window.MinWidth;Window.UpdateLayout();await Task.Delay(150);
        Check(await core.ExecuteScriptAsync("document.documentElement.scrollWidth<=innerWidth+1 && (()=>{const a=document.getElementById('header-device-state').getBoundingClientRect(),b=document.getElementById('refresh').getBoundingClientRect();return a.right<=b.left||a.bottom<=b.top})()") == "true", "long in-content status cannot overlap refresh control");
        Window.Width=1280;Window.UpdateLayout();
        await CheckScrolling(core);
        Console.WriteLine("Device bridge regression passed; no real module commands issued.");
    }

    private static async Task CheckChrome(CoreWebView2 core, int port)
    {
        Check(Window!.WindowStyle == WindowStyle.None && !Window.AllowsTransparency, "native title bar removed without layered-window rendering");
        var chrome = WindowChrome.GetWindowChrome(Window);
        Check(chrome is not null && chrome.CaptionHeight == 40 && chrome.ResizeBorderThickness.Left >= 6 && !chrome.UseAeroCaptionButtons, "custom caption keeps native drag and resize hit zones");
        var handle = new WindowInteropHelper(Window).Handle;
        Check(core.Settings.IsNonClientRegionSupportEnabled, "WebView2 native app-region support enabled before navigation");
        Check(((FrameworkElement)Window.FindName("ChromeTitleBar")).Visibility==Visibility.Collapsed, "separate native caption is hidden after content loads");
        Check(Window.FindName("ReloadButton") is null && Window.FindName("StatusText") is null, "separate native footer removed");
        var browser=(WebView2)Window.FindName("Browser");
        Window.UpdateLayout();
        Check(Math.Abs(browser.ActualHeight-Window.ActualHeight)<2, "WebView paints the full window including former top and bottom bands");
        Check(await core.ExecuteScriptAsync("document.querySelectorAll('.desktop-window-chrome button').length===3 && getComputedStyle(document.querySelector('.desktop-window-chrome')).backgroundColor==='rgba(0, 0, 0, 0)' && getComputedStyle(document.querySelector('.desktop-window-drag')).getPropertyValue('app-region')==='drag' && getComputedStyle(document.querySelector('[data-window-action=maximize]')).getPropertyValue('app-region')==='no-drag'") == "true", "transparent caption preserves draggable region and interactive window controls");
        var captionPoint = browser.PointToScreen(new Point(browser.ActualWidth / 2, 18));
        Check(Hit(handle, captionPoint) == 2, "drag strip hits HTCAPTION");
        var resizePoint = Window.PointToScreen(new Point(2, Window.ActualHeight / 2));
        Check(Hit(handle, resizePoint) == 10, "left edge hits HTLEFT for native resizing");
        async Task Click(string action)
        {
            var raw=await core.ExecuteScriptAsync($"(()=>{{const r=document.querySelector('[data-window-action={action}]').getBoundingClientRect();return {{x:r.x+r.width/2,y:r.y+r.height/2}}}})()");
            using var point=JsonDocument.Parse(raw);
            var x=point.RootElement.GetProperty("x").GetDouble();var y=point.RootElement.GetProperty("y").GetDouble();
            foreach(var kind in new[]{"mousePressed","mouseReleased"})
                await core.CallDevToolsProtocolMethodAsync("Input.dispatchMouseEvent",JsonSerializer.Serialize(new { type=kind,x,y,button="left",clickCount=1 }));
            await Task.Delay(200);
        }
        await Click("minimize");
        Check(Window.WindowState == WindowState.Minimized, "themed HTML minimize button reaches native window");
        SystemCommands.RestoreWindow(Window); await Task.Delay(150);
        await Click("maximize");
        Check(Window.WindowState == WindowState.Maximized, "themed HTML maximize button reaches native window");
        Check(await core.ExecuteScriptAsync("document.querySelector('[data-window-action=maximize]').getAttribute('aria-label')") == "\"还原窗口\"", "caption accessibility label follows native window state");
        var monitor = new MonitorInfo { Size = Marshal.SizeOf<MonitorInfo>() };
        Check(GetMonitorInfo(MonitorFromWindow(handle, 2), ref monitor), "monitor work area available");
        Window.UpdateLayout();
        var contentTop = browser.PointToScreen(new Point(0,0));
        var contentBottom = browser.PointToScreen(new Point(browser.ActualWidth,browser.ActualHeight));
        Check(contentTop.Y >= monitor.Work.Top - 1 && contentBottom.Y <= monitor.Work.Bottom + 1 && contentBottom.X <= monitor.Work.Right + 1, "maximized full-window content stays inside monitor work area");
        await Click("maximize");
        Check(Window.WindowState == WindowState.Normal, "themed HTML restore button works");
        SendMessage(handle, 0x00A3, new IntPtr(2), Pack(captionPoint)); await Task.Delay(150);
        Check(Window.WindowState == WindowState.Maximized, "caption double-click maximizes");
        SystemCommands.RestoreWindow(Window); await Task.Delay(150);
        foreach(var theme in new[]{"dark","light","dark"})
        {
            await core.ExecuteScriptAsync($"setThemePreference('{theme}')");await Task.Delay(250);
            Check(BrowserTheme(theme,browser) && await core.ExecuteScriptAsync("getComputedStyle(document.querySelector('.desktop-window-chrome')).backgroundColor==='rgba(0, 0, 0, 0)' && getComputedStyle(document.querySelector('.workspace-status-footer')).backgroundColor==='rgba(0, 0, 0, 0)' && getComputedStyle(document.querySelector('.workspace-status-footer')).position!=='fixed'")=="true", theme+" theme covers caption and in-content footer without separate color bands");
        }
        Field("_shownTrayHint").SetValue(Window, true);
        await Click("close");
        Check(!Window.IsVisible, "custom close keeps close-to-tray semantics");
        Check(new Uri(core.Source).Port == port && ReferenceEquals(Field("_activeBackend").GetValue(Window), ((Dictionary<string, BackendProcessService>)Field("_backends").GetValue(Window)!)["module-a"]), "window controls do not switch or restart selected module");
        Window.Show();
        await Ready(core, port, "module-a");
    }

    private static bool BrowserTheme(string theme, WebView2 browser) => browser.DefaultBackgroundColor == (theme=="dark" ? System.Drawing.Color.FromArgb(255,17,28,45) : System.Drawing.Color.FromArgb(255,240,243,248));

    private static IntPtr Pack(Point point) => new(unchecked((int)(((uint)(ushort)(int)point.Y << 16) | (ushort)(int)point.X)));
    private static async Task CheckScrolling(CoreWebView2 core)
    {
        Check(await core.ExecuteScriptAsync("document.body.classList.contains('is-desktop-host') && getComputedStyle(document.documentElement).scrollbarWidth === 'none' && getComputedStyle(document.documentElement, '::-webkit-scrollbar').display === 'none'") == "true", "Windows page scrollbar is hidden");
        Check(await core.ExecuteScriptAsync("[...document.querySelectorAll('.task-nav, textarea, .workspace-chat-stream')].every(n=>getComputedStyle(n).scrollbarWidth==='none')") == "true", "nested navigation, message and input scrollbars are hidden");
        // Temporary DOM only in the isolated QA page, never real messages or devices.
        await core.ExecuteScriptAsync("window.__qaSpacer=document.createElement('div');__qaSpacer.style.height='1800px';document.body.append(__qaSpacer);scrollTo(0,0);window.__qaScrollBox=document.createElement('div');__qaScrollBox.style.cssText='position:fixed;left:240px;top:160px;width:300px;height:120px;overflow:auto;z-index:99999;background:white';__qaScrollBox.innerHTML='<div style=\"height:1000px\">scroll QA</div>';document.body.append(__qaScrollBox)");
        await core.CallDevToolsProtocolMethodAsync("Input.dispatchMouseEvent", "{\"type\":\"mouseWheel\",\"x\":700,\"y\":400,\"deltaX\":0,\"deltaY\":600}");
        await Task.Delay(300);
        Check(await core.ExecuteScriptAsync("document.scrollingElement.scrollTop > 0") == "true", "mouse wheel scrolls page with hidden scrollbar");
        await core.CallDevToolsProtocolMethodAsync("Input.dispatchMouseEvent", "{\"type\":\"mouseWheel\",\"x\":300,\"y\":200,\"deltaX\":0,\"deltaY\":200}");
        await Task.Delay(300);
        Check(await core.ExecuteScriptAsync("__qaScrollBox.scrollTop > 0 && getComputedStyle(__qaScrollBox).scrollbarWidth === 'none'") == "true", "nested containers retain wheel scrolling");
        await core.ExecuteScriptAsync("scrollTo(0,document.scrollingElement.scrollHeight)");
        Check(await core.ExecuteScriptAsync("document.scrollingElement.scrollTop > 0 && document.scrollingElement.scrollTop + innerHeight >= document.scrollingElement.scrollHeight - 2") == "true", "bottom content remains reachable");
        await core.ExecuteScriptAsync("__qaSpacer.remove();__qaScrollBox.remove();scrollTo(0,0)");
    }
    private static int Hit(IntPtr window, Point point) => (int)SendMessage(window, 0x0084, IntPtr.Zero, Pack(point));
    [StructLayout(LayoutKind.Sequential)]
    private struct Rect { public int Left, Top, Right, Bottom; }
    [StructLayout(LayoutKind.Sequential)]
    private struct MonitorInfo { public int Size; public Rect Monitor, Work; public uint Flags; }
    [DllImport("user32.dll")]
    private static extern IntPtr SendMessage(IntPtr window, uint message, IntPtr wParam, IntPtr lParam);
    [DllImport("user32.dll")]
    private static extern IntPtr MonitorFromWindow(IntPtr window, uint flags);
    [DllImport("user32.dll", EntryPoint = "GetMonitorInfoW")]
    [return: MarshalAs(UnmanagedType.Bool)]
    private static extern bool GetMonitorInfo(IntPtr monitor, ref MonitorInfo info);

    private static async Task Ready(CoreWebView2 core, int port, string key)
    {
        for (var attempt = 0; attempt < 150; attempt++)
        {
            try
            {
                var script = $"location.port === '{port}' && document.querySelector('.workspace-module[aria-pressed=\"true\"]')?.dataset.deviceKey === '{key}' && document.getElementById('page-title')?.textContent.length > 0";
                if (await core.ExecuteScriptAsync(script) == "true") return;
            }
            catch (InvalidOperationException) { }
            await Task.Delay(100);
        }
        throw new TimeoutException("Module navigation did not settle: " + key);
    }
}
