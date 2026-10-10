using System.Diagnostics;
using DJ4Hub.Windows.Services;

var backendPath = args.Last();
Environment.SetEnvironmentVariable("DJ4HUB_BACKEND_PATH", Path.GetFullPath(backendPath));
var root = Path.Combine(Path.GetTempPath(), "DJ4Hub-supervision-" + Guid.NewGuid().ToString("N"));
if (args[0] == "--parent-crash")
{
    var childHost = new BackendProcessService(dataDirectory: root);
    await childHost.StartAsync(CancellationToken.None);
    Console.WriteLine(childHost.ProcessId);
    Console.Out.Flush();
    Console.ReadLine();
    await childHost.StopAsync();
    return;
}

void Check(bool condition, string message)
{
    if (!condition) throw new Exception(message);
    Console.WriteLine("PASS " + message);
}
var first = new BackendProcessService(dataDirectory: Path.Combine(root,"first"));
var second = new BackendProcessService(dataDirectory: Path.Combine(root,"second"));
try
{
    await Task.WhenAll(first.StartAsync(CancellationToken.None), second.StartAsync(CancellationToken.None));
    Check(first.BaseUri!.Port != second.BaseUri!.Port, "parallel hosts reserve distinct loopback ports");
    Check(await first.CheckATHealthAsync(CancellationToken.None) && await second.CheckATHealthAsync(CancellationToken.None), "both isolated idle hosts healthy");
    var exited = new TaskCompletionSource<bool>(TaskCreationOptions.RunContinuationsAsynchronously);
    first.ExitedUnexpectedly += (_,_) => exited.TrySetResult(true);
    using var victim = Process.GetProcessById(first.ProcessId!.Value);
    victim.Kill();
    await exited.Task.WaitAsync(TimeSpan.FromSeconds(5));
    await first.StartAsync(CancellationToken.None);
    Check(await first.CheckATHealthAsync(CancellationToken.None), "unexpected exit can restart same host object");
    Check(await second.CheckATHealthAsync(CancellationToken.None), "peer remains healthy during recovery");
    await first.StopAsync();
    Check(!first.IsRunning, "explicit stop leaves no child process");
    await first.StartAsync(CancellationToken.None);
    Check(first.IsRunning, "stop then restart lifecycle works");

    var info = new ProcessStartInfo(Environment.ProcessPath!) {
        UseShellExecute=false, CreateNoWindow=true, RedirectStandardOutput=true, RedirectStandardInput=true
    };
    info.ArgumentList.Add("--parent-crash"); info.ArgumentList.Add(backendPath);
    using var parent = Process.Start(info)!;
    var line = await parent.StandardOutput.ReadLineAsync().WaitAsync(TimeSpan.FromSeconds(10));
    using var ownedHost = Process.GetProcessById(int.Parse(line!));
    parent.Kill(); await parent.WaitForExitAsync();
    await ownedHost.WaitForExitAsync().WaitAsync(TimeSpan.FromSeconds(8));
    Check(ownedHost.HasExited, "parent crash releases orphan backend automatically");
}
finally { await first.StopAsync(); await second.StopAsync(); }
