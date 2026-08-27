using System.Text.Json;
using CloudKeyWizard.Models;

namespace CloudKeyWizard.Services;

/// <summary>
/// Confirms the box is a usable starting point before the wizard lets anything destructive run,
/// and separately detects which runbook steps are already applied so re-running the wizard against
/// a partially-converted box resumes correctly instead of assuming a clean slate.
/// All commands here are read-only queries -- nothing in this class changes device state.
/// </summary>
public sealed class PreflightService
{
    private readonly SshSessionService _ssh;

    public PreflightService(SshSessionService ssh) => _ssh = ssh;

    // Mirrors phase1-purge.sh's own $PACKAGES exactly -- the packages that script actually targets
    // for removal. Deliberately NOT the same list as that script's (and this class's own) broader
    // "unifi|ubnt|uos-|ucs-|uid-agent|ulp-go" grep, which is intentionally wider for a human to
    // eyeball residual state after a purge -- it also matches packages the purge script keeps ON
    // PURPOSE (ck-ui/ubnt-tools, the hard FORBIDDEN set) or never targeted at all
    // (ubnt-archive-keyring, ubnt-zram-swap, etc.). Using that broader grep here to decide "is the
    // purge step Done" meant it could never actually report Done -- those deliberately-kept packages
    // always match it, on every device, forever. If phase1-purge.sh's $PACKAGES ever changes,
    // update this list to match (see Scripts/runbook/README.md for the update procedure).
    private const string PurgeTargetPackages =
        "unifi-assets-uckp unifi-assets-uckg2 unifi-email-templates-all python3-unifi-console-protos " +
        "mongodb-server mongodb-clients mongodb-server-core unifi unifi-core unifi-directory " +
        "unifi-identity-update uid-agent ucs-agent uos-agent uos-discovery-client uos ulp-go ustd " +
        "ubnt-systemhub ubnt-unifi-setup ucore-setup-listener";

    public async Task<PreflightResult> RunAsync(CancellationToken ct = default)
    {
        var result = new PreflightResult();
        var checks = result.Checks;

        // 1. Positively identify the hardware -- never trust a user's guess about which model this is.
        var (_, baseFilesOut, _) = await _ssh.RunSimpleAsync(
            "dpkg-query -W -f='${Package}\\n' 'cloudkey-*-apq8053-base-files' 2>/dev/null", ct);
        var baseFilesPkg = baseFilesOut.Split('\n', StringSplitOptions.RemoveEmptyEntries).FirstOrDefault();

        DeviceModel model = baseFilesPkg switch
        {
            "cloudkey-plus-apq8053-base-files" => DeviceModel.CloudKeyGen2Plus,
            "cloudkey-g2-apq8053-base-files" => DeviceModel.CloudKeyGen2,
            _ => DeviceModel.Unknown,
        };

        checks.Add(new PreflightCheckResult
        {
            Name = "Hardware model",
            Passed = model != DeviceModel.Unknown,
            Detail = model switch
            {
                DeviceModel.CloudKeyGen2Plus => "Cloud Key Gen2 Plus (base-files: cloudkey-plus-apq8053-base-files)",
                DeviceModel.CloudKeyGen2 => "Cloud Key Gen2 (base-files: cloudkey-g2-apq8053-base-files)",
                _ => $"Could not identify a known Cloud Key base-files package (saw: '{baseFilesPkg ?? "<none>"}'). " +
                     "This doesn't look like a UCK-G2/UCK-G2-Plus -- stop and confirm before proceeding.",
            },
        });

        // 2. Root / privilege check -- the runbook scripts assume they're run as root.
        var (_, whoamiOut, _) = await _ssh.RunSimpleAsync("id -u", ct);
        var isRoot = whoamiOut.Trim() == "0";
        checks.Add(new PreflightCheckResult
        {
            Name = "Root access",
            Passed = isRoot,
            Detail = isRoot ? "Connected as uid 0 (root)." : "Not connected as root -- the runbook scripts require root.",
        });

        // 3. SSH server genuinely healthy, not just "the one connection we already have happens to work" --
        //    mirrors phase1-purge.sh's own liveness check.
        var (_, sshActiveOut, _) = await _ssh.RunSimpleAsync("systemctl is-active ssh; ss -tlnp 2>/dev/null | grep -q ':22\\b' && echo LISTENING", ct);
        var sshHealthy = sshActiveOut.Contains("active") && sshActiveOut.Contains("LISTENING");
        checks.Add(new PreflightCheckResult
        {
            Name = "SSH service health",
            Passed = sshHealthy,
            Detail = sshHealthy ? "sshd is active and listening on :22." : "sshd does not report both active + listening -- unexpected for a box you're already SSH'd into.",
        });

        // 4. dpkg/apt not already broken -- running a purge on top of broken dpkg state is how a
        //    recoverable box turns into an unrecoverable one. But dpkg --audit's "missing the
        //    md5sums control file" finding is a known, common, harmless false alarm on this
        //    hardware's vendor-built packages (ck-ui, uos*, unifi-* routinely ship without an
        //    md5sums manifest in their .deb control archive -- dpkg still treats them as fully
        //    installed, it just can't verify checksums) -- confirmed against real Cloud Key Gen2
        //    Plus hardware. Only genuinely bad findings should actually block the wizard.
        var (_, auditOut, _) = await _ssh.RunSimpleAsync("dpkg --audit 2>&1", ct);
        var auditTrimmed = auditOut.Trim();
        var dpkgClean = string.IsNullOrWhiteSpace(auditTrimmed);
        var dpkgHasRealIssue = !dpkgClean && ContainsAnyIgnoreCase(auditTrimmed,
            "mess", "half configured", "half installed", "not yet configured",
            "triggers-awaited", "triggers-pending", "unrecoverable");

        checks.Add(new PreflightCheckResult
        {
            Name = "Package manager health",
            Passed = dpkgClean || !dpkgHasRealIssue,
            IsBlocking = dpkgHasRealIssue,
            Detail = dpkgClean
                ? "dpkg --audit reports no issues."
                : dpkgHasRealIssue
                    ? $"dpkg --audit reports issues -- resolve these manually before purging:\n{auditTrimmed}"
                    : $"dpkg --audit flags some vendor packages as missing md5sums metadata -- common and harmless on this hardware's stock images, not a broken package manager:\n{auditTrimmed}",
        });

        // 5. Free space sanity check -- informational only, not blocking.
        var (_, dfOut, _) = await _ssh.RunSimpleAsync("df -h / | tail -n 1", ct);
        checks.Add(new PreflightCheckResult
        {
            Name = "Disk space (/)",
            Passed = true,
            IsBlocking = false,
            Detail = string.IsNullOrWhiteSpace(dfOut) ? "Could not read df output." : dfOut.Trim(),
        });

        result = new PreflightResult { Model = model, BaseFilesPackage = baseFilesPkg, Checks = checks };
        return result;
    }

    private static bool ContainsAnyIgnoreCase(string text, params string[] needles)
        => needles.Any(n => text.Contains(n, StringComparison.OrdinalIgnoreCase));

    /// <summary>
    /// Re-detects which runbook steps are already satisfied on this box and updates each step's
    /// Status/Detail in place. Called after preflight and again any time the wizard reconnects
    /// (including after a manual drop-to-shell session), so state never silently drifts from what's
    /// actually on the device.
    /// </summary>
    public async Task DetectStepStatusAsync(IReadOnlyList<PhaseStep> steps, DeviceModel model, CancellationToken ct = default)
    {
        PhaseStep? Find(string id) => steps.FirstOrDefault(s => s.Id == id);

        // Is the purge actually done? Check only the packages phase1-purge.sh itself targets for
        // removal (PurgeTargetPackages, mirroring the script's own remaining_packages() function) --
        // NOT the broader "unifi|ubnt|..." grep phase1-verify.sh prints for a human to eyeball, which
        // also matches packages the purge deliberately keeps (ck-ui/ubnt-tools) or never targeted in
        // the first place. That broader grep can never come back clean, so using it here meant this
        // step could never actually show Done even after a fully successful purge.
        var (_, purgeResidualOut, _) = await _ssh.RunSimpleAsync(
            $"for p in {PurgeTargetPackages}; do dpkg-query -W -f='${{db:Status-Abbrev}}\\n' \"$p\" 2>/dev/null; done | grep -qE '^(i|rc)' && echo STILL_PRESENT || true", ct);
        var stillHasUnifiPackages = purgeResidualOut.Contains("STILL_PRESENT");

        if (Find("purge") is { } purge)
        {
            purge.Status = stillHasUnifiPackages ? StepStatus.Pending : StepStatus.Done;
            purge.Detail = stillHasUnifiPackages ? "Target UniFi packages still present." : "All target UniFi packages removed (ck-ui and other deliberately-kept packages remain, as intended).";
        }

        // rsync + nfs-common already present?
        var (_, baseToolsOut, _) = await _ssh.RunSimpleAsync("command -v rsync >/dev/null && command -v mount.nfs >/dev/null && echo OK", ct);
        if (Find("base-tools") is { } baseTools)
        {
            var done = baseToolsOut.Contains("OK");
            baseTools.Status = done ? StepStatus.Done : StepStatus.Pending;
            baseTools.Detail = done ? "rsync + nfs-common already installed." : "Not installed yet.";
        }

        // /volume already mounted?
        if (Find("format-volume") is { } formatVolume)
        {
            if (model != DeviceModel.CloudKeyGen2Plus)
            {
                formatVolume.Status = StepStatus.Skipped;
                formatVolume.Detail = "Not applicable -- Gen2 Plus only.";
            }
            else
            {
                var (_, mountOut, _) = await _ssh.RunSimpleAsync("mountpoint -q /volume && echo MOUNTED", ct);
                var mounted = mountOut.Contains("MOUNTED");
                formatVolume.Status = mounted ? StepStatus.Done : StepStatus.Pending;
                formatVolume.Detail = mounted ? "/volume is mounted." : "/volume not mounted yet.";
            }
        }

        // 'cloudkey' admin user already provisioned?
        var (_, userOut, _) = await _ssh.RunSimpleAsync("getent passwd cloudkey >/dev/null && echo EXISTS", ct);
        if (Find("security") is { } security)
        {
            var exists = userOut.Contains("EXISTS");
            security.Status = exists ? StepStatus.Done : StepStatus.Pending;
            security.Detail = exists ? "'cloudkey' user exists." : "'cloudkey' user not created yet.";
        }
    }
}
