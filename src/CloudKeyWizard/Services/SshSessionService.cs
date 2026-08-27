using System.IO;
using System.Net.Sockets;
using System.Text;
using CloudKeyWizard.Models;
using Renci.SshNet;

namespace CloudKeyWizard.Services;

/// <summary>
/// Owns the one SSH (+ SFTP) session to the Cloud Key. Every command the wizard runs, and every
/// byte typed/received in the manual "drop to shell" pane, flows through here so the terminal log
/// is a complete, honest record of everything the app did -- nothing happens off-screen.
/// </summary>
public sealed class SshSessionService : IDisposable
{
    public const string RemoteWorkDir = "/root/.cloudkey-wizard";

    private SshClient? _client;
    private SftpClient? _sftp;

    public event Action<TerminalLine>? OutputReceived;

    public bool IsConnected => _client?.IsConnected == true;

    public async Task ConnectAsync(ConnectionProfile profile, CancellationToken ct = default)
    {
        var authMethods = new List<AuthenticationMethod>();

        if (!string.IsNullOrWhiteSpace(profile.PrivateKeyPath))
        {
            var keyFile = string.IsNullOrEmpty(profile.PrivateKeyPassphrase)
                ? new PrivateKeyFile(profile.PrivateKeyPath)
                : new PrivateKeyFile(profile.PrivateKeyPath, profile.PrivateKeyPassphrase);
            authMethods.Add(new PrivateKeyAuthenticationMethod(profile.Username, keyFile));
        }

        if (!string.IsNullOrEmpty(profile.Password))
        {
            authMethods.Add(new PasswordAuthenticationMethod(profile.Username, profile.Password));

            // Some UniFi OS / Debian PAM sshd configs don't advertise plain "password" as an
            // accepted method at all -- only "publickey" and "keyboard-interactive" -- and deliver
            // the password prompt through keyboard-interactive instead. PuTTY/OpenSSH clients paper
            // over this automatically; SSH.NET does not, so both methods are offered here and
            // whichever the server actually accepts is used. Confirmed against real Cloud Key
            // hardware: password-only auth failed with "No suitable authentication method found
            // (publickey,keyboard-interactive)" until this was added.
            var keyboardInteractive = new KeyboardInteractiveAuthenticationMethod(profile.Username);
            keyboardInteractive.AuthenticationPrompt += (_, e) =>
            {
                foreach (var prompt in e.Prompts) prompt.Response = profile.Password!;
            };
            authMethods.Add(keyboardInteractive);
        }

        if (authMethods.Count == 0)
            throw new InvalidOperationException("Provide either a password or a private key to connect.");

        var connectionInfo = new ConnectionInfo(profile.Host, profile.Port, profile.Username, authMethods.ToArray())
        {
            Timeout = TimeSpan.FromSeconds(12),
        };

        Emit(TerminalLineKind.AppInfo, $"Connecting to {profile.Username}@{profile.Host}:{profile.Port} ...");

        await Task.Run(() =>
        {
            ct.ThrowIfCancellationRequested();
            var client = new SshClient(connectionInfo);
            client.Connect();

            var sftp = new SftpClient(connectionInfo);
            sftp.Connect();

            _client = client;
            _sftp = sftp;
        }, ct);

        // Make sure the work dir exists before anything tries to upload a script into it.
        await RunSimpleAsync($"mkdir -p {RemoteWorkDir}", ct);

        Emit(TerminalLineKind.AppInfo, "Connected.");
    }

    public void Disconnect()
    {
        try { _sftp?.Disconnect(); } catch { /* best effort */ }
        try { _client?.Disconnect(); } catch { /* best effort */ }
        _sftp?.Dispose();
        _client?.Dispose();
        _sftp = null;
        _client = null;
        Emit(TerminalLineKind.AppInfo, "Disconnected.");
    }

    /// <summary>Runs a short-lived command and waits for it to finish, returning exit code + captured
    /// output. Used for preflight checks and small state queries (dpkg-query, lsblk, etc) -- not for
    /// long-running scripts, which should use <see cref="RunStreamingAsync"/> instead.</summary>
    /// <param name="displayOverride">What to show in the terminal pane's command-echo line instead
    /// of the real command -- for the one case where the real command line necessarily contains a
    /// secret (setting a password via `... | chpasswd`). The real command still runs unchanged;
    /// only what's logged/displayed is redacted. Leave null for every normal call.</param>
    public async Task<(int ExitCode, string StdOut, string StdErr)> RunSimpleAsync(string command, CancellationToken ct = default, string? displayOverride = null)
    {
        EnsureConnected();
        Emit(TerminalLineKind.CommandEcho, displayOverride ?? command);

        return await Task.Run(() =>
        {
            using var cmd = _client!.CreateCommand(command);
            var stdOut = cmd.Execute();
            var stdErr = cmd.Error;

            foreach (var line in SplitLines(stdOut)) Emit(TerminalLineKind.StdOut, line);
            foreach (var line in SplitLines(stdErr)) Emit(TerminalLineKind.StdErr, line);

            return (cmd.ExitStatus ?? -1, stdOut, stdErr);
        }, ct);
    }

    /// <summary>Runs a command and streams stdout/stderr into <see cref="OutputReceived"/> as it's
    /// produced, for anything that takes more than a second or two (apt purge, dist stuff). Blocks
    /// the calling task until the remote command exits. Also returns the full combined output text
    /// (not just the exit code) -- <see cref="OutcomeInterpreter"/> needs to scan it for the actual
    /// scripts' known marker lines, since exit code alone is not a reliable go/no-go signal (a
    /// purge Simulate run is designed to exit non-zero on its safe "aborted before any change"
    /// path).</summary>
    /// <param name="displayOverride">What to show in the terminal pane's command-echo line instead
    /// of the real command -- same redaction pattern <see cref="RunSimpleAsync"/> already uses for
    /// chpasswd's plaintext password. Added after a real live incident: an Extra's env-var-prefixed
    /// invocation (e.g. `ADMIN_PASSWORD='...' bash script.sh`) was echoing the actual secret value
    /// straight into the terminal pane, unredacted -- <see cref="RunScriptStreamingAsync"/> is the
    /// caller that now builds a redacted version for any IsSecret Param and passes it here.</param>
    public async Task<(int ExitCode, string Output)> RunStreamingAsync(string command, CancellationToken ct = default, string? displayOverride = null)
    {
        EnsureConnected();
        Emit(TerminalLineKind.CommandEcho, displayOverride ?? command);

        return await Task.Run(() =>
        {
            var buffer = new StringBuilder();
            using var cmd = _client!.CreateCommand(command);
            var asyncResult = cmd.BeginExecute();

            // Only stream stdout live, and stop reading the instant the command is marked
            // complete -- no trailing "drain whatever's left" pass. Two things confirmed against
            // real Cloud Key hardware, both classic SSH.NET streaming gotchas:
            //   1. Reading two separate channel streams (stdout + ExtendedOutputStream for stderr)
            //      with blocking reads interleaved on one thread can deadlock -- fixed by reading
            //      only stdout live and grabbing stderr in one shot via cmd.Error afterward.
            //   2. Even a single stream's EndOfStream/ReadLine() can block forever waiting to
            //      confirm there's truly no more data, even after the remote command has already
            //      exited (verified live: `ps aux`/`fuser` on the box showed nothing running and no
            //      dpkg lock held while this was still stuck). There is no reliable way to bound a
            //      single synchronous Stream.Read() call in .NET, so the fix is to simply never make
            //      one we don't need: once IsCompleted is true, stop calling EndOfStream/ReadLine at
            //      all. In practice this loses nothing that matters -- everything a script actually
            //      prints streams through live during the loop below, well before completion.
            using (var outReader = new StreamReader(cmd.OutputStream, Encoding.UTF8))
            {
                while (!asyncResult.IsCompleted)
                {
                    ct.ThrowIfCancellationRequested();
                    if (!DrainLine(outReader, TerminalLineKind.StdOut, buffer))
                        Thread.Sleep(75);
                }
            }

            cmd.EndExecute(asyncResult);

            if (!string.IsNullOrWhiteSpace(cmd.Error))
            {
                foreach (var line in SplitLines(cmd.Error))
                {
                    Emit(TerminalLineKind.StdErr, line);
                    buffer.AppendLine(line);
                }
            }

            return (cmd.ExitStatus ?? -1, buffer.ToString());
        }, ct);
    }

    /// <summary>Reads and emits one line if one is immediately available (also appending it to
    /// <paramref name="buffer"/> for outcome interpretation); returns false without blocking
    /// indefinitely if not. Best-effort -- SSH.NET's output stream can still block briefly on a
    /// partial line, which is an accepted tradeoff for simplicity here.</summary>
    private bool DrainLine(StreamReader reader, TerminalLineKind kind, StringBuilder buffer)
    {
        if (reader.EndOfStream) return false;
        var line = reader.ReadLine();
        if (line is null) return false;
        Emit(kind, line);
        buffer.AppendLine(line);
        return true;
    }

    /// <summary>Uploads script content (loaded and hashed from this exe's embedded resources by
    /// <see cref="BundledScriptProvider"/> -- never
    /// hand-authored here) to the box's work dir, marks it executable, and streams its execution.
    /// The uploaded copy is left in place afterward as an on-device audit trail of exactly what ran.</summary>
    /// <param name="envPrefix">Optional `NAME='value' NAME2='value2'` prefix -- for scripts that read
    /// their config from environment variables (e.g. phase3-autossh-client.sh's RELAYS) rather than
    /// positional args. Caller is responsible for shell-quoting each value.</param>
    /// <param name="envPrefixDisplay">What to show in the terminal echo in place of envPrefix -- pass
    /// a redacted version (secret values replaced, e.g. with '***') when any of envPrefix's values
    /// are sensitive. Defaults to envPrefix itself (no redaction) when not supplied, for every
    /// existing caller that has nothing to hide.</param>
    public async Task<(int ExitCode, string Output)> RunScriptStreamingAsync(string scriptId, string scriptContent, string args, CancellationToken ct = default, string envPrefix = "", string? envPrefixDisplay = null)
    {
        EnsureConnected();
        var remotePath = $"{RemoteWorkDir}/{scriptId}.sh";
        var normalized = scriptContent.Replace("\r\n", "\n");

        await Task.Run(() =>
        {
            using var stream = new MemoryStream(Encoding.UTF8.GetBytes(normalized));
            _sftp!.UploadFile(stream, remotePath, canOverride: true);
        }, ct);

        await RunSimpleAsync($"chmod 755 {remotePath}", ct);

        // `< /dev/null` is load-bearing, not decoration: an SSH exec channel does NOT close stdin
        // just because we never write to it. Without this, a script's `read -rp` (the confirmation
        // prompt every Danger-tier script here has, hit on any non-"-y" i.e. Simulate run) blocks
        // forever waiting for input that will never arrive -- confirmed live against real Cloud Key
        // hardware: the app appeared hung, but the remote bash process was genuinely still alive and
        // parked at its own read prompt the whole time (ps aux only checked for apt/dpkg, which is
        // exactly why that didn't show up). This forces immediate EOF at the shell level, which is
        // precisely the non-interactive mode these scripts' own authors designed -y/--dry-run around.
        var prefix = string.IsNullOrWhiteSpace(envPrefix) ? "" : envPrefix.Trim() + " ";
        var command = string.IsNullOrWhiteSpace(args) ? $"{prefix}bash {remotePath} < /dev/null" : $"{prefix}bash {remotePath} {args} < /dev/null";

        string? display = null;
        if (envPrefixDisplay is not null && envPrefixDisplay != envPrefix)
        {
            var displayPrefix = string.IsNullOrWhiteSpace(envPrefixDisplay) ? "" : envPrefixDisplay.Trim() + " ";
            display = string.IsNullOrWhiteSpace(args) ? $"{displayPrefix}bash {remotePath} < /dev/null" : $"{displayPrefix}bash {remotePath} {args} < /dev/null";
        }
        return await RunStreamingAsync(command, ct, display);
    }

    /// <summary>Uploads a file into the remote work dir and marks it executable, without running it --
    /// for scripts that another script looks up as a sibling (e.g. phase1-security-setup.sh execs
    /// security-lock.sh via $(dirname "$0")) rather than being run directly by this app.</summary>
    public async Task UploadSupportFileAsync(string fileName, string content, CancellationToken ct = default)
    {
        EnsureConnected();
        var remotePath = $"{RemoteWorkDir}/{fileName}";
        var normalized = content.Replace("\r\n", "\n");

        await Task.Run(() =>
        {
            using var stream = new MemoryStream(Encoding.UTF8.GetBytes(normalized));
            _sftp!.UploadFile(stream, remotePath, canOverride: true);
        }, ct);

        await RunSimpleAsync($"chmod 755 {remotePath}", ct);
    }

    /// <summary>Uploads raw bytes to an arbitrary absolute remote path and chmods it -- for content
    /// that isn't a UTF-8 script, e.g. the compiled fdtscout binary (Extras' FDT.Scout web console).
    /// Unlike RunScriptStreamingAsync/UploadSupportFileAsync this is NOT confined to RemoteWorkDir,
    /// since a real installed binary belongs under /opt, not the scripts audit-trail directory --
    /// caller is responsible for creating the parent directory first (e.g. via RunSimpleAsync
    /// "mkdir -p ...") and for picking a sane absolute destination path.
    ///
    /// Uploads to a temp sibling path (same directory, so the final `mv` is a same-filesystem
    /// rename) and moves it into place afterward, rather than opening the destination path
    /// directly -- confirmed live (2026-08-23) that re-running an Extra to update an already-
    /// running binary (e.g. fdtscout.service actively executing /opt/fdtscout/fdtscout) fails the
    /// direct-write with a bare SFTP "Failure": Linux refuses a write-open on a file that's the
    /// current exec image of a running process (ETXTBSY), and OpenSSH's sftp-server reports that
    /// back with no further detail. `mv`/`rename()` doesn't need write access to the target
    /// inode's *content* (only the directory entry), so it works regardless of what's currently
    /// executing the old inode -- the already-running process keeps its old code mapped until
    /// restarted, which is expected and fine; the new binary is simply what the next start/restart
    /// picks up. Strictly safer even when nothing's running, too: no partial-write window if the
    /// connection drops mid-upload.</summary>
    public async Task UploadBinaryFileAsync(string remoteAbsolutePath, byte[] content, string chmodMode, CancellationToken ct = default)
    {
        EnsureConnected();
        var tempPath = $"{remoteAbsolutePath}.uploading-{Guid.NewGuid():N}";
        await Task.Run(() =>
        {
            using var stream = new MemoryStream(content);
            _sftp!.UploadFile(stream, tempPath, canOverride: true);
        }, ct);
        await RunSimpleAsync($"chmod {chmodMode} {tempPath}", ct);
        var (exitCode, _, stdErr) = await RunSimpleAsync($"mv -f {tempPath} {remoteAbsolutePath}", ct);
        if (exitCode != 0)
        {
            try { _sftp!.DeleteFile(tempPath); } catch { /* best effort cleanup */ }
            throw new InvalidOperationException($"Uploaded '{remoteAbsolutePath}' but couldn't move it into place: {stdErr}");
        }
    }

    /// <summary>Opens a raw interactive shell channel for the "drop to shell" pane. Caller owns the
    /// returned stream's lifetime (dispose it when leaving manual mode) and is responsible for wiring
    /// its DataReceived event into the terminal log.</summary>
    public ShellStream OpenInteractiveShell()
    {
        EnsureConnected();
        Emit(TerminalLineKind.AppInfo, "Opening interactive shell ...");
        return _client!.CreateShellStream("xterm", 120, 30, 800, 600, 4096);
    }

    /// <summary>Polls a host:port until a TCP connection attempt clearly fails (refused, reset, or
    /// doesn't complete within a couple seconds) or the timeout elapses. This is the FIRST half of a
    /// real reboot checkpoint -- a background `reboot` command takes a moment to actually start
    /// shutting things down, so polling for "is it back up" without first confirming it went down at
    /// all is a race: the very first probe can land while sshd is still fully up from before the
    /// reboot was even triggered, reporting a false "it's back" instantly. Only a real down-then-up
    /// transition (this method, then WaitForPortAsync) counts as a confirmed reboot.</summary>
    public static async Task<bool> WaitForPortDownAsync(string host, int port, TimeSpan timeout, CancellationToken ct = default)
    {
        var deadline = DateTime.UtcNow + timeout;
        while (DateTime.UtcNow < deadline)
        {
            ct.ThrowIfCancellationRequested();
            try
            {
                using var probe = new TcpClient();
                var connectTask = probe.ConnectAsync(host, port);
                var completed = await Task.WhenAny(connectTask, Task.Delay(2000, ct));
                if (completed != connectTask || !probe.Connected) return true; // didn't connect -- down
            }
            catch
            {
                return true; // refused/reset/unreachable -- down
            }
            await Task.Delay(1000, ct);
        }
        return false; // never observed it go down within the timeout
    }

    /// <summary>Polls a host:port until a TCP connection succeeds or the timeout elapses. Used after
    /// issuing `reboot` -- the wizard cannot know the box came back except by watching sshd reappear.
    /// Callers should confirm the port went DOWN first (see WaitForPortDownAsync) before calling this
    /// -- otherwise a still-up box from before the reboot was triggered can satisfy this immediately.</summary>
    public static async Task<bool> WaitForPortAsync(string host, int port, TimeSpan timeout, CancellationToken ct = default)
    {
        var deadline = DateTime.UtcNow + timeout;
        while (DateTime.UtcNow < deadline)
        {
            ct.ThrowIfCancellationRequested();
            try
            {
                using var probe = new TcpClient();
                var connectTask = probe.ConnectAsync(host, port);
                var completed = await Task.WhenAny(connectTask, Task.Delay(2000, ct));
                if (completed == connectTask && probe.Connected) return true;
            }
            catch
            {
                // expected while the box is down/rebooting -- keep polling
            }
            await Task.Delay(3000, ct);
        }
        return false;
    }

    private static IEnumerable<string> SplitLines(string text)
        => text.Replace("\r\n", "\n").Split('\n').Where(l => l.Length > 0);

    private void EnsureConnected()
    {
        if (!IsConnected) throw new InvalidOperationException("Not connected to the Cloud Key.");
    }

    private void Emit(TerminalLineKind kind, string text)
        => OutputReceived?.Invoke(new TerminalLine { Kind = kind, Text = text });

    public void Dispose() => Disconnect();
}
