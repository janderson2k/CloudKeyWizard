namespace CloudKeyWizard;

/// <summary>Version/changelog for CloudKeyWizard.exe itself -- versioned independently from
/// FDT.Scout (see tools/fdtscout/version.go), a separate program with its own release cadence.
/// Bump both the version and add a ChangelogEntry whenever a real user-facing change ships;
/// mirror the same entry into QUE.MD's "What's New" (that's the detailed session log, this is the
/// short in-app version).</summary>
public static class AppVersion
{
    public const string Version = "2.14.0";
    public const string BuildDate = "2026-08-28";

    /// <summary>The FDT.Scout version actually bundled/embedded in THIS build (Scripts/fdtscout/
    /// fdtscout-arm64) -- must be bumped by hand alongside tools/fdtscout/version.go's own Version
    /// const whenever that binary is rebuilt and re-embedded. Nothing enforces this automatically;
    /// it's a plain string literal precisely so a mismatch is a copy-paste bug, not a build failure
    /// that would block shipping an otherwise-unrelated fix. Used by MainViewModel's live
    /// version-check against an already-converted device's installed FDT.Scout (fdtscout -version
    /// over SSH) to tell the operator whether re-running that Extra would actually install
    /// something newer.</summary>
    public const string BundledFdtScoutVersion = "2.1.3";

    public sealed record ChangelogEntry(string Version, string Date, string[] Notes);

    public static readonly ChangelogEntry[] Changelog =
    {
        new("2.14.0", "2026-08-28", new[]
        {
            "Extras: connecting to an already-set-up device now checks the live-installed FDT.Scout version against what this Wizard build actually bundles -- a yellow \"Update available\" badge appears next to its title, its Run button changes to Update, and the detail line shows both versions, so it's finally visible when re-running that Extra would install something newer instead of silently reinstalling the same version. Bundled FDT.Scout upgraded to 2.1.3, which adds the -version flag this check needs.",
        }),
        new("2.13.0", "2026-08-27", new[]
        {
            "Bundled FDT.Scout console upgraded to 2.1.2: fixed a real bug where proactive Pushbullet alerts (disk space, service down, login lockout, IP change, the daily digest) carried no device identifier at all -- if you run this on more than one Cloud Key against the same Pushbullet account, every alert looked identical with no way to tell which device sent it. Every alert's title is now tagged with that device's own callsign.",
        }),
        new("2.12.1", "2026-08-25", new[]
        {
            "Fixed a real inconsistency in the About feature list added last version: WireGuard's line was worded to lead with \"FullDuplexTech,\" but the 27-original/10-jnovack total right below it only works if WireGuard counts as jnovack-sourced (which is the correct call -- the 7 scripts and systemd units that actually do the VPN work are jnovack's, verbatim; only the orchestrator wiring them together is original). Reworded that one line so it matches the total instead of contradicting it.",
        }),
        new("2.12.0", "2026-08-25", new[]
        {
            "About: added a full feature-by-feature breakdown (all 37 features across the core wizard steps, Optional Extras, and FDT.Scout's tabs) with each one's actual source noted -- FullDuplexTech (original to this project) or jnovack/cloudkey -- plus a short note on why this app exists from its author's own perspective (getting FDT.Scout running), since that reason may not match yours. Presentation only, no functional change.",
        }),
        new("2.11.3", "2026-08-25", new[]
        {
            "The first-run disclaimer overlay now also credits the FullDuplexTech.com article that inspired this whole project, alongside the license line added last version.",
        }),
        new("2.11.2", "2026-08-25", new[]
        {
            "The first-run disclaimer overlay now also names jnovack/cloudkey's license (CC BY-NC-SA 4.0) and links to it directly, instead of that only being in Help -> About -- this is the one screen guaranteed to be seen before anything runs.",
        }),
        new("2.11.1", "2026-08-25", new[]
        {
            "About: now explicitly names jnovack/cloudkey's actual license (CC BY-NC-SA 4.0) and links to it, states this app is given away for free on a personal, non-commercial basis, and adds a LICENSE-jnovack-cloudkey.md alongside the bundled runbook scripts it governs -- previously only credited the project by name without stating the license itself. Bundled FDT.Scout console upgraded to 2.1.1 with the same About-text change.",
        }),
        new("2.11.0", "2026-08-25", new[]
        {
            "Fixed a real bug found on a live run: the \"Remove UniFi App Layer\" step could never actually show as Done, even after a fully successful purge -- it was re-checking completeness with a grep broad enough to match packages the purge deliberately keeps (like ck-ui) or never targeted in the first place, so it always looked incomplete. Now checks only the packages the purge script actually targets.",
            "Fixed a real bug found on the same live run: the Reboot step's \"wait for reconnect\" could lose a race and fall straight through without actually waiting, because it only ever polled for the box coming back up -- and the box is often still fully up for a moment after triggering reboot, so the very first check could reconnect to the not-yet-rebooted box and falsely report success instantly. It now confirms the box's SSH port genuinely goes down first, then waits for it to come back -- a real down-then-up check instead of one that could trivially pass the whole time.",
        }),
        new("2.10.0", "2026-08-25", new[]
        {
            "Remove UniFi App Layer: a known-safe purge failure (a later batch trying to purge a package an earlier batch already removed as a dependency-cascade side effect -- apt errors \"Unable to locate package\" since it's already gone) is now auto-retried once instead of just being explained and left for you to click Apply again. The purge script itself recomputes what's actually left to do on every run, including its own Step 0 safety gate, so retrying it is exactly as safe as running it fresh. A genuine failure (forbidden package, liveness check failure, unexpected simulate mismatch) still stops and waits for you, same as before.",
        }),
        new("2.9.0", "2026-08-24", new[]
        {
            "Removed the \"Self-hosted Headscale server\" placeholder from Optional Extras -- it targeted a separate VPS this app has no relationship with, and there's no real per-deployment config UI for it planned. The Tailscale Extra's own description still covers pointing at a self-hosted Headscale server manually, for anyone who wants that.",
            "Bundled FDT.Scout console upgraded to 2.1.0: new Docker tab -- a general-purpose container manager (install/verify, full container lifecycle control, a run-from-image form, log viewing, and an explicit storage-location picker that can move Docker's data to /volume). Separate from any single app's own Docker use -- Home Assistant's container, if installed, still shows on FDT.Scout's Apps tab as before.",
        }),
        new("2.8.0", "2026-08-24", new[]
        {
            "New Backup category in Optional Extras: a Restic backup server (rest-server), so this device can be a real backup destination for your other machines. Backups are encrypted on the client side before they arrive here -- this device never sees your actual data. Installs the real official upstream binary, checksum-verified. Uses /volume for backup data if that's mounted.",
        }),
        new("2.7.1", "2026-08-24", new[]
        {
            "Bundled FDT.Scout console upgraded to 2.0.1: fixed a real bug where the device replied to its own Pushbullet messages in a loop, unrecognized commands are now silently ignored as intended, and the IP range scan can now target a custom start/end address range instead of only the device's own local subnet.",
        }),
        new("2.7.0", "2026-08-24", new[]
        {
            "Bundled FDT.Scout console upgraded to 2.0.0, the biggest single addition to it yet: a new Monitoring tab (watch any host's uptime/latency over time, an automatic WAN speed test, public IP tracking with an optional Dynamic DNS updater), on-demand local network + open-port scanning, a passive LAN device list with Wake-on-LAN, log aggregation onto real storage with 90-day retention, real-cron scheduled tasks, and a two-way Pushbullet command channel (text the device's callsign to get status back, with a confirm step before any action runs). See FDT.Scout's own About tab for the full list.",
        }),
        new("2.6.0", "2026-08-24", new[]
        {
            "The masked-password field is CONFIRMED fixed -- live-tested successfully. Root cause was a WPF quirk in how the field's keystroke handler attached itself; the diagnostic build's own tracing pinpointed it precisely, and it's now been watched working end-to-end (password entered, Run succeeded, FDT.Scout came up and answered).",
            "Fixed a real, separate bug the above fix exposed: an Extra's secret Params (e.g. FDT.Scout's admin password) were echoed in PLAIN TEXT in the terminal pane's command-echo line -- every other password-touching flow in this app already redacts, this one didn't. If you ran the FDT.Scout Extra on a build before this one, change that password now, since it was visible in your own terminal history.",
            "Removed the temporary [debug]-prefixed diagnostic tracing added while chasing the above -- no longer needed now that it's confirmed fixed.",
        }),
        new("2.5.2", "2026-08-24", new[]
        {
            "The masked-password field is STILL broken as of 2.5.1, confirmed live again. Diagnostic-only build, no new fix attempt: swapped the guard that decides which password box a keystroke belongs to over to a more direct WPF API, added a trace right before that decision is made so the next report shows exactly what it saw, and mirrored the \"Enter a value... first\" diagnostic into the terminal text itself (not just the on-screen label) since a plain text copy turned out more reliable than a screenshot.",
        }),
        new("2.5.1", "2026-08-24", new[]
        {
            "The masked-password field is STILL broken as of 2.5.0, confirmed live again. Diagnostic-only build, no new fix attempt: the \"Enter a value... first\" message now also shows how many times the password keystroke handler actually fired this session (\"pwFires=\", \"pwMatched=\"), directly in that one line, so a single screenshot of it pinpoints exactly which of three distinct failure points is the real one.",
        }),
        new("2.5.0", "2026-08-24", new[]
        {
            "Found the real cause of the masked-password bug from the 2.4.1 diagnostic build's own trace: the keystroke handler was never actually attaching itself in the first place (a WPF quirk where a change-notification callback doesn't reliably fire when a field's starting value equals its declared default, which was true here). All three previous \"fixes\" were patching code that, as a result, never actually ran. Rewired it to attach unconditionally instead of depending on that notification.",
        }),
        new("2.4.1", "2026-08-24", new[]
        {
            "The masked-password field is STILL broken as of 2.4.0, confirmed live again -- diagnostic build only, no new fix attempt. Added temporary [debug] lines to the terminal pane that trace exactly what happens when you type into the field and click Run, so the next report is hard evidence instead of another guess. No user-facing behavior change otherwise.",
        }),
        new("2.4.0", "2026-08-24", new[]
        {
            "Fixed the masked-password field for real this time (confirmed live it was still broken after the last two attempts). Actual root cause: every Extra Param's edit box (plain text, dropdown, masked password, revealed password, multiline) was always present at once in the same layout, just hidden -- so the Timezone dropdown's own binding, empty for every non-dropdown field, was silently resetting the password field back to nothing the moment you typed into it. Each Param now only builds the one control it actually needs, so there's nothing left to interfere.",
        }),
        new("2.3.0", "2026-08-24", new[]
        {
            "Fixed the masked-password field for real this time -- the 2.2.0 fix above was a genuine bug fix but turned out to be incomplete; a second, separate bug in the same helper (using SetValue instead of SetCurrentValue on a bound property, which silently disconnects the binding) was still blocking it. Confirmed both bugs together account for the reported symptom.",
            "Fixed the new Timezone dropdown (and any other dropdown, including the \"Previously connected\" host list) rendering completely blank/unreadable -- the ComboBox control itself only had partial styling; it now gets the same full-template treatment ComboBoxItem already had, so the selected value actually displays.",
        }),
        new("2.2.0", "2026-08-24", new[]
        {
            "Fixed a real bug: the masked password field on Extras (e.g. FDT.Scout's admin password) never actually saved what you typed while hidden -- Run would fail with \"enter a value first\" even after typing one, and it only worked once you clicked Show. Typing while hidden now works correctly.",
            "\"Set timezone to UTC\" is now just \"Set timezone\", with a dropdown covering every major zone -- UTC is still the pre-selected default.",
            "Bundled FDT.Scout console upgraded to 1.9.0: its new Settings tab also gained a Timezone dropdown, populated from the device's own real zone list.",
        }),
        new("2.1.0", "2026-08-24", new[]
        {
            "Remove UniFi App Layer: Apply now shows a \"Batch N of 5\" progress indicator while the purge streams, instead of just raw scrolling output.",
            "Bundled FDT.Scout console upgraded to 1.8.0: the front panel's Front Panel tab is overhauled -- your own screens, each with up to 4 lines picked from hostname/IP/time/CPU/RAM/disk/uptime/custom text, a live preview, an on/off toggle, and a configurable cycle interval, all editable right on the page. A new Settings tab consolidates hostname, IP/DHCP, DNS, and NTP (time sync) in one place -- DNS and NTP are newly controllable from the dashboard at all.",
        }),
        new("2.0.0", "2026-08-24", new[]
        {
            "The front panel display now actually works -- it shows the hostname and IP, drawn directly by the bundled FDT.Scout console rather than depending on a separate app that was never rendering correctly on this hardware. \"Replace Front-Panel LCD App\" is no longer a separate wizard step -- it just works once FDT.Scout is installed.",
            "Install Base Tooling now also installs curl, wget, git, nano, unzip, tmux, and htop -- a more complete general-purpose Linux box, not just the bare minimum later steps needed.",
            "Remove Orphaned Accounts is no longer its own separate step -- it now runs automatically right after removing the UniFi app layer, since that's really the second half of the same cleanup.",
            "Remove UniFi App Layer: the confirmation screen now shows a live preview of exactly which packages on your device are about to be removed, instead of just generic prose. A snapshot of your package list is also captured beforehand as a safety record.",
            "Harden Access now explains what it actually does directly on the page, so Skip vs. Apply is an informed choice either way.",
            "Extras: password and other sensitive fields (like the FDT.Scout admin password and a pasted WireGuard config) are now masked by default, with a Show/Hide toggle -- previously shown in fully visible plain text.",
            "About: corrected authorship.",
        }),
        new("1.9.0", "2026-08-23", new[]
        {
            "Bundled FDT.Scout console upgraded to 1.6.0: process Kill button, a System specs block on the Health tab, and a real (kernel-log-confirmed) diagnosis + one-click attempted fix for the front-panel-shows-only-the-UniFi-logo issue -- AppArmor blocking cloudkey's framebuffer access, not a permissions or ck-ui conflict as first guessed.",
            "About sections (both apps) corrected: built by Jay Anderson of FullDuplexTech.com; jnovack/cloudkey credited as the upstream source this app automates, not the author.",
        }),
        new("1.8.0", "2026-08-23", new[]
        {
            "Bundled FDT.Scout console upgraded to 1.5.0: Health tab now has a top-style process table (CPU/MEM/RSS per process, load average, uptime), auto-refreshing every 3 seconds while open.",
        }),
        new("1.7.0", "2026-08-23", new[]
        {
            "Bundled FDT.Scout console upgraded to 1.4.0: its Apps tab can now actually install things (full parity with this app's own Optional Extras, including WireGuard's full config form), plus USB drive mount/browse and static IP/DHCP control with a real 5-minute confirm-or-auto-revert safety mechanism. See FDT.Scout's own About tab for the full list.",
        }),
        new("1.6.0", "2026-08-23", new[]
        {
            "Added a shared on-device state file (Services/DeviceStateStore.cs, fdtscout-state.json under the existing SSH audit-trail directory) that FDT.Scout also reads/writes -- Extras' status now syncs both ways between the two apps, and a different copy of CloudKey Wizard connecting to an already-converted Cloud Key picks up real state instead of starting blank.",
            "FDT.Scout (bundled web console): added an Apps-tab-visible auth log with login lockout, network throughput on the Health tab, and a configurable HTTPS port + port-80 redirect toggle on the Certificates tab.",
        }),
        new("1.5.0", "2026-08-23", new[]
        {
            "Fixed re-running the FDT.Scout Extra to update an already-installed console: uploads now go to a temp path and atomically rename into place (was writing directly over the running binary, which Linux refuses), and the installer now explicitly restarts the service instead of enable --now, which silently no-ops on an already-active unit.",
            "Fixed the \"Previously connected\" host dropdown being hard to read in Dark Mode: gave ComboBoxItem a full ControlTemplate override (a plain Style's property setters weren't enough to fully override the default template's own hover/selection visuals).",
            "FDT.Scout: sessions now idle-timeout after 5 minutes of inactivity (sliding, plus a 24-hour absolute cap), with the dashboard auto-redirecting to login on expiry instead of failing silently.",
        }),
        new("1.4.0", "2026-08-23", new[]
        {
            "Added an About window (Help menu) with version, references/resources, and the full disclaimer -- previously just a short MessageBox.",
            "Added a View Changelog window (Help menu) showing this list in-app.",
            "Added a liability disclaimer (\"use at your own risk, informational purposes only, not affiliated with Ubiquiti\") to the first-run warning overlay, alongside the existing technical-risk warning.",
            "FDT.Scout: added an Apps tab (status + start/stop/enable/disable for anything CloudKey Wizard's Optional Extras can install) and its own About tab; hardened hostname changes (three redundant methods instead of relying on hostnamectl alone) plus real diagnostics for the front-panel-not-updating case.",
        }),
        new("1.3.0", "2026-08-23", new[]
        {
            "Fixed terminal pane copy/paste -- the log was a non-selectable TextBlock list, replaced with a read-only RichTextBox.",
            "Built FDT.Scout: a real password-gated HTTPS web console for the Cloud Key (login, web terminal, TLS cert management, front-panel hostname control, health metrics), shipped as a new Optional Extra.",
            "WireGuard fail-closed egress shipped as a real, fully-functional Extra (previously an explained placeholder).",
        }),
        new("1.2.0", "2026-08-23", new[]
        {
            "AutoSSH rescue tunnel, the Servarr media stack (NZBGet/Sonarr/Radarr/Prowlarr), and Tailscale client all shipped as real Extras.",
            "Extras model extended: multi-field Params, BundledScriptFileName, PrerequisiteBundledScriptFileName, SiblingBundledScriptFileNames.",
        }),
        new("1.1.0", "2026-08-23", new[]
        {
            "Optional Extras step + rebuilt Finish/Summary + password-change utility.",
            "Dark/Light theme toggle, known-hosts (Previously connected) list with export/import.",
        }),
        new("1.0.0", "2026-08-23", new[]
        {
            "Initial build: three-pane wizard, full Phase 1 runbook automation, first live hardware test against a Cloud Key Gen2 Plus.",
        }),
    };

    public const string AboutText =
        "Built by Jay Anderson of FullDuplexTech.com (https://fullduplextech.com) -- the original " +
        "article that started this whole project is at https://fullduplextech.com/turn-unifi-cloud-key-gen-2-into-a-headless-linux-server/.\n\n" +
        "CloudKey Wizard converts a UniFi Cloud Key Gen2/Gen2 Plus into a hardened Debian server " +
        "by driving jnovack/cloudkey's runbook over SSH.\n\n" +
        "This tool is provided for informational and convenience purposes only, as-is, with no " +
        "warranty of any kind -- use at your own risk. It is not affiliated with or endorsed by " +
        "Ubiquiti Inc. Every command it runs is streamed live into the terminal pane so nothing " +
        "happens off-screen; Danger-labeled steps require a typed confirmation. This app is given " +
        "away for free, built and distributed on a personal, non-commercial basis.\n\n" +
        "References and resources:\n" +
        "- jnovack/cloudkey (https://github.com/jnovack/cloudkey) -- not the author of this app, but the source of the Phase 1-5 runbook this app automates. Every bundled script is pinned to a specific upstream commit; see Scripts/runbook/README.md. Full credit to that project for the underlying de-Ubiquitizing research and tooling. Licensed under CC BY-NC-SA 4.0 (https://creativecommons.org/licenses/by-nc-sa/4.0/) -- the bundled scripts sourced verbatim from it remain under those terms; see Scripts/runbook/LICENSE-jnovack-cloudkey.md.\n\n" +
        "FDT.Scout, the optional web console this app can install onto the device, is app-authored " +
        "(not from jnovack/cloudkey) -- also built by the same author, source at tools/fdtscout/ in this repo.\n\n" +
        "Why this app exists: for its author, the real goal was always getting FDT.Scout -- the web " +
        "console below -- running on this hardware. Everything else here (the Phase 1 conversion, the " +
        "other Optional Extras) exists to get a Cloud Key to a clean, hardened state where that's " +
        "possible. Your own reason for running this might be completely different -- maybe you just " +
        "want a de-Ubiquitized Debian box, or Plex, or the WireGuard kill-switch, and have no interest " +
        "in FDT.Scout at all -- which is exactly why every item below is a separate, optional, " +
        "individually-run step rather than one all-or-nothing install.\n\n" +
        "Everything this app can do, and where it's actually from (FullDuplexTech = original to this " +
        "project; jnovack/cloudkey = that project's own content, verbatim or largely so -- see " +
        "Scripts/runbook/README.md for exactly which):\n\n" +
        "Core wizard steps:\n" +
        "- Connect (FullDuplexTech)\n" +
        "- Identify & Health Check (FullDuplexTech)\n" +
        "- Remove UniFi App Layer (jnovack/cloudkey)\n" +
        "- Reboot & Wait for Reconnect (FullDuplexTech)\n" +
        "- Post-Reboot Verify (jnovack/cloudkey)\n" +
        "- Install Base Tooling (FullDuplexTech)\n" +
        "- Format & Mount Storage (FullDuplexTech)\n" +
        "- Harden Access (jnovack/cloudkey)\n" +
        "- Optional Extras (FullDuplexTech) -- the 17 items below\n" +
        "- Finish & Summary (FullDuplexTech)\n\n" +
        "Optional Extras:\n" +
        "- FDT.Scout web console (FullDuplexTech)\n" +
        "- Set timezone (FullDuplexTech)\n" +
        "- Unattended upgrades (FullDuplexTech)\n" +
        "- fail2ban (FullDuplexTech)\n" +
        "- Apt upgrade now (FullDuplexTech)\n" +
        "- Set hostname (FullDuplexTech)\n" +
        "- OS-release check (FullDuplexTech)\n" +
        "- Plex (FullDuplexTech installer; Plex Media Server itself is third-party)\n" +
        "- NZBGet (jnovack/cloudkey)\n" +
        "- Sonarr (jnovack/cloudkey)\n" +
        "- Radarr (jnovack/cloudkey)\n" +
        "- Prowlarr (jnovack/cloudkey)\n" +
        "- Home Assistant (FullDuplexTech installer; Home Assistant itself is third-party)\n" +
        "- AutoSSH rescue tunnel (jnovack/cloudkey)\n" +
        "- Tailscale client (jnovack/cloudkey)\n" +
        "- WireGuard fail-closed egress (jnovack/cloudkey -- the 7 scripts and systemd units that actually do the work are theirs, verbatim; a FullDuplexTech orchestrator just wires them together and fills in your config)\n" +
        "- Restic backup server (FullDuplexTech installer; Restic itself is third-party)\n\n" +
        "FDT.Scout web console tabs (all FullDuplexTech):\n" +
        "- Terminal, Health, Monitoring, Apps, Docker, Users, Certificates, Front Panel, Settings, About\n\n" +
        "37 features total: 27 original to this project, 10 sourced from jnovack/cloudkey.";
}
