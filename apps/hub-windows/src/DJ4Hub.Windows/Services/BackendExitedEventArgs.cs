namespace DJ4Hub.Windows.Services;

public sealed class BackendExitedEventArgs(int exitCode, string diagnostic) : EventArgs
{
    public int ExitCode { get; } = exitCode;
    public string Diagnostic { get; } = diagnostic;
}
