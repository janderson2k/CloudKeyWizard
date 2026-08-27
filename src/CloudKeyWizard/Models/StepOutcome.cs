namespace CloudKeyWizard.Models;

public enum OutcomeVerdict
{
    Go,
    NoGo,
    Inconclusive,
}

/// <summary>The plain-language call OutcomeInterpreter renders after a step's run finishes, shown
/// as a banner instead of making the operator parse scrollback themselves.</summary>
public sealed class StepOutcome
{
    public required OutcomeVerdict Verdict { get; init; }
    public required string Reason { get; init; }

    /// <summary>True only for a specific, recognized NoGo where re-running the exact same script is
    /// known-safe and likely to just work -- e.g. purge's batch-vs-dependency-cascade race, where the
    /// script itself recomputes what's actually left to do on every invocation. When true,
    /// MainViewModel auto-retries the step once instead of leaving it Failed and waiting for the
    /// operator to notice the explanation and click Apply again by hand. Never set for a genuine
    /// ABORT (forbidden package, liveness check, unexpected simulate mismatch) -- those still need a
    /// human to actually read what happened before proceeding.</summary>
    public bool SafeAutoRetry { get; init; }
}
