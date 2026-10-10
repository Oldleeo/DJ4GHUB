using System.Runtime.InteropServices;
using System.Windows;
using System.Windows.Automation;
using System.Windows.Interop;
using System.Text.Json;
using System.Windows.Media;
using Brush = System.Windows.Media.Brush;

namespace DJ4Hub.Windows;

public partial class MainWindow
{
    protected override void OnSourceInitialized(EventArgs e)
    {
        base.OnSourceInitialized(e);
        var handle = new WindowInteropHelper(this).Handle;
        HwndSource.FromHwnd(handle)?.AddHook(ChromeWindowHook);
        UpdateChromeAppearance();
        PostWindowState();
    }

    protected override void OnStateChanged(EventArgs e)
    {
        base.OnStateChanged(e);
        if (ChromeMaximizeButton is not null)
        {
            var label = WindowState == WindowState.Maximized ? "还原窗口" : "最大化";
            ChromeMaximizeButton.ToolTip = label;
            AutomationProperties.SetName(ChromeMaximizeButton, label);
        }
        UpdateChromeAppearance();
        PostWindowState();
    }

    private void ChromeMinimize_Click(object sender, RoutedEventArgs e) => SystemCommands.MinimizeWindow(this);

    private void ChromeMaximize_Click(object sender, RoutedEventArgs e)
    {
        if (WindowState == WindowState.Maximized) SystemCommands.RestoreWindow(this);
        else SystemCommands.MaximizeWindow(this);
    }

    private void ChromeClose_Click(object sender, RoutedEventArgs e) => Close();

    private void PostWindowState()
    {
        if (Browser?.CoreWebView2 is not null)
            Browser.CoreWebView2.PostWebMessageAsJson(JsonSerializer.Serialize(new
            {
                type = "window-state", maximized = WindowState == WindowState.Maximized,
            }));
    }

    private void ApplyWindowTheme(bool dark)
    {
        // Only visible during startup/navigation; loaded content paints the
        // entire window and owns its palette, including caption buttons.
        var color = dark ? "#111C2D" : "#F0F3F8";
        Background = (Brush)new BrushConverter().ConvertFromString(color)!;
        StartupTitle.Foreground = (Brush)new BrushConverter().ConvertFromString(dark ? "#E7EDF7" : "#202B3B")!;
        StartupDetail.Foreground = (Brush)new BrushConverter().ConvertFromString(dark ? "#A4B1C5" : "#697789")!;
        Browser.DefaultBackgroundColor = dark
            ? System.Drawing.Color.FromArgb(255, 17, 28, 45)
            : System.Drawing.Color.FromArgb(255, 240, 243, 248);
    }

    private IntPtr ChromeWindowHook(IntPtr window, int message, IntPtr wParam, IntPtr lParam, ref bool handled)
    {
        if (message != 0x0024 || lParam == IntPtr.Zero) return IntPtr.Zero; // WM_GETMINMAXINFO
        var monitor = new MonitorBounds { Size = Marshal.SizeOf<MonitorBounds>() };
        if (!GetMonitorInfo(MonitorFromWindow(window, 2), ref monitor)) return IntPtr.Zero;
        var limits = Marshal.PtrToStructure<MinMaxBounds>(lParam);
        // Relative to the target monitor (including negative-coordinate monitors),
        // not PrimaryScreen/SystemParameters. This also respects side/top taskbars.
        limits.MaxPosition.X = monitor.Work.Left - monitor.Monitor.Left;
        limits.MaxPosition.Y = monitor.Work.Top - monitor.Monitor.Top;
        limits.MaxSize.X = monitor.Work.Right - monitor.Work.Left;
        limits.MaxSize.Y = monitor.Work.Bottom - monitor.Work.Top;
        var scale = System.Windows.Media.VisualTreeHelper.GetDpi(this);
        limits.MinTrackSize.X = (int)Math.Ceiling(MinWidth * scale.DpiScaleX);
        limits.MinTrackSize.Y = (int)Math.Ceiling(MinHeight * scale.DpiScaleY);
        Marshal.StructureToPtr(limits, lParam, false);
        handled = true;
        return IntPtr.Zero;
    }

    private void UpdateChromeAppearance()
    {
        if (!OperatingSystem.IsWindowsVersionAtLeast(10, 0, 22000)) return;
        var handle = new WindowInteropHelper(this).Handle;
        if (handle == IntPtr.Zero) return;
        // DWMWA_WINDOW_CORNER_PREFERENCE and DWMWA_BORDER_COLOR, Windows 11+.
        // Unsupported attributes simply return a failing HRESULT: never affect device hosts.
        var corners = WindowState == WindowState.Maximized ? 1u : 2u;
        var noBorder = 0xFFFFFFFEu;
        _ = DwmSetWindowAttribute(handle, 33, ref corners, sizeof(uint));
        _ = DwmSetWindowAttribute(handle, 34, ref noBorder, sizeof(uint));
    }

    [DllImport("dwmapi.dll")]
    private static extern int DwmSetWindowAttribute(IntPtr window, uint attribute, ref uint value, uint size);

    [StructLayout(LayoutKind.Sequential)]
    private struct NativePoint { public int X, Y; }
    [StructLayout(LayoutKind.Sequential)]
    private struct NativeRect { public int Left, Top, Right, Bottom; }
    [StructLayout(LayoutKind.Sequential)]
    private struct MonitorBounds { public int Size; public NativeRect Monitor, Work; public uint Flags; }
    [StructLayout(LayoutKind.Sequential)]
    private struct MinMaxBounds { public NativePoint Reserved, MaxSize, MaxPosition, MinTrackSize, MaxTrackSize; }
    [DllImport("user32.dll")]
    private static extern IntPtr MonitorFromWindow(IntPtr window, uint flags);
    [DllImport("user32.dll", EntryPoint = "GetMonitorInfoW")]
    [return: MarshalAs(UnmanagedType.Bool)]
    private static extern bool GetMonitorInfo(IntPtr monitor, ref MonitorBounds bounds);
}
