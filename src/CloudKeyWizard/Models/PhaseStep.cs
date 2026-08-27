using CloudKeyWizard.ViewModels;

namespace CloudKeyWizard.Models;

/// <summary>
/// One step in the wizard. Data-only definition of *what* a step is; <see cref="Status"/> and
/// <see cref="LastVerifyOk"/> are the only mutable, bindable bits, updated live as the wizard
/// runs and as PreflightService re-detects device state.
/// </summary>
public sealed class PhaseStep : ObservableObject
{
    /// <summary>Stable id used for ordering/lookups, e.g. "purge", "format-volume".</summary>
    public required string Id { get; init; }

    public required string Title { get; init; }

    /// <summary>Plain-English "what this does", shown in the middle pane.</summary>
    public required string Description { get; init; }

    public required StepSeverity Severity { get; init; }

    /// <summary>
    /// Filename under scripts/runbook/ in the jnovack/cloudkey repo, fetched (never embedded/guessed)
    /// via ScriptCatalog.PinnedCommitSha at run time. Null for pure-UI steps (Connect, Identify,
    /// Reboot-wait, Finish) that don't run a runbook script directly.
    /// </summary>
    public string? ScriptFileName { get; init; }

    /// <summary>True for a bundled script that's app-authored (not sourced from jnovack/cloudkey)
    /// even though it's loaded through the same ScriptFileName/BundledScriptProvider mechanism as
    /// the jnovack-sourced ones -- affects only how its provenance is labeled in the terminal log
    /// (MainViewModel.RunSelectedStepAsync), never which scripts get bundled or how they run.</summary>
    public bool IsAppAuthored { get; init; }

    /// <summary>Whether the script accepts a "--simulate"/dry-run style preview before Apply is offered.</summary>
    public bool SupportsSimulate { get; init; }

    /// <summary>If set, this step is only offered when the detected hardware matches (e.g. the
    /// drive-bay format/mount step is Gen2 Plus only). Null = applies to any model.</summary>
    public DeviceModel? RequiresModel { get; init; }

    /// <summary>True if the step can be safely skipped by the user (optional add-ons like the LCD app).</summary>
    public bool IsOptional { get; init; }

    /// <summary>What must be true, in plain words, before this step's Apply is confirmable.
    /// Shown next to the typed-confirmation box for Danger-severity steps.</summary>
    public string? ConfirmationPhrase { get; init; }

    private StepStatus _status = StepStatus.Locked;
    public StepStatus Status
    {
        get => _status;
        set => SetField(ref _status, value);
    }

    private string _detail = string.Empty;
    /// <summary>Short live status line under the title in the left nav, e.g. "already applied" or "failed: see log".</summary>
    public string Detail
    {
        get => _detail;
        set => SetField(ref _detail, value);
    }
}
