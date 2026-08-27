# CloudKey Wizard

**[⬇ Download the latest release](https://github.com/janderson2k/CloudKeyWizard/releases/latest)** —
a single self-contained `CloudKeyWizard.exe`, no install needed. Read the in-app first-run
disclaimer before using it against real hardware.

Built by [Jay Anderson](https://fullduplextech.com) of FullDuplexTech.com — a portable Windows
(WPF) wizard that converts a Ubiquiti UniFi Cloud Key Gen2 / Gen2 Plus into a clean, hardened
Debian server, by orchestrating the [jnovack/cloudkey](https://github.com/jnovack/cloudkey) Phase 1
"de-Ubiquitizing" runbook over SSH. jnovack/cloudkey is not the author of this app — it's the
upstream project whose research and scripts this app automates; full credit to it for that work.
The original idea for converting this hardware came from [an article on FullDuplexTech.com](https://fullduplextech.com/turn-unifi-cloud-key-gen-2-into-a-headless-linux-server/).
FullDuplexTech.com is a personal, non-commercial site — this app is given away for free, not sold.

jnovack/cloudkey is licensed under [CC BY-NC-SA 4.0](https://creativecommons.org/licenses/by-nc-sa/4.0/)
(Attribution-NonCommercial-ShareAlike); the runbook scripts this app bundles verbatim from that
project (see `src/CloudKeyWizard/Scripts/runbook/README.md` and `LICENSE-jnovack-cloudkey.md` in
that same folder) remain under those terms, unmodified.

**Picking this project back up?** See [QUE.MD](QUE.MD) first — it's the running queue of
discussed-but-not-built features, a map of what exists today and where it lives, and a
session-by-session changelog. Keep it up to date as the source of truth for "where did we leave off."

Version 2.0.0 (also shown in-app: Help → About / View Changelog). FDT.Scout, the optional web
console, is versioned independently (currently 1.7.0, its own About tab). Provided for
informational and convenience purposes only, as-is, no warranty — use at your own risk; not
affiliated with or endorsed by Ubiquiti Inc.

## What this is (and isn't)

- **Not a firmware flasher.** No recovery mode, no factory reset, no firmware re-upload. The
  runbook this app drives starts from a normal, currently-running UniFi OS console with SSH
  enabled — that one step (System Settings → Advanced → SSH) is manual and happens on the device
  itself.
- **Not a Debian-version upgrader.** It never runs `apt full-upgrade`/`dist-upgrade` across Debian
  releases. It strips the UniFi application layer in place and leaves the vendor base OS/kernel
  alone, because this hardware depends on a vendor bootup-hook framework to boot at all.
- **Phase 1, plus an "Optional Extras" step.** Automates the Phase 1 conversion (purge → verify →
  base tools → optional storage → harden access, with account cleanup running automatically as
  part of the purge itself). Extras are independent,
  individually-run adjustments in two flavors: app-authored (timezone, hostname, unattended
  updates, fail2ban — clearly labeled as not from jnovack/cloudkey) and real upstream jnovack
  scripts with actual config UI (Servarr media stack: NZBGet/Sonarr/Radarr/Prowlarr; AutoSSH rescue
  tunnel; Tailscale client; WireGuard fail-closed egress — pins chosen services into an isolated
  network namespace whose only route out is a WireGuard tunnel, built from the 7 bundled Phase 4
  scripts plus the exact systemd units documented on the upstream wiki), same hash/provenance
  treatment as Phase 1. Self-hosted Headscale is the one remaining placeholder — its server script
  targets a separate VPS with Docker Swarm + Traefik already running, not the Cloud Key at all, so
  there's nothing for an SSH-only tool like this to drive there.
  Also in General: **FDT.Scout**, an app-authored (not jnovack/cloudkey) password-gated HTTPS web
  console for the device — user accounts with login history and lockout after repeated failures, a
  real web terminal (a genuine PTY over WebSocket, not a command box), TLS certificate
  generate/install plus a configurable HTTPS port and an 80→HTTPS redirect toggle, the front-panel
  display itself (hostname + IP, drawn directly to the framebuffer, updates live when you change
  the hostname), a top-style process view with a Kill button, System specs and 7-day health charts
  including network throughput, USB drive mount/unmount with a read-only file browser, static
  IP/DHCP control (a change is provisional for 5 minutes — confirm it or it reverts automatically),
  and an Apps tab that can now actually install things — full parity with the Extras above, reusing
  the exact same bundled scripts rather than a second copy. An About tab rounds it out. Source at
  `tools/fdtscout/`, cross-compiled to a single
  linux/arm64 binary and embedded the same way the runbook scripts are — no compiler needed on the
  device. Listed first and recommended by default, but still requires the same one Run click every
  Extra requires; it opens a real root-capable HTTPS listener on your network the moment it
  finishes, gated only by the password you set, so it's flagged plainly rather than treated as just
  another checkbox.
- **Finish & Summary** shows a live report (host, model, auth method, every step's final status)
  and a password-change utility (`security-unlock.sh` → `chpasswd` → `security-lock.sh`) for
  setting a new password on `root`/`cloudkey` even after Harden Access has disabled password login.

## Safety model

- **Fully self-contained — no runtime GitHub dependency.** Every script this app runs is bundled
  directly into the exe at build time (`Services/BundledScriptProvider.cs`, sourced verbatim from
  jnovack/cloudkey at a pinned commit — see `Scripts/runbook/README.md`), SHA-256 hashed at read
  time, with the hash and upstream reference URL shown in the UI before you run anything. The SSH
  connection to the Cloud Key is the *only* network activity this app performs — one exception:
  the optional LCD-replacement step calls out to GitHub **from the Cloud Key itself**, not from
  this app, to download jnovack/cloudkey's prebuilt binary (unavoidable; it's a compiled release
  asset, not something this app can bundle as source).
- **Full transparency.** Every command run — by the wizard or by you in the "drop to shell" pane —
  streams into the terminal log in real time. Nothing happens off-screen.
- **Danger-tier steps require typed confirmation**, not just a click: purging the UniFi stack,
  wiping/formatting a drive, and locking password login all require typing an exact phrase (or,
  for the drive wipe, the exact device path) before Apply is enabled.
- **The drive-wipe step never accepts a typed device path.** It queries `lsblk` on the box live and
  offers only what's actually attached, in a picker.
- **Preflight before anything destructive.** The wizard positively identifies the hardware (Gen2 vs
  Gen2 Plus, from the installed base-files package — never trusted from user input), confirms
  SSH/root/dpkg health, and detects which steps are already applied so re-running against a
  partially-converted box resumes correctly.
- **"Simulate" is a real no-op**, not a fake preview: for purge/account-cleanup/format-volume it
  invokes the actual upstream script in the exact non-interactive mode its own authors designed for
  safe dry runs (no `-y`, no stdin, so an interactive confirm prompt fails closed).

This tool takes the device outside Ubiquiti's supported configuration and provides no factory-revert
path. Read the in-app disclaimer before using it.

## Project layout

```
src/CloudKeyWizard/
  Models/            PhaseStep, ExtraItem, KnownHost, enums, connection/preflight/device/terminal-line records
  Scripts/runbook/    the actual runbook scripts, embedded into the exe at build time (see its README)
  Services/
    ScriptCatalog.cs         the ordered Phase 1 step list
    ExtraCatalog.cs          the Optional Extras list (General/MediaServer/RemoteAccess)
    BundledScriptProvider.cs reads + hashes runbook scripts from this exe's embedded resources
    SshSessionService.cs     SSH/SFTP session, streaming exec, interactive shell, reboot-poll
    PreflightService.cs      hardware ID + health checks + idempotent step-status detection
    BlockDeviceService.cs    `lsblk -J` parsing for the storage-wipe device picker, safe-device heuristic
    OutcomeInterpreter.cs    turns a step's captured output into a GO/NO-GO/INCONCLUSIVE banner
    KnownHostsStore.cs       loads/saves the local known-hosts list (JSON, never a password)
    AppSettingsStore.cs      persists app-wide preferences (currently just the theme choice)
  Themes/              DarkPalette.xaml / LightPalette.xaml -- swapped at runtime via View > Dark Mode
  ViewModels/        MainViewModel (all wizard state/flow) + hand-rolled ICommand/ObservableObject
  Converters/         status/severity → glyph/brush/visibility converters for the XAML
  MainWindow.xaml(.cs) the three-pane wizard UI (left: step nav, middle: current step, right: terminal)
scripts/Publish.ps1   builds the portable single-file exe
QUE.MD               queue / feature map / changelog -- read this first when resuming
```

## Building / running

Requires the .NET 8 SDK.

```
dotnet build src/CloudKeyWizard/CloudKeyWizard.csproj
```

## Publishing the portable exe

```
pwsh -File scripts/Publish.ps1
```

Produces `publish/CloudKeyWizard.exe` — a single self-contained win-x64 file (~70MB; WPF can't be
safely trimmed, so this isn't a tiny console-tool-sized exe). No installer, no registry writes, no
admin rights, no separate .NET runtime needed on the target machine. Copy it anywhere and run it.

Do **not** add `-p:InvariantGlobalization=true` to the publish flags — WPF's data-binding engine
needs real ICU culture data and throws at startup without it (confirmed by hitting exactly that
crash while building this).

## Updating the bundled scripts

The scripts in `Scripts/runbook/` are pinned to a specific jnovack/cloudkey commit rather than
floating on `main`, so an upstream change can't silently alter what runs on someone's hardware
between one build of this app and the next — see `Scripts/runbook/README.md` for exactly which
commit and how to bump it. Re-test each step's Simulate mode against a real device before trusting
Apply after any update.
