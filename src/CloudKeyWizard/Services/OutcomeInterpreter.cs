using CloudKeyWizard.Models;

namespace CloudKeyWizard.Services;

/// <summary>
/// Turns a step's raw captured output into a plain GO / NO-GO / INCONCLUSIVE call, so the operator
/// isn't left to parse scrollback themselves to decide whether it's safe to proceed.
///
/// Deliberately conservative: anything that doesn't match a known-good or known-bad marker from
/// the actual bundled script text (see Scripts/runbook/) falls to Inconclusive rather than ever
/// guessing Go. Exit code alone is NOT a reliable signal here -- e.g. purge's *safe* Simulate run
/// is designed to exit non-zero (it aborts cleanly at an EOF confirmation prompt before changing
/// anything), so a naive "0 = good" rule would call that safe outcome a failure.
/// </summary>
public static class OutcomeInterpreter
{
    public static StepOutcome Interpret(string stepId, bool simulate, int exitCode, string output) => stepId switch
    {
        "purge" => simulate ? InterpretPurgeSimulate(output) : InterpretPurgeApply(exitCode, output),
        "format-volume" => simulate ? InterpretFormatVolumeSimulate(output) : InterpretFormatVolumeApply(exitCode, output),
        "base-tools" => InterpretGeneric(exitCode, output, mustContain: "base tooling present"),
        "security" => InterpretGeneric(exitCode, output, mustContain: "SAFE state applied",
            badMarkers: new[] { "FATAL", "REFUSING to lock down" }),
        "verify" => InterpretVerify(exitCode),
        _ => Default(exitCode),
    };

    private static StepOutcome InterpretPurgeSimulate(string output)
    {
        if (Contains(output, "ABORT: simulate wants to remove forbidden package"))
            return NoGo("The simulate step found a forbidden package (ck-ui or ubnt-tools) in the removal list. Do not proceed -- this needs investigation before ever purging.");
        if (Contains(output, "ABORT: simulated removal list does not exactly match"))
            return NoGo("The simulated removal list didn't exactly match the expected target packages -- something unexpected would be removed. Do not proceed without reviewing this by hand.");
        if (Contains(output, "Simulate matches exactly the remaining target packages. Safe to proceed."))
            return Go("The package removal list matches exactly what's expected, and neither ck-ui nor ubnt-tools would be touched. It then aborted safely before making any change, as designed for a no-'-y' run. Safe to Apply.");
        return Inconclusive("Didn't see the expected 'Simulate matches...' line or a known ABORT reason in the output -- read the terminal yourself before deciding.");
    }

    private static StepOutcome InterpretPurgeApply(int exitCode, string output)
    {
        // A batch purge can legitimately fail with "Unable to locate package X" when an EARLIER
        // batch's apt-get already cascaded and removed X as a dependency side-effect (observed live:
        // purging mongodb-server pulled the 'unifi' package out with it before batch 3 got to it by
        // name). apt can no longer resolve a package by name once it's gone and isn't in apt's own
        // index (true for these locally-installed vendor packages). This is NOT a dangerous failure --
        // the script recomputes what's still actually installed at the start of every run (Step 0's
        // remaining_packages()) and skips any batch with nothing left to purge, so simply re-running
        // it is genuinely safe -- Step 0's simulate/exact-match gate re-runs fresh every time too,
        // giving the retry the same safety check as the original run. MainViewModel.RunSelectedStepAsync
        // auto-retries once when SafeAutoRetry is set here, rather than leaving the operator to notice
        // this explanation and click Apply again by hand. (The real fix -- have each batch re-check
        // live dpkg state right before purging instead of a stale Step-0 snapshot -- lives in
        // phase1-purge.sh itself, which is a verbatim jnovack/cloudkey file this app doesn't silently
        // patch; see Scripts/runbook/README.md. This auto-retry is the app-authored answer instead.)
        if (Contains(output, "E: Unable to locate package") && Contains(output, "ABORT: apt-get purge failed for batch"))
            return NoGo("A batch failed because apt couldn't resolve one of its packages by name -- almost always because an earlier batch's purge already removed it as a dependency-cascade side effect. This is a known-safe situation: the script recomputes what's left on every run and skips anything already done, so this app retries it automatically once.", safeAutoRetry: true);
        if (Contains(output, "ABORT:"))
            return NoGo("The script hit an ABORT during the real run. Stop and read the terminal output before doing anything else on this box.");
        if (exitCode == 0 && Contains(output, "Steps 0, 1, 2, 4 complete"))
            return Go("Purge completed all steps. Next: reboot the box, then run Post-Reboot Verify.");
        return exitCode == 0
            ? Inconclusive("Exited 0 but didn't show the expected completion marker -- worth a manual read before rebooting.")
            : NoGo($"Exited with code {exitCode}. Do not reboot yet -- read the terminal output first.");
    }

    private static StepOutcome InterpretFormatVolumeSimulate(string output)
    {
        if (Contains(output, "is not a block device"))
            return NoGo("The selected device wasn't recognized as a block device by the script. Do not proceed with this selection.");
        return Go("Preview only -- device info was printed and the script aborted before wiping anything (no -y was supplied). Nothing on the device changed. Double-check the device details above before Apply.");
    }

    private static StepOutcome InterpretFormatVolumeApply(int exitCode, string output)
    {
        if (exitCode == 0 && Contains(output, "Done. /volume is mounted"))
            return Go("Drive wiped, reformatted, and mounted at /volume.");
        return NoGo($"Exited with code {exitCode} without the expected 'Done. /volume is mounted' confirmation -- check the drive's actual state before assuming this worked.");
    }

    private static StepOutcome InterpretVerify(int exitCode)
        => exitCode == 0
            ? Go("Verify ran clean. Check the 'failed units' and 'residual UniFi packages' sections above look as expected (a stray nginx/telemetry unit is normal and harmless there).")
            : Inconclusive("One of verify's own checks returned non-zero -- read the sections above (failed units / residual packages) yourself before deciding.");

    private static StepOutcome InterpretDryRun(int exitCode, string output)
        => exitCode == 0 && Contains(output, "accounts removed:")
            ? Go("Dry run completed -- nothing was changed. Review the per-account SKIP / WOULD REMOVE lines above, then Apply for real if they look right.")
            : Inconclusive("Dry run didn't show the expected summary line -- read the output before applying.");

    private static StepOutcome InterpretGeneric(int exitCode, string output, string? mustContain, string[]? badMarkers = null)
    {
        if (badMarkers is not null)
            foreach (var marker in badMarkers)
                if (Contains(output, marker))
                    return NoGo($"Output contains '{marker}' -- do not proceed without reading it.");

        if (exitCode != 0) return NoGo($"Exited with code {exitCode}.");
        if (mustContain is not null && !Contains(output, mustContain))
            return Inconclusive("Completed with exit code 0 but didn't show the expected confirmation text -- worth a quick read before moving on.");
        return Go("Completed successfully.");
    }

    private static StepOutcome Default(int exitCode) => exitCode == 0 ? Go("Completed successfully.") : NoGo($"Exited with code {exitCode}.");

    private static bool Contains(string haystack, string needle) => haystack.Contains(needle, StringComparison.Ordinal);

    private static StepOutcome Go(string reason) => new() { Verdict = OutcomeVerdict.Go, Reason = reason };
    private static StepOutcome NoGo(string reason) => new() { Verdict = OutcomeVerdict.NoGo, Reason = reason };
    private static StepOutcome NoGo(string reason, bool safeAutoRetry) => new() { Verdict = OutcomeVerdict.NoGo, Reason = reason, SafeAutoRetry = safeAutoRetry };
    private static StepOutcome Inconclusive(string reason) => new() { Verdict = OutcomeVerdict.Inconclusive, Reason = reason };
}
