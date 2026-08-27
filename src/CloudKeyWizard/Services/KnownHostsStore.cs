using System.IO;
using System.Text.Json;
using CloudKeyWizard.Models;

namespace CloudKeyWizard.Services;

/// <summary>Reads/writes the local known-hosts list. Local file only -- never synced anywhere,
/// never contains a password.</summary>
public static class KnownHostsStore
{
    private static readonly string FilePath = Path.Combine(
        Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
        "CloudKeyWizard", "hosts.json");

    public static List<KnownHost> Load()
    {
        try
        {
            if (!File.Exists(FilePath)) return new List<KnownHost>();
            var json = File.ReadAllText(FilePath);
            return JsonSerializer.Deserialize<List<KnownHost>>(json) ?? new List<KnownHost>();
        }
        catch
        {
            // A corrupt/unreadable hosts file shouldn't block the app from starting -- just start
            // with an empty list rather than throwing out of the constructor.
            return new List<KnownHost>();
        }
    }

    public static void Save(IEnumerable<KnownHost> hosts)
    {
        Directory.CreateDirectory(Path.GetDirectoryName(FilePath)!);
        var json = JsonSerializer.Serialize(hosts.ToList(), new JsonSerializerOptions { WriteIndented = true });
        File.WriteAllText(FilePath, json);
    }

    /// <summary>For File > Export/Import -- an arbitrary path the user picked, not the default store.</summary>
    public static List<KnownHost> LoadFrom(string path)
    {
        var json = File.ReadAllText(path);
        return JsonSerializer.Deserialize<List<KnownHost>>(json) ?? new List<KnownHost>();
    }

    public static void SaveTo(string path, IEnumerable<KnownHost> hosts)
    {
        var json = JsonSerializer.Serialize(hosts.ToList(), new JsonSerializerOptions { WriteIndented = true });
        File.WriteAllText(path, json);
    }
}
