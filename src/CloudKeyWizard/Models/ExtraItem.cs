using CloudKeyWizard.ViewModels;

namespace CloudKeyWizard.Models;

public enum ExtraCategory
{
    General,
    MediaServer,
    RemoteAccess,
    HomeAutomation,
    Backup,
}

/// <summary>One named environment-variable input an Extra's script reads (e.g. RELAYS, TIER1_PORT
/// for AutoSSH). Passed to the remote script as `NAME='value' bash script.sh`, not positional args
/// -- matches how the real bundled scripts (e.g. phase3-autossh-client.sh) actually read config.</summary>
public sealed class ExtraParam : ObservableObject
{
    public required string EnvVarName { get; init; }
    public required string Label { get; init; }
    public string HelpText { get; init; } = string.Empty;
    public bool IsRequired { get; init; } = true;

    /// <summary>True for a value that's genuinely multi-line (e.g. a pasted WireGuard peer
    /// config) -- shown as a taller, wrapping, paste-friendly box instead of a single-line one.</summary>
    public bool IsMultiline { get; init; }

    /// <summary>True for a value that shouldn't be plainly visible while typed (a password, or a
    /// pasted config containing a private key) -- masked by default, with a reveal toggle rather
    /// than always-plaintext. Before this, every Extra Param -- including ADMIN_PASSWORD and
    /// WireGuard's WG_CONFIG -- rendered in fully visible plain text with no masking at all.</summary>
    public bool IsSecret { get; init; }

    /// <summary>True for a fixed-choice param shown as a dropdown instead of free text (e.g.
    /// Timezone) -- <see cref="Options"/> is the legal value list. Curated in this Windows app
    /// (not live-queried from the device -- Extras render all at once rather than one-at-a-time
    /// like the wizard steps, so there's no natural "selected step" moment to trigger an SSH round
    /// trip the way LoadPurgePreviewAsync/LoadBlockDevicesAsync do); FDT.Scout's own Settings tab
    /// timezone control queries the real device's tzdata live instead, since it already runs on
    /// the box as root.</summary>
    public bool IsDropdown { get; init; }
    public List<string> Options { get; init; } = new();

    /// <summary>Whether a secret's reveal toggle is currently showing plaintext. Only meaningful
    /// when IsSecret is true; ignored otherwise.</summary>
    private bool _isRevealed;
    public bool IsRevealed
    {
        get => _isRevealed;
        set
        {
            if (SetField(ref _isRevealed, value))
            {
                OnPropertyChanged(nameof(ShowMaskedBox));
                OnPropertyChanged(nameof(ShowRevealedSecretBox));
            }
        }
    }

    // Three mutually-exclusive display states for the single-line case (IsMultiline's own two-way
    // toggle is handled separately, unaffected by IsSecret -- a multiline secret like WG_CONFIG
    // just isn't maskable the same way a single password field is). Computed rather than stored so
    // there's exactly one place (IsRevealed's setter above) that has to remember to notify them.
    public bool ShowPlainTextBox => !IsMultiline && !IsSecret && !IsDropdown;
    public bool ShowMaskedBox => !IsMultiline && IsSecret && !IsRevealed;
    public bool ShowRevealedSecretBox => !IsMultiline && IsSecret && IsRevealed;
    public bool ShowDropdown => !IsMultiline && !IsSecret && IsDropdown;

    private string _value = string.Empty;
    public string Value { get => _value; set => SetField(ref _value, value ?? string.Empty); }
}

/// <summary>One BinaryUploads entry: a resource file embedded under Scripts/, its destination
/// absolute path on the device, and the chmod mode to apply after upload.</summary>
public sealed record BinaryUpload(string ResourceFileName, string RemotePath, string ChmodMode = "755");

/// <summary>
/// One optional post-conversion adjustment/install, shown grouped by category on the Extras step.
/// Unlike the core Phase 1 steps, these are independent of each other (no ordering, no shared
/// confirmation flow) -- each has its own inline Run control.
/// </summary>
public sealed class ExtraItem : ObservableObject
{
    public required string Id { get; init; }
    public required string Title { get; init; }
    public required string Description { get; init; }
    public required ExtraCategory Category { get; init; }

    /// <summary>False = a "coming soon" placeholder shown for visibility/roadmap purposes, with no
    /// Run control -- see ExtraCatalog for why (real config inputs this app doesn't have UI for yet).</summary>
    public bool IsAvailable { get; init; } = true;

    /// <summary>Inline script content authored directly by this app -- NOT sourced from
    /// jnovack/cloudkey, unlike everything in Scripts/runbook/. Mutually exclusive with
    /// <see cref="BundledScriptFileName"/>. Kept short, standard, and idempotent.</summary>
    public string? ScriptContent { get; init; }

    /// <summary>Filename under Scripts/runbook/ for an Extra that's real upstream jnovack/cloudkey
    /// content (e.g. AutoSSH) rather than app-authored -- loaded via BundledScriptProvider, same
    /// hash-and-provenance treatment as the Phase 1 steps. Mutually exclusive with
    /// <see cref="ScriptContent"/>.</summary>
    public string? BundledScriptFileName { get; init; }

    /// <summary>Named env-var inputs this Extra's script needs. Empty for the simple no-config
    /// items (timezone, fail2ban, ...).</summary>
    public List<ExtraParam> Params { get; init; } = new();

    /// <summary>A bundled script (filename under Scripts/runbook/) to run to completion BEFORE the
    /// main one, aborting without running the main script if it fails -- e.g.
    /// phase2-00-provision-drive.sh before any of the Servarr app installers. Run automatically,
    /// not a separate click, since it's idempotent and required rather than optional.</summary>
    public string? PrerequisiteBundledScriptFileName { get; init; }

    /// <summary>Bundled scripts (filenames under Scripts/runbook/) uploaded alongside the main one
    /// but never executed directly -- for a script the main one looks up via $(dirname "$0") at
    /// runtime, e.g. phase2-05-servarr-sqlite-heal.sh for Radarr/Prowlarr (same pattern as
    /// phase1-security-setup.sh's security-lock.sh dependency).</summary>
    public List<string> SiblingBundledScriptFileNames { get; init; } = new();

    /// <summary>Non-script content (a compiled binary, a systemd unit) uploaded byte-for-byte to an
    /// absolute remote path BEFORE the main script runs -- unlike SiblingBundledScriptFileNames
    /// (always text, always RemoteWorkDir-relative), this covers content that can't round-trip
    /// through a UTF-8 string (e.g. the fdtscout ELF binary) and needs to land at a real install
    /// path. The parent directory is created automatically if missing.</summary>
    public List<BinaryUpload> BinaryUploads { get; init; } = new();

    private StepStatus _status = StepStatus.Pending;
    public StepStatus Status { get => _status; set => SetField(ref _status, value); }

    private string _detail = string.Empty;
    public string Detail { get => _detail; set => SetField(ref _detail, value); }
}
