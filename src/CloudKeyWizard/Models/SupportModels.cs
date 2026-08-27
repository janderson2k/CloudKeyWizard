namespace CloudKeyWizard.Models;

/// <summary>Connection details for the SSH session. Password is held only in memory for the
/// lifetime of the session -- never written to disk, never logged to the terminal pane.</summary>
public sealed class ConnectionProfile
{
    public string Host { get; set; } = string.Empty;
    public int Port { get; set; } = 22;
    public string Username { get; set; } = "root";
    public string? Password { get; set; }
    public string? PrivateKeyPath { get; set; }
    public string? PrivateKeyPassphrase { get; set; }
}

/// <summary>Result of one named preflight check (see PreflightService). The wizard renders a
/// checklist from a list of these before unlocking Page 2.</summary>
public sealed class PreflightCheckResult
{
    public required string Name { get; init; }
    public required bool Passed { get; init; }
    public required string Detail { get; init; }
    /// <summary>If false, a failure here is shown as a warning but does not block "Next".</summary>
    public bool IsBlocking { get; init; } = true;
}

/// <summary>Aggregate preflight outcome: identified hardware + every individual check.</summary>
public sealed class PreflightResult
{
    public DeviceModel Model { get; init; } = DeviceModel.Unknown;
    public string? BaseFilesPackage { get; init; }
    public List<PreflightCheckResult> Checks { get; init; } = new();
    public bool AllBlockingPassed => Checks.Where(c => c.IsBlocking).All(c => c.Passed);
}

/// <summary>Whether <see cref="BlockDeviceService"/>'s heuristic thinks this device is a sane
/// choice to wipe. Advisory only -- the operator still has to select it and type its exact path
/// to confirm; this never auto-picks anything the operator hasn't seen and agreed to.</summary>
public enum DeviceRisk
{
    /// <summary>Unmounted, not a known boot/secure/virtual device -- looks like real bulk storage.</summary>
    Recommended,
    /// <summary>Mounted, or a known boot/secure/virtual device name pattern (root fs, RPMB, raw
    /// NAND, zram swap, ...) -- wiping this would very likely brick or break the box.</summary>
    DoNotSelect,
    /// <summary>Doesn't match either pattern confidently -- no guidance offered either way.</summary>
    Unknown,
}

/// <summary>One block device row from `lsblk -J`, used to populate the storage-wipe step's device
/// picker. Deliberately never accepts a free-typed path -- see MainViewModel.LoadBlockDevicesAsync.</summary>
public sealed class BlockDeviceInfo
{
    public string Name { get; init; } = string.Empty;
    public string Path { get; init; } = string.Empty;
    public string Size { get; init; } = string.Empty;
    public string? Model { get; init; }
    public string? Mountpoint { get; init; }
    public string Type { get; init; } = string.Empty;
    public DeviceRisk Risk { get; init; } = DeviceRisk.Unknown;
    /// <summary>Plain-language reason for the Risk verdict, shown next to the device in the picker.</summary>
    public string RiskReason { get; init; } = string.Empty;

    public override string ToString()
    {
        var badge = Risk switch
        {
            DeviceRisk.Recommended => "✓ Suggested — ",
            DeviceRisk.DoNotSelect => "⚠ DO NOT SELECT — ",
            _ => "",
        };
        return $"{badge}{Path}  ({Size}, {Model ?? Type}){(string.IsNullOrEmpty(Mountpoint) ? "" : $"  [mounted at {Mountpoint}]")}";
    }
}

/// <summary>One line appended to the right-hand terminal pane. Kept structured (not just a string)
/// so the view can color stdout/stderr/command-echo differently.</summary>
public sealed class TerminalLine
{
    public required string Text { get; init; }
    public required TerminalLineKind Kind { get; init; }
    public DateTime Timestamp { get; init; } = DateTime.Now;
}

public enum TerminalLineKind
{
    /// <summary>Echo of a command the app or the user is about to run.</summary>
    CommandEcho,
    StdOut,
    StdErr,
    /// <summary>App-generated status line (connecting, reconnecting, step boundaries), not device output.</summary>
    AppInfo,
}
