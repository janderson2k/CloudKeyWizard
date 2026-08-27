using System.Text.Json;

namespace CloudKeyWizard.Services;

/// <summary>
/// A small JSON state file written TO THE DEVICE ITSELF (not just locally in this Windows app's
/// AppData, unlike Services/KnownHostsStore.cs) recording which Extras are installed. Solves a
/// real continuity gap: if a *different* copy of CloudKey Wizard (a different Windows machine)
/// later connects to this same already-converted Cloud Key, it has no local memory of what's been
/// done -- this file is that memory, carried on the device instead. Same schema/field names as the
/// matching store in FDT.Scout (tools/fdtscout/devicestate.go) -- both apps read and write this
/// exact file, and either one installing something updates it, so the two stay in sync regardless
/// of which app someone happens to be using at the time.
///
/// Device-authoritative by design: when this file exists, its contents override whatever a local
/// KnownHosts.json entry remembers for the same host -- the file reflects what's actually true on
/// the device, while local memory can go stale (edited from elsewhere, or just old).
/// </summary>
public sealed class DeviceStateStore
{
    public const string FileName = "fdtscout-state.json";

    public sealed record DeviceState(
        Dictionary<string, string> ExtraStatuses,
        Dictionary<string, string> ExtraDetails,
        string DetectedModel,
        string LastUpdatedBy,
        DateTime LastUpdatedAt
    );

    /// <summary>Reads the state file from the device, if present. Returns null if the file doesn't
    /// exist yet (first time this device has been touched by either app) or fails to parse (treated
    /// the same as absent, rather than throwing -- a corrupt remote file shouldn't block Connect).</summary>
    public static async Task<DeviceState?> ReadAsync(SshSessionService ssh, CancellationToken ct = default)
    {
        var (exitCode, stdOut, _) = await ssh.RunSimpleAsync($"cat {SshSessionService.RemoteWorkDir}/{FileName} 2>/dev/null", ct);
        if (exitCode != 0 || string.IsNullOrWhiteSpace(stdOut)) return null;
        try
        {
            return JsonSerializer.Deserialize<DeviceState>(stdOut, JsonOptions);
        }
        catch (JsonException)
        {
            return null;
        }
    }

    public static async Task WriteAsync(SshSessionService ssh, Dictionary<string, string> extraStatuses,
        Dictionary<string, string> extraDetails, string detectedModel, string updatedBy, CancellationToken ct = default)
    {
        var state = new DeviceState(extraStatuses, extraDetails, detectedModel, updatedBy, DateTime.UtcNow);
        var json = JsonSerializer.Serialize(state, JsonOptions);
        await ssh.UploadSupportFileAsync(FileName, json, ct);
    }

    private static readonly JsonSerializerOptions JsonOptions = new() { PropertyNamingPolicy = JsonNamingPolicy.CamelCase };
}
