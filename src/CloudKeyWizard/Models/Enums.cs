namespace CloudKeyWizard.Models;

/// <summary>Lifecycle status of a single wizard step, driven by live device state, not just "did the app click through it".</summary>
public enum StepStatus
{
    /// <summary>Not reachable yet (a prerequisite step hasn't completed).</summary>
    Locked,
    /// <summary>Reachable, not yet done.</summary>
    Pending,
    /// <summary>The wizard is on this step right now.</summary>
    Current,
    /// <summary>Detected as already satisfied on the box (idempotency check passed) or completed this session.</summary>
    Done,
    /// <summary>The user chose to skip an optional step.</summary>
    Skipped,
    /// <summary>The step ran and failed, or its verify check failed.</summary>
    Failed,
}

/// <summary>How dangerous/irreversible a step is. Drives the warning banner style and whether typed confirmation is required.</summary>
public enum StepSeverity
{
    /// <summary>Read-only or purely informational (checks, verify passes).</summary>
    Info,
    /// <summary>Changes state but is low-risk / already guarded by the script itself.</summary>
    Caution,
    /// <summary>Irreversible or capable of bricking/locking out the box. Requires typed confirmation, not just a click.</summary>
    Danger,
}

/// <summary>Cloud Key hardware generation, positively identified from the installed base-files package name
/// rather than trusted from user input -- see PreflightService.</summary>
public enum DeviceModel
{
    Unknown,
    CloudKeyGen2,
    CloudKeyGen2Plus,
}
