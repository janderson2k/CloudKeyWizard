using System.IO;
using System.Reflection;
using System.Security.Cryptography;
using System.Text;

namespace CloudKeyWizard.Services;

/// <summary>
/// Reads runbook scripts from resources embedded in this exe at build time -- no network fetch at
/// runtime, no dependency on GitHub being reachable. See Scripts/runbook/README.md for exactly
/// where these came from (a specific jnovack/cloudkey commit) and how to update them. The SSH
/// connection to the Cloud Key itself is the only network activity this app performs.
/// </summary>
public sealed class BundledScriptProvider
{
    /// <summary>The jnovack/cloudkey commit these bundled scripts were copied from verbatim.
    /// Purely informational now (nothing fetches this at runtime) -- kept so the UI can show
    /// exactly which upstream version is embedded, and as the basis for SourceUrlFor below.</summary>
    public const string SourceCommitSha = "47fa33bf412deaadec36676b9abbee841bbdfa43";

    private static readonly Assembly Asm = typeof(BundledScriptProvider).Assembly;

    public sealed record LoadedScript(string FileName, string Content, string Sha256Hex);

    public Task<LoadedScript> GetScriptAsync(string fileName, CancellationToken ct = default)
    {
        // Match by suffix rather than hardcoding the full dotted logical name (RootNamespace +
        // folder path) -- robust to the project's namespace/folder structure shifting later
        // without this silently starting to throw.
        var suffix = "." + fileName;
        var resourceName = Asm.GetManifestResourceNames().FirstOrDefault(n => n.EndsWith(suffix, StringComparison.Ordinal));
        if (resourceName is null)
            throw new InvalidOperationException($"'{fileName}' is not bundled into this build. Available: {string.Join(", ", Asm.GetManifestResourceNames())}");

        using var stream = Asm.GetManifestResourceStream(resourceName)
            ?? throw new InvalidOperationException($"Embedded resource '{resourceName}' could not be opened.");
        using var reader = new StreamReader(stream, Encoding.UTF8);
        var content = reader.ReadToEnd();

        return Task.FromResult(new LoadedScript(fileName, content, Sha256(content)));
    }

    /// <summary>Reference link only -- not fetched. Lets the UI show "here's the exact upstream
    /// file this bundled copy came from" for anyone who wants to compare.</summary>
    public static string SourceUrlFor(string fileName)
        => $"https://github.com/jnovack/cloudkey/blob/{SourceCommitSha}/scripts/runbook/{fileName}";

    public sealed record LoadedBinary(string FileName, byte[] Content, string Sha256Hex);

    /// <summary>Same lookup as GetScriptAsync but reads raw bytes, not UTF-8 text -- for embedded
    /// content that isn't a script, e.g. the compiled fdtscout binary (App-authored, not
    /// jnovack/cloudkey content -- see Scripts/fdtscout/README.md).</summary>
    public Task<LoadedBinary> GetBinaryAsync(string fileName, CancellationToken ct = default)
    {
        var suffix = "." + fileName;
        var resourceName = Asm.GetManifestResourceNames().FirstOrDefault(n => n.EndsWith(suffix, StringComparison.Ordinal));
        if (resourceName is null)
            throw new InvalidOperationException($"'{fileName}' is not bundled into this build. Available: {string.Join(", ", Asm.GetManifestResourceNames())}");

        using var stream = Asm.GetManifestResourceStream(resourceName)
            ?? throw new InvalidOperationException($"Embedded resource '{resourceName}' could not be opened.");
        using var mem = new MemoryStream();
        stream.CopyTo(mem);
        var bytes = mem.ToArray();

        return Task.FromResult(new LoadedBinary(fileName, bytes, Convert.ToHexString(SHA256.HashData(bytes)).ToLowerInvariant()));
    }

    private static string Sha256(string text)
    {
        var bytes = SHA256.HashData(Encoding.UTF8.GetBytes(text.Replace("\r\n", "\n")));
        return Convert.ToHexString(bytes).ToLowerInvariant();
    }
}
