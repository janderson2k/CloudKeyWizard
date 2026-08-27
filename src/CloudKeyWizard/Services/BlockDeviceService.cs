using System.Text.Json;
using CloudKeyWizard.Models;

namespace CloudKeyWizard.Services;

/// <summary>
/// Lists the box's actual block devices via `lsblk -J` so the storage-wipe step can offer a picker
/// instead of a free-typed device path. A typo in a hand-typed /dev path is exactly the kind of
/// mistake that turns "format the data drive" into "format the boot drive".
/// </summary>
public static class BlockDeviceService
{
    public static async Task<List<BlockDeviceInfo>> GetDisksAsync(SshSessionService ssh, CancellationToken ct = default)
    {
        var (exitCode, stdOut, _) = await ssh.RunSimpleAsync(
            "lsblk -J -b -o NAME,PATH,SIZE,MODEL,MOUNTPOINT,TYPE", ct);

        if (exitCode != 0 || string.IsNullOrWhiteSpace(stdOut))
            throw new InvalidOperationException("lsblk did not return usable output -- cannot safely offer a device picker.");

        using var doc = JsonDocument.Parse(stdOut);
        var devices = new List<BlockDeviceInfo>();

        foreach (var dev in doc.RootElement.GetProperty("blockdevices").EnumerateArray())
        {
            var type = GetString(dev, "type");
            if (type != "disk") continue; // only whole disks -- format-mount-volume targets a whole disk, not a partition

            var mountpoints = CollectMountpoints(dev);
            var name = GetString(dev, "name") ?? "";
            var mountpoint = mountpoints.Count == 0 ? null : string.Join(", ", mountpoints);
            var (risk, reason) = ClassifyRisk(name, mountpoint);

            devices.Add(new BlockDeviceInfo
            {
                Name = name,
                Path = GetString(dev, "path") ?? $"/dev/{name}",
                Size = FormatSize(GetNumberAsString(dev, "size")),
                Model = GetString(dev, "model"),
                Type = type ?? "disk",
                Mountpoint = mountpoint,
                Risk = risk,
                RiskReason = reason,
            });
        }

        return devices;
    }

    /// <summary>
    /// Advisory classification only -- shown as a badge in the picker, never auto-applied without
    /// the operator explicitly selecting the device and typing its path to confirm. Deliberately
    /// name/mountpoint-pattern based rather than trying to be clever about it: these patterns
    /// (mtd = raw NAND boot flash, rpmb = the eMMC's write-protected secure partition, zram = a
    /// virtual compressed-swap device, not a real disk at all, and anything actually mounted) are
    /// standard Linux block-device conventions, not board-specific guesses.
    /// </summary>
    private static (DeviceRisk Risk, string Reason) ClassifyRisk(string name, string? mountpoint)
    {
        if (!string.IsNullOrWhiteSpace(mountpoint))
            return (DeviceRisk.DoNotSelect, $"Currently mounted at {mountpoint} -- likely the live system.");
        if (name.StartsWith("mtd", StringComparison.OrdinalIgnoreCase))
            return (DeviceRisk.DoNotSelect, "Raw NAND boot flash (bootloader/kernel), not bulk storage.");
        if (name.EndsWith("rpmb", StringComparison.OrdinalIgnoreCase))
            return (DeviceRisk.DoNotSelect, "eMMC RPMB partition -- a secure/write-protected region, not a data target.");
        if (name.StartsWith("zram", StringComparison.OrdinalIgnoreCase))
            return (DeviceRisk.DoNotSelect, "Virtual compressed-swap device, not a physical disk.");
        return (DeviceRisk.Recommended, "Unmounted, not a known boot/secure/virtual device -- looks like real bulk storage.");
    }

    private static List<string> CollectMountpoints(JsonElement dev)
    {
        var found = new List<string>();
        var self = GetString(dev, "mountpoint");
        if (!string.IsNullOrWhiteSpace(self)) found.Add(self);

        if (dev.TryGetProperty("children", out var children) && children.ValueKind == JsonValueKind.Array)
        {
            foreach (var child in children.EnumerateArray())
                found.AddRange(CollectMountpoints(child));
        }

        return found;
    }

    private static string? GetString(JsonElement el, string prop)
        => el.TryGetProperty(prop, out var v) && v.ValueKind == JsonValueKind.String ? v.GetString() : null;

    /// <summary>Some lsblk/util-linux versions emit numeric-looking JSON fields (like SIZE with -b)
    /// as a real JSON number rather than a quoted string. GetString alone silently returns null for
    /// those, which was showing "unknown size" for every device on real hardware -- accept either.</summary>
    private static string? GetNumberAsString(JsonElement el, string prop)
    {
        if (!el.TryGetProperty(prop, out var v)) return null;
        return v.ValueKind switch
        {
            JsonValueKind.String => v.GetString(),
            JsonValueKind.Number => v.GetRawText(),
            _ => null,
        };
    }

    private static string FormatSize(string? bytesText)
    {
        if (!long.TryParse(bytesText, out var bytes)) return bytesText ?? "unknown size";
        string[] units = { "B", "KB", "MB", "GB", "TB" };
        double size = bytes;
        var unit = 0;
        while (size >= 1024 && unit < units.Length - 1) { size /= 1024; unit++; }
        return $"{size:0.#} {units[unit]}";
    }
}
