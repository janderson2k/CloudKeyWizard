namespace CloudKeyWizard.Models;

/// <summary>
/// One remembered device this app has connected to before, persisted locally so returning to a
/// Cloud Key means picking it from a list instead of retyping connection details. Never stores a
/// password -- only what's needed to prefill the Connect page and to restore Extras' status (the
/// one thing that doesn't self-resume from live device detection the way Phase 1 steps do).
/// </summary>
public sealed class KnownHost
{
    public string Host { get; set; } = string.Empty;
    public string Username { get; set; } = string.Empty;
    public bool UseKeyAuth { get; set; }
    public string? PrivateKeyPath { get; set; }
    public DateTime LastConnected { get; set; }
    public string DetectedModel { get; set; } = "Unknown";

    /// <summary>Extra id -> its last-known StepStatus (as a string, so this survives enum reordering
    /// across app versions without needing a migration). Phase 1 step statuses are deliberately NOT
    /// stored here -- live device detection is the single source of truth for those.</summary>
    public Dictionary<string, string> ExtraStatuses { get; set; } = new();
    public Dictionary<string, string> ExtraDetails { get; set; } = new();

    public override string ToString()
        => $"{Host}  ({Username}, {(UseKeyAuth ? "key" : "password")})  — last connected {LastConnected:g}";
}
