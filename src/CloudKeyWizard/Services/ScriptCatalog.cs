using CloudKeyWizard.Models;

namespace CloudKeyWizard.Services;

/// <summary>
/// The ordered Phase-1 runbook (de-Ubiquitizing -> clean, hardened Debian-on-stock-OS). Most steps
/// are built from jnovack/cloudkey at <see cref="BundledScriptProvider.SourceCommitSha"/>; a few
/// (base-tools, format-volume) are app-authored instead -- see each PhaseStep's IsAppAuthored flag
/// and Scripts/runbook/README.md for exactly which. Phases 2-5 (Servarr apps, AutoSSH, WireGuard,
/// Headscale) are optional add-ons layered on top of a finished Phase 1 box and are out of scope
/// here; see the Extras step instead.
/// </summary>
public static class ScriptCatalog
{
    public static List<PhaseStep> BuildSteps() => new()
    {
        new PhaseStep
        {
            Id = "connect",
            Title = "Connect",
            Severity = StepSeverity.Info,
            Description = "Connect to the Cloud Key over SSH. Enable SSH first from the UniFi Console " +
                          "(System Settings -> Advanced -> SSH) if you haven't already -- that step is " +
                          "manual and can't be automated from here.",
        },
        new PhaseStep
        {
            Id = "preflight",
            Title = "Identify & Health Check",
            Severity = StepSeverity.Info,
            Description = "Positively identify the hardware (Gen2 vs Gen2 Plus) from the installed " +
                          "base-files package, confirm SSH/sudo are actually usable, check dpkg/apt " +
                          "aren't already in a broken state, and detect which later steps are already " +
                          "applied. Nothing here changes the device.",
        },
        new PhaseStep
        {
            Id = "purge",
            Title = "Remove UniFi App Layer",
            Severity = StepSeverity.Danger,
            ScriptFileName = "phase1-purge.sh",
            SupportsSimulate = true,
            ConfirmationPhrase = "PURGE",
            Description = "Disables UniFi's systemd units and purges the UniFi application packages " +
                          "(Network, Protect support libs, MongoDB, uos/ucs/uid agents, ...) in small " +
                          "verified batches, checking SSH is still alive after each batch. Never touches " +
                          "ck-ui or ubnt-tools -- both are load-bearing for this hardware and the script " +
                          "hard-refuses to remove them. Cleans up leftover app data and databases, then " +
                          "automatically removes the orphaned UniFi service accounts the purge itself " +
                          "leaves behind (every removal individually guarded -- see the terminal log). " +
                          "Stops itself right before reboot on purpose -- a script can't observe its own " +
                          "box failing to come back from a bad reboot.",
        },
        new PhaseStep
        {
            Id = "reboot",
            Title = "Reboot",
            Severity = StepSeverity.Caution,
            Description = "Reboots the box and waits for SSH to come back. This is the checkpoint the " +
                          "purge step deliberately left for a human (and this wizard) to watch, rather " +
                          "than assuming a reboot will always succeed.",
        },
        new PhaseStep
        {
            Id = "verify",
            Title = "Post-Reboot Verify",
            Severity = StepSeverity.Info,
            ScriptFileName = "phase1-verify.sh",
            Description = "Confirms the box actually rebooted, DHCP renewed, only the expected units " +
                          "failed (a stray nginx/telemetry daemon is normal and harmless here), and no " +
                          "UniFi packages remain outside the deliberately-kept set.",
        },
        new PhaseStep
        {
            Id = "base-tools",
            Title = "Install Base Tooling",
            Severity = StepSeverity.Caution,
            ScriptFileName = "phase1-base-tools.sh",
            IsAppAuthored = true,
            Description = "Installs rsync, nfs-common, and a handful of standard tools a real Linux box " +
                          "is expected to have (curl, wget, git, nano, unzip, ca-certificates, tmux, htop) " +
                          "that aren't in the stock image. Idempotent; safe to re-run.",
        },
        new PhaseStep
        {
            Id = "format-volume",
            Title = "Format & Mount Storage",
            Severity = StepSeverity.Danger,
            ScriptFileName = "phase1-format-mount-volume.sh",
            IsAppAuthored = true,
            RequiresModel = DeviceModel.CloudKeyGen2Plus,
            IsOptional = true,
            SupportsSimulate = true,
            ConfirmationPhrase = "WIPE",
            Description = "Gen2 Plus only (has the internal drive bay). Wipes the selected drive " +
                          "completely and reformats it whole-disk ext4, mounted at /volume via a " +
                          "systemd .mount unit -- NOT /etc/fstab, because this board's vendor bootup-hook " +
                          "framework silently resets fstab to a bare template on every boot. There is no " +
                          "device picker text box in this wizard on purpose: you choose from a live list " +
                          "of the box's actual drives, never a typed path.",
        },
        new PhaseStep
        {
            Id = "security",
            Title = "Harden Access",
            Severity = StepSeverity.Danger,
            ScriptFileName = "phase1-security-setup.sh",
            ConfirmationPhrase = "LOCK",
            IsOptional = true,
            Description = "Optional -- a policy choice, not a required part of the conversion, so it's " +
                          "safe to skip. What it does, plainly: creates an unprivileged 'cloudkey' user " +
                          "with passwordless sudo and key-only SSH, then locks the root/ubnt/cloudkey " +
                          "account passwords entirely -- password login is turned off everywhere after " +
                          "this, SSH keys become the only way in. Bootstraps the new user's " +
                          "authorized_keys from whatever key you're already connected with (or a key you " +
                          "supply), specifically so this step can never lock you out. If you're not sure " +
                          "you want this, Skip is a completely normal choice -- you can always come back " +
                          "and run it later.",
        },
        new PhaseStep
        {
            Id = "extras",
            Title = "Optional Extras",
            Severity = StepSeverity.Info,
            IsOptional = true,
            Description = "Independent, individually-optional adjustments on top of the finished box -- " +
                          "each has its own Run control below, in no particular order and with no bearing " +
                          "on anything already applied.",
        },
        new PhaseStep
        {
            Id = "finish",
            Title = "Finish & Summary",
            Severity = StepSeverity.Info,
            Description = "Final state summary. From here the box is a hardened, de-Ubiquitized Cloud " +
                          "Key you can treat as a normal small ARM server.",
        },
    };
}
