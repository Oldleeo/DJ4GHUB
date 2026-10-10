using System.Collections.Concurrent;
using System.ComponentModel;
using System.Diagnostics;
using System.IO;
using System.Net;
using System.Net.Http;
using System.Net.Sockets;
using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace DJ4Hub.Windows.Services;

public sealed class BackendProcessService
{
    private readonly HttpClient _httpClient = new() { Timeout = TimeSpan.FromSeconds(5) };
    private readonly ConcurrentQueue<string> _recentOutput = new();
    private Process? _process;
    private bool _stopping;
    private volatile bool _ready;
    private readonly SemaphoreSlim _lifecycle = new(1, 1);
    private static readonly ConcurrentDictionary<int, byte> ReservedPorts = new();

    public BackendProcessService(DeviceDescriptor? device = null, string? dataDirectory = null)
    {
        Device = device;
        DataDirectory = dataDirectory;
    }

    public event EventHandler<BackendExitedEventArgs>? ExitedUnexpectedly;
    public Uri? BaseUri { get; private set; }
    public DeviceDescriptor? Device { get; private set; }
    public string? DataDirectory { get; }
    public bool IsRunning => _process is { HasExited: false };
    public int? ProcessId => IsRunning ? _process?.Id : null;

    public void UpdateDevice(DeviceDescriptor device)
    {
        if (IsRunning)
        {
            throw new InvalidOperationException("运行中的设备不能更换端口。");
        }
        Device = device;
    }

    public static async Task<IReadOnlyList<DeviceDescriptor>> DiscoverDevicesAsync(
        CancellationToken cancellationToken,
        IEnumerable<string>? excludedPorts = null, bool probe = true)
    {
        var startInfo = new ProcessStartInfo
        {
            FileName = FindBackendPath(),
            UseShellExecute = false,
            CreateNoWindow = true,
            RedirectStandardOutput = true,
            RedirectStandardError = true,
            StandardOutputEncoding = Encoding.UTF8,
            StandardErrorEncoding = Encoding.UTF8,
        };
        startInfo.ArgumentList.Add("-list-devices");
        startInfo.ArgumentList.Add("-parent-pid");
        startInfo.ArgumentList.Add(Environment.ProcessId.ToString());
        if (!probe) startInfo.ArgumentList.Add("-no-probe");
        var exclusions = excludedPorts?
            .Where(port => !string.IsNullOrWhiteSpace(port))
            .Select(port => port.Trim())
            .Distinct(StringComparer.OrdinalIgnoreCase)
            .ToArray() ?? [];
        if (exclusions.Length > 0)
        {
            startInfo.ArgumentList.Add("-skip-ports");
            startInfo.ArgumentList.Add(string.Join(',', exclusions));
        }
        using var process = new Process { StartInfo = startInfo };
        if (!process.Start())
        {
            throw new InvalidOperationException("无法启动设备扫描。");
        }
        var stdoutTask = process.StandardOutput.ReadToEndAsync();
        var stderrTask = process.StandardError.ReadToEndAsync();
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
        timeout.CancelAfter(TimeSpan.FromSeconds(12));
        try
        {
            await process.WaitForExitAsync(timeout.Token);
        }
        catch (OperationCanceledException)
        {
            try
            {
                if (!process.HasExited)
                {
                    process.Kill(entireProcessTree: true);
                    await process.WaitForExitAsync();
                }
            }
            catch (InvalidOperationException)
            {
                // The scan exited while cancellation was being handled.
            }
            if (cancellationToken.IsCancellationRequested)
            {
                throw;
            }
            throw new TimeoutException("设备扫描超过 12 秒，已安全终止扫描进程；运行中的模块不会被打断。");
        }
        var stdout = await stdoutTask;
        var stderr = await stderrTask;
        if (process.ExitCode != 0)
        {
            throw new InvalidOperationException(string.IsNullOrWhiteSpace(stderr) ? "扫描 DJI 4G 模块失败。" : stderr.Trim());
        }
        return JsonSerializer.Deserialize<List<DeviceDescriptor>>(stdout, new JsonSerializerOptions
        {
            PropertyNameCaseInsensitive = true,
        }) ?? [];
    }

    public async Task<Uri> StartAsync(CancellationToken cancellationToken)
    {
        await _lifecycle.WaitAsync(cancellationToken);
        try { return await StartCoreAsync(cancellationToken); }
        finally { _lifecycle.Release(); }
    }

    private async Task<Uri> StartCoreAsync(CancellationToken cancellationToken)
    {
        if (_process is not null)
        {
            if (!_process.HasExited) throw new InvalidOperationException("本地后台已经启动。");
            _process.Dispose();
            _process = null;
        }

        _stopping = false;
        var backendPath = FindBackendPath();
        var port = SelectLoopbackPort();
        try
        {
        BaseUri = new Uri($"http://127.0.0.1:{port}/", UriKind.Absolute);

        var dataDirectory = DataDirectory ?? Path.Combine(
            Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
            "DJ4Hub");
        Directory.CreateDirectory(dataDirectory);

        var startInfo = new ProcessStartInfo
        {
            FileName = backendPath,
            UseShellExecute = false,
            CreateNoWindow = true,
            RedirectStandardOutput = true,
            RedirectStandardError = true,
            StandardOutputEncoding = Encoding.UTF8,
            StandardErrorEncoding = Encoding.UTF8,
            WorkingDirectory = Path.GetDirectoryName(backendPath)!,
        };
        startInfo.ArgumentList.Add("-listen");
        startInfo.ArgumentList.Add($"127.0.0.1:{port}");
        startInfo.ArgumentList.Add("-data-dir");
        startInfo.ArgumentList.Add(dataDirectory);
        startInfo.ArgumentList.Add("-parent-pid");
        startInfo.ArgumentList.Add(Environment.ProcessId.ToString());
        if (Device is not null)
        {
            startInfo.ArgumentList.Add("-port");
            startInfo.ArgumentList.Add(Device.Port);
            startInfo.ArgumentList.Add("-device-key");
            startInfo.ArgumentList.Add(Device.Key);
            startInfo.ArgumentList.Add("-device-name");
            startInfo.ArgumentList.Add(Device.Label);
        }
        else startInfo.ArgumentList.Add("-idle-host");
        // The Go backend performs exact Windows discovery for the signed
        // Quectel VID_2CA3&PID_4006 AT interface. It refuses unrelated COM
        // ports and keeps listening only on loopback.

        _ready = false;
        _process = new Process { StartInfo = startInfo, EnableRaisingEvents = true };
        _process.OutputDataReceived += CaptureOutput;
        _process.ErrorDataReceived += CaptureOutput;
        _process.Exited += Process_Exited;
        if (!_process.Start())
        {
            _process.Dispose();
            _process = null;
            throw new InvalidOperationException("无法启动 DJ4GHUB 本地后台。");
        }
        _process.BeginOutputReadLine();
        _process.BeginErrorReadLine();

        await WaitForHealthAsync(cancellationToken);
        _ready = true;
        if (_process.HasExited) throw new InvalidOperationException("本地后台退出，等待重新连接。");
        return BaseUri;
        }
        catch
        {
            await StopCoreAsync();
            throw;
        }
        finally { ReservedPorts.TryRemove(port, out _); }
    }

    public async Task<bool> WaitForModemReadyAsync(TimeSpan duration, CancellationToken cancellationToken)
    {
        if (BaseUri is null)
        {
            return false;
        }
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
        timeout.CancelAfter(duration);
        var healthUri = new Uri(BaseUri, "api/health");
        while (!timeout.IsCancellationRequested)
        {
            try
            {
                using var response = await _httpClient.GetAsync(healthUri, timeout.Token);
                if (response.IsSuccessStatusCode)
                {
                    await using var stream = await response.Content.ReadAsStreamAsync(timeout.Token);
                    using var json = await JsonDocument.ParseAsync(stream, cancellationToken: timeout.Token);
                    if (json.RootElement.TryGetProperty("at_ready", out var ready) && ready.GetBoolean())
                    {
                        return true;
                    }
                }
            }
            catch (Exception ex) when (ex is HttpRequestException or TaskCanceledException or JsonException)
            {
                // The modem and its COM interface are still re-enumerating.
            }
            try
            {
                await Task.Delay(500, timeout.Token);
            }
            catch (TaskCanceledException)
            {
                break;
            }
        }
        return false;
    }

    public async Task<bool> WaitForSubscriberReadyAsync(TimeSpan duration, CancellationToken cancellationToken)
    {
        if (BaseUri is null)
        {
            return false;
        }
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
        timeout.CancelAfter(duration);
        var statusUri = new Uri(BaseUri, "api/status?refresh=1");
        while (!timeout.IsCancellationRequested)
        {
            try
            {
                using var response = await _httpClient.GetAsync(statusUri, timeout.Token);
                if (response.IsSuccessStatusCode)
                {
                    await using var stream = await response.Content.ReadAsStreamAsync(timeout.Token);
                    using var json = await JsonDocument.ParseAsync(stream, cancellationToken: timeout.Token);
                    var root = json.RootElement;
                    var simReady = root.TryGetProperty("sim_inserted", out var sim) && sim.GetBoolean();
                    var hasICCID = root.TryGetProperty("iccid", out var iccid) && !string.IsNullOrWhiteSpace(iccid.GetString());
                    if (simReady && hasICCID)
                    {
                        return true;
                    }
                }
            }
            catch (Exception ex) when (ex is HttpRequestException or TaskCanceledException or JsonException)
            {
                // The module may answer AT before the selected SIM identity is ready.
            }
            try
            {
                await Task.Delay(1000, timeout.Token);
            }
            catch (TaskCanceledException)
            {
                break;
            }
        }
        return false;
    }

    public async Task StopAsync()
    {
        await _lifecycle.WaitAsync();
        try { await StopCoreAsync(); }
        finally { _lifecycle.Release(); }
    }

    private async Task StopCoreAsync()
    {
        _stopping = true;
        _ready = false;
        var process = _process;
        _process = null;
        if (process is null)
        {
            return;
        }

        try
        {
            if (!process.HasExited)
            {
                process.Kill(entireProcessTree: true);
                await process.WaitForExitAsync().WaitAsync(TimeSpan.FromSeconds(5));
            }
        }
        catch (InvalidOperationException)
        {
            // It exited between HasExited and Kill.
        }
        catch (Win32Exception)
        {
            // The process exited or Windows already released its handle.
        }
        catch (TimeoutException)
        {
            if (!process.HasExited)
            {
                process.Kill(entireProcessTree: true);
            }
        }
        finally
        {
            process.Dispose();
        }
    }

    private async Task WaitForHealthAsync(CancellationToken cancellationToken)
    {
        if (BaseUri is null)
        {
            throw new InvalidOperationException("本地地址未初始化。");
        }

        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
        timeout.CancelAfter(TimeSpan.FromSeconds(30));
        var healthUri = new Uri(BaseUri, "api/health");
        Exception? lastError = null;
        while (!timeout.IsCancellationRequested)
        {
            if (_process?.HasExited == true)
            {
                throw new InvalidOperationException($"本地后台启动后立即退出。\n{GetDiagnostic()}");
            }
            try
            {
                using var response = await _httpClient.GetAsync(healthUri, timeout.Token);
                if (response.IsSuccessStatusCode)
                {
                    using var json = JsonDocument.Parse(await response.Content.ReadAsStringAsync(timeout.Token));
                    if (json.RootElement.TryGetProperty("device_key", out var key)
                        && key.GetString() == (Device?.Key ?? "local")) return;
                    lastError = new InvalidOperationException("本地端口返回了其他模块的服务；拒绝连接。");
                }
                lastError = new HttpRequestException($"HTTP {(int)response.StatusCode}");
            }
            catch (Exception ex) when (ex is HttpRequestException or TaskCanceledException)
            {
                lastError = ex;
            }
            await Task.Delay(200, timeout.Token);
        }
        throw new TimeoutException($"等待本地后台超时：{lastError?.Message}\n{GetDiagnostic()}");
    }

    private void Process_Exited(object? sender, EventArgs e)
    {
        if (_stopping || !_ready || sender is not Process process || !ReferenceEquals(process, _process))
        {
            return;
        }
        var exitCode = process.ExitCode;
        _ready = false;
        ExitedUnexpectedly?.Invoke(this, new BackendExitedEventArgs(exitCode, GetDiagnostic()));
    }

    public async Task<bool> CheckATHealthAsync(CancellationToken cancellationToken)
    {
        if (!IsRunning || BaseUri is null) return false;
        try
        {
            using var response = await _httpClient.GetAsync(new Uri(BaseUri, "api/health?probe=1"), cancellationToken);
            if (!response.IsSuccessStatusCode) return false;
            using var json = JsonDocument.Parse(await response.Content.ReadAsStringAsync(cancellationToken));
            return json.RootElement.TryGetProperty("device_key", out var key)
                && key.GetString() == (Device?.Key ?? "local")
                && (Device is null || json.RootElement.GetProperty("at_ready").GetBoolean());
        }
        catch (Exception ex) when (ex is HttpRequestException or TaskCanceledException or JsonException)
        { return false; }
    }

    private void CaptureOutput(object sender, DataReceivedEventArgs e)
    {
        if (string.IsNullOrWhiteSpace(e.Data))
        {
            return;
        }
        _recentOutput.Enqueue(e.Data);
        while (_recentOutput.Count > 30)
        {
            _recentOutput.TryDequeue(out _);
        }
    }

    private string GetDiagnostic() => string.Join(Environment.NewLine, _recentOutput);

    private static string FindBackendPath()
    {
        var candidates = new[]
        {
            Environment.GetEnvironmentVariable("DJ4HUB_BACKEND_PATH"),
            Path.Combine(AppContext.BaseDirectory, "backend", "dj4ghub-service.exe"),
            Path.Combine(AppContext.BaseDirectory, "dj4ghub-service.exe"),
        };
        foreach (var candidate in candidates)
        {
            if (!string.IsNullOrWhiteSpace(candidate) && File.Exists(candidate))
            {
                return Path.GetFullPath(candidate);
            }
        }
        throw new FileNotFoundException("找不到 dj4ghub-service.exe，请重新解压完整发行包。");
    }

    private static int SelectLoopbackPort()
    {
        if (CanBind(7575, out _) && ReservedPorts.TryAdd(7575, 0))
        {
            return 7575;
        }
        for (var attempt = 0; attempt < 20; attempt++)
        {
            if (CanBind(0, out var port) && ReservedPorts.TryAdd(port, 0)) return port;
        }
        throw new SocketException((int)SocketError.AddressNotAvailable);
    }

    private static bool CanBind(int requestedPort, out int selectedPort)
    {
        selectedPort = 0;
        TcpListener? listener = null;
        try
        {
            listener = new TcpListener(IPAddress.Loopback, requestedPort);
            listener.Start();
            selectedPort = ((IPEndPoint)listener.LocalEndpoint).Port;
            return true;
        }
        catch (SocketException)
        {
            return false;
        }
        finally
        {
            listener?.Stop();
        }
    }
}

public sealed record DeviceDescriptor(
    [property: JsonPropertyName("key")] string Key,
    [property: JsonPropertyName("port")] string Port,
    [property: JsonPropertyName("label")] string Label,
    [property: JsonPropertyName("suffix")] string? Suffix,
    [property: JsonPropertyName("available")] bool Available,
    [property: JsonPropertyName("error")] string? Error);
