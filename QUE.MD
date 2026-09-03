# CloudKey Wizard — Queue, Feature Map & What's New

This file is the resume point for this project. Three sections: the **Queue** (things discussed
but not built yet), the **Feature Map** (what exists today and where it lives), and **What's New**
(a session-by-session changelog). Update this file whenever the queue changes or a session ships
something — it's the source of truth for "where did we leave off," not the private memory file.

---

## Queue

Ordered roughly as discussed, not by priority. **Queue mode re-activated 2026-08-24** ("Lets que
that up. (Que mode now)") -- discussion only below, nothing built until the user says to build it.

**Bug: "Remove UniFi App Layer" step never shows Done, even after a genuinely successful purge: built.**
See What's New (`publish-v41`, CloudKeyWizard 2.11.0) for what shipped -- design record kept below.
(Found live 2026-08-25, real hardware run against `10.0.100.139`.) The step's circle
stays a hollow Pending indicator with detail text "UniFi packages still present." forever, even
though the terminal log for that exact run shows the purge completed cleanly (all target batches
purged or correctly skipped-as-already-done, `phase1-verify.sh` confirmed only the deliberately-kept
packages remain) and `RunSelectedStepAsync` did set `Status = Done` / `"Applied successfully."` right
after the script exited 0.
- **Root cause**: `PreflightService.DetectStepStatusAsync` (`Services/PreflightService.cs:122-130`)
  re-verifies the purge step using the exact same grep `phase1-verify.sh` prints for a *human* to
  eyeball -- `dpkg -l | grep -iE 'unifi|ubnt|uos-|ucs-|uid-agent|ulp-go'` -- and treats ANY match as
  "purge not done." But that grep is deliberately broad in `verify.sh` (it's informational: "here's
  what's left, confirm only the deliberately-kept ones remain"), and it will always match on a
  perfectly successful purge, because several of the packages it catches are things `phase1-purge.sh`
  never targets on purpose: `ck-ui`/`ubnt-tools` are the hard-coded `FORBIDDEN` set the script refuses
  to touch under any circumstance, and `jq`/`libjq1`/`ubnt-archive-keyring`/`ubnt-binmecpp`/
  `ubnt-disk-smart-mon`/`ubnt-ucp4cpp`/`ubnt-zram-swap` aren't in the script's own `$PACKAGES` removal
  list at all.
- **Sequence that produces the bug, every time, on every device**: purge succeeds, `RunSelectedStepAsync`
  sets the step to Done -> its own success path immediately calls `_preflight.DetectStepStatusAsync`
  to resync all steps against live device state (this is real, useful behavior for resuming a
  half-finished wizard run -- not the bug) -> that resync's purge check sees `ck-ui` (correctly still
  installed, exactly as intended) -> matches the broad grep -> **overwrites the step it just marked
  Done back to Pending**, immediately, unconditionally, every single time.
- **Fix shipped**: added `PreflightService.PurgeTargetPackages`, a duplicate of `phase1-purge.sh`'s
  own `$PACKAGES` list, and changed the check to `for p in $PurgeTargetPackages; do dpkg-query -W
  -f='${db:Status-Abbrev}' "$p" 2>/dev/null; done | grep -qE '^(i|rc)'` -- same prefix-matching logic
  the script's own `remaining_packages()` uses, checking only what the purge actually targets instead
  of `verify.sh`'s intentionally-broader human-facing grep. "Purge considered Done" now means the
  same thing on both sides.
- **Not a display glitch, not a race condition** -- confirmed by reading the actual code path, not
  guessed: it was a straightforward "two different completeness checks measuring two different
  things" bug, and it would have reproduced on literally every device this app ever purged.

**Bug: Reboot step's "wait for reconnect" can lose the race and not actually wait at all: built.**
See What's New (`publish-v41`) for what shipped -- design record kept below. (Found live 2026-08-25,
same session as the purge-Done bug above.) User reported
the Reboot page's button appeared to just fall straight through to the next step instead of visibly
waiting for the box to come back.
- **Root cause**: `MainViewModel.RebootAndWaitAsync` (`ViewModels/MainViewModel.cs:651-700`) sends
  `nohup sh -c 'sleep 1; reboot' & disown` (returns almost immediately -- the box hasn't started
  going down yet, it's still inside that 1-second sleep), immediately disconnects the app's own SSH
  session, and immediately starts polling port 22 via `SshSessionService.WaitForPortAsync` for it to
  come back. Nothing yet confirms the box actually went DOWN first -- `WaitForPortAsync` only ever
  polls for "is the port open," and sshd is still fully up and accepting connections during that
  1-second window (and often for a while after, until systemd actually gets around to stopping it
  mid-shutdown). So the very first poll -- which starts within milliseconds of disconnecting -- can
  simply reconnect to the still-running, not-yet-rebooted box and report "back up!" almost instantly.
  The code then immediately reconnects for real, marks the Reboot step Done, and auto-advances to
  Post-Reboot Verify -- while the actual reboot the backgrounded command triggers happens later,
  completely unsupervised by the wizard.
- **Why it's inconsistent, not always broken**: this is a genuine race between the SSH command
  round-trip time and the remote script's 1-second sleep, not a deterministic bug -- some runs the
  poll happens to land during the box's real down-then-up window and it waits correctly (as it
  appeared to in the full terminal transcript from the purge-Done bug report above), others the first
  poll wins the race against the sleep and it falls straight through, exactly what got reported live.
- **Fix shipped**: `SshSessionService` gained `WaitForPortDownAsync` (up to 90s, polls for a connect
  attempt that clearly fails/refuses/times out -- the down half) alongside the existing
  `WaitForPortAsync` (the up half, unchanged). `MainViewModel.RebootAndWaitAsync` now calls the down
  check first and only proceeds to the up check once it succeeds; if the port never goes down within
  90s, the step is marked Failed with a clear "reboot may not have actually run" message instead of
  silently declaring success. Only a real down-then-up transition counts as "rebooted" now, matching
  the design intent already stated in the Reboot page's own caption text ("checkpoint... rather than
  assuming a reboot will always succeed").

**Docker as a first-class feature in FDT.Scout: built.** See What's New (`publish-v39`, FDT.Scout
2.1.0) and the Feature Map for what actually landed -- full scope below is the design record.
Started from "could we run Docker on this" -- confirmed Docker already runs today (Home Assistant's
Extra installs and uses it), but only as an implementation detail buried inside that one Extra. User
asked to make it a real, general capability, discussed over several rounds -- including one round
where the user pasted a much larger generic "full Docker dashboard" spec (exec-into-container
shells, live log streaming with search, a full docker-compose stack editor, image search, network/
volume management UI, bulk operations) that this assistant explicitly pushed back on as oversized
for this specific hardware (tight on RAM, ~6GB root storage) rather than building as-is -- user then
confirmed the trimmed scope below.
- **New Docker tab, gated on the feature being enabled** -- doesn't exist/appear until Docker is
  actually installed and verified, same "don't show controls for something that isn't there" pattern
  the rest of the app already follows.
- **Install/verify Docker, not a true on/off toggle.** Real snag surfaced and agreed on: once
  something (e.g. Home Assistant) is already using Docker, "turning it off" would break that -- so
  this is an idempotent "make sure Docker is installed and running" action, not a reversible switch
  presented as if it were one.
- **Full lifecycle control** on existing containers: start/stop/pause/restart/remove, plus name,
  image, status, ports, uptime, and per-container disk/resource usage in the list.
- **Run a new container from an image** -- a real create-container form: image (required),
  container name (optional, auto-generated if blank), ports (repeatable host:container), volumes
  (repeatable, nudged toward `/volume` by default), environment variables (repeatable KEY=value),
  restart policy (dropdown: unless-stopped/always/on-failure/no, matching Home Assistant's own
  install), and an optional memory limit (a cheap safety valve given RAM is this hardware's real
  binding constraint). Flow: `docker pull <image>` first as its own visible step (clear failure on a
  bad image name/network hiccup), then the actual `docker run` with the assembled flags.
- **Log viewing**: tail + refresh, with client-side filtering on whatever's already loaded --
  explicitly NOT real-time streaming with server-side search, which was in the larger pasted spec
  but sized down as a separate, bigger feature.
- **Storage-location picker, refined from "silently point at /volume" to an explicit user choice**:
  the tab shows where Docker is currently storing data (root partition free space) and offers to
  move it to `/volume` (free space shown there too) if mounted -- a real decision the user makes,
  not a default applied behind their back. This is also the direct answer to "designate space (the
  spinning disk)."
- **A simple daemon status block**: Docker version, container count, engine running/stopped -- reuses
  the existing `info-block` pattern already used elsewhere in the app.
- **No image allow-list**, deliberately different from the Pushbullet action-bounding decision: this
  lives entirely inside the already-authenticated FDT.Scout session, which already has full root
  terminal access -- pulling/running whatever image the user wants is a nicer UI over something
  already possible by hand, not a new capability.
- **Explicitly OUT of this scope, not silently dropped -- named and available to revisit**: a full
  docker-compose stack editor/lifecycle (directly fights the same RAM/storage constraints just
  established -- a multi-service stack is exactly the workload this hardware can't comfortably
  carry), exec-into-container shell access (real and buildable -- would reuse the existing PTY-over-
  WebSocket machinery the terminal already has, scoped to `docker exec` instead of a host shell --
  but a genuinely separate, non-trivial feature deserving its own sizing conversation), Docker Hub
  image search (the create-container form's plain image-name field already covers "I know what I
  want"), network/volume management UI, and bulk operations across multiple containers.
- **Where it lives**: shipped as its own new top-level "Docker" tab in FDT.Scout, separate from the
  Apps tab -- the Apps tab's existing Docker awareness (Home Assistant's container specifically,
  `kindDocker` in `apps.go`) is untouched and keeps working exactly as before.

**Remove the Headscale-server placeholder Extra: built** (decided 2026-08-24, built same session
as the Docker feature above, `publish-v39`). User asked
about the "Self-hosted Headscale server" placeholder in Remote Access, discussed why it exists
(explicitly out of scope -- the upstream script targets a separate VPS with Docker Swarm + Traefik,
not the Cloud Key, structurally unreachable from an app that only ever SSHes into the Cloud Key),
then asked to just remove it outright rather than keep it as an explained "coming soon" stub.
Checked before agreeing, not assumed:
- **Safe to remove**: `RestoreExtrasFromDeviceStateAsync` looks up each of the app's own known
  Extras BY ID in the device's saved state -- never iterates the device's JSON keys outward -- so
  deleting the catalog entry can't break any lookup, only stop one from happening.
- **No other real references**: FDT.Scout's own install catalog and `ScriptCatalog.cs` both mention
  Headscale, but only in passing description text (e.g. "or `--login-server=...` for a self-hosted
  Headscale server" as a how-to note on the Tailscale item) -- nothing else that's actually an Extra
  needing matching cleanup.
- **Device-state cleanup is already automatic, verified in the code, no new logic needed**:
  `WriteDeviceStateAsync` rebuilds the whole `statuses`/`details` dictionary from scratch every time,
  using only the app's *current* known Extras list, and `DeviceStateStore.WriteAsync` uploads that as
  a full replacement of the device's JSON file (not a merge) -- confirmed via `UploadSupportFileAsync`
  overwriting wholesale, not patching. So the very next time any Extra runs on an already-converted
  device, the stale `headscale-server` key in that device's own `fdtscout-state.json` disappears on
  its own. Also checked FDT.Scout's own Go-side state writer (`devicestate.go`) -- it only ever does
  targeted single-key upserts for things it actually installs, never referenced Headscale, so it
  can't reintroduce the key either.
- **What actually changes**: delete the one `ExtraItem { IsAvailable = false, ... }` entry from
  `Services/ExtraCatalog.cs` (the sole remaining user of the `IsAvailable=false`/"Coming soon" UI
  path -- everything else that was ever a placeholder got built for real over the course of this
  project). The reasoning for excluding it stays fully preserved here in QUE.MD and the private
  memory file, just no longer visible in the live app itself.

**Backup target: built.** Restic's `rest-server` shipped as a real Extra in a new Backup category
-- see What's New (`publish-v38`) and the Feature Map above for what actually landed. Syncthing
(the other lightweight option discussed) was NOT built -- still available to revisit if a
continuous-sync use case (rather than point-in-time backup) turns out to be wanted too.

**Step-by-step refactor review** (2026-08-23 discussion, 2026-08-24 build). Walked through every
core wizard step one at a time in chat, evaluating each against: (1) does it need to exist, (2)
does it offer a net benefit, (3) if kept, can it be rebuilt independently. Every agreed action item
below is now **built and published** (`publish-v26`) except the one still-open discussion point on
Step 2 (device-state visibility in the Health Check UI -- genuinely undecided, not deferred). Full
detail of each decision kept below for reference; see the Feature Map above and the What's New
entry dated 2026-08-24 for what actually shipped.
- **Step 1, Connect**: keep as-is. Never jnovack-sourced (app-authored SSH.NET code); nothing to
  remove. No changes queued.
- **Step 2, Identify & Health Check**: keep as-is. Never jnovack-sourced (app-authored
  `Services/PreflightService.cs`). One real discussion point queued: the on-device state file
  (`fdtscout-state.json`) currently only tracks Extras' status, not core Phase 1 step history, and
  its read (`RestoreExtrasFromDeviceStateAsync`) happens silently in the background right after
  this step rather than being surfaced in the Health Check UI itself. Open question for a future
  pass: should this step's own UI show "last touched by X on date Y" from that file explicitly,
  and/or should core Phase 1 step history be tracked in it too (currently those re-detect live from
  the device every time instead, which may be the right call already -- not yet decided).
- **Step 3, Remove UniFi App Layer (`phase1-purge.sh`)**: keep as jnovack-sourced, deliberately --
  this is the one step where "rebuild it ourselves" was explicitly rejected. The batched removal
  ordering encodes real hard-won knowledge about what's safe to remove and in what sequence on this
  hardware; re-deriving that independently would be a safety regression, not an improvement, unlike
  WireGuard (where we rebuilt the *assembly* logic around real upstream primitives -- here even the
  assembly is the valuable part). If there's ever an appetite to change this step's actual removal
  logic, the right channel is contributing upstream, not forking it. Three real improvements queued
  (additive, don't touch the purge logic itself):
  1. **Pre-purge snapshot before Apply runs** -- capture `dpkg --get-selections`/package list (and
     maybe key config files) before the purge fires, so there's an actual forensic trail if
     something goes wrong mid-purge, not just the terminal log. Read-only, cheap, zero interaction
     with the jnovack script.
  2. **Show a live preview on the confirmation screen itself**, not just prose -- right now Simulate
     exists as a separate manual step; the typed-confirmation screen for Apply describes the
     operation generically rather than showing the actual packages on *this* device about to be
     removed. Auto-run a quick read-only query when landing on the confirmation screen instead of
     requiring a separate Simulate click first.
  3. **Batch progress indicator -- built.** A "Batch N of 5" indicator now shows on the confirmation
     screen while a real Apply streams, derived purely by watching the purge script's own existing
     output markers as they scroll past (`--- purging: ... ---` / `--- skipping batch... ---`) --
     doesn't touch the script itself. Stays blank through Simulate, which never reaches the batch
     loop (see `MainViewModel.PurgeBatchProgress`'s doc comment).
  Confirmed NOT a gap: resumability. A failed/cancelled purge already resumes correctly on re-run --
  the upstream script recomputes what's still installed at the start of every run.
- **Step 4, Reboot**: keep as-is. Never jnovack-sourced (app-authored `RebootAndWaitAsync`) -- the
  "reboot after purge" *decision* comes from jnovack's runbook, but the mechanics are already ours.
  One thing to verify (not yet confirmed either way): does this step's "wait for port 22" polling
  loop share the same auto-reconnect logic built earlier this session (`TryReconnectLoopAsync`), or
  does it have its own separate implementation that could drift from it over time? A reboot is a
  *guaranteed* connection drop, not an edge case, so this is exactly the step where that logic
  matters most.
- **Step 5, Post-Reboot Verify (`phase1-verify.sh`)**: keep jnovack-sourced -- unlike Base Tooling,
  this stays in lockstep with whatever Purge actually did (same author, same assumptions about what
  "clean" looks like), so there's no real independence benefit here. One thing to verify: does this
  step's output feed the GO/NO-GO banner the same way Purge's does, or does it just pass/fail
  silently -- confirm that's wired up consistently.
- **Step 6, Install Base Tooling (`phase1-base-tools.sh`)**: **rebuild app-authored**, replacing the
  jnovack-sourced script -- weakest case for keeping upstream ownership (no hard-won
  ordering/hardware knowledge here, just an apt install list), and the app already has to keep it
  in sync with what its *other* still-jnovack-sourced bundled scripts assume is present regardless.
  Expanded package list agreed:
  - Add: `curl` (confirmed missing from the stock image -- `phase1-cloudkey-install.sh`'s own
    comment says so, and multiple scripts already work around this individually with
    `command -v curl || apt-get install curl`; this closes that gap once for everyone downstream),
    `wget`, `git`, `nano` (stock image likely only has busybox `vi`), `unzip`, `ca-certificates`
    (so HTTPS actually works cleanly for curl/wget/apt, not just "installed"), `tmux` (this becomes
    a real general-purpose box people SSH into directly too, not just through this app -- persistent
    sessions matter for anything long-running), `htop` (nicer terminal-side process view,
    complements rather than replaces FDT.Scout's own web `top` view).
  - Deliberately NOT adding: `build-essential` (heavy, most people won't compile anything on this
    specific low-power ARM box -- a deliberate opt-in if ever needed, not a default), `net-tools`
    (legacy; `ip`/iproute2 already covers this and is already confirmed present).
  - `jq` needs no action -- already present on the stock image (confirmed via the user's own dpkg
    output earlier this session).
- **Step 7, Format & Mount Storage (`phase1-format-mount-volume.sh`, Gen2+ only)**: **rebuild
  app-authored**, replacing the jnovack-sourced script. Flagged as riskier to get subtly wrong than
  Base Tooling was -- needs to land on the right filesystem type, mount options, and `/etc/fstab`
  entry format for this specific hardware to survive a reboot correctly, not just "run one apt
  command." Whoever implements this should verify those specifics against the current script's
  actual mechanics before writing the replacement, not assume. The extensive device-picker safety
  UI already built around this step (✓ Suggested / ⚠ DO NOT SELECT badges, risk classification,
  typed-exact-device-path confirmation) stays -- this is a script-layer rebuild, not a UI rewrite.
  Open sub-question, not yet decided: add a live partition-table preview (what's actually about to
  be wiped, read straight off the selected device) directly on the confirmation screen, same
  "informed consent based on real data" idea raised for Step 3's purge confirmation -- revisit when
  this step is actually built.
- **Step 8, Replace Front-Panel LCD App (`phase1-cloudkey-install.sh`)**: **rebuild app-authored,
  folded into FDT.Scout itself** -- confirmed by the user (2026-08-23) that the panel has never
  shown anything but the UniFi logo, not once, across every attempt this whole project. jnovack's
  `cloudkey` binary itself reads as a genuinely well-built, maintained project (real docs, a full
  CLI/env-var reference, versioned releases, a systemd unit that shows real engineering judgment --
  it deliberately omits `ProtectKernelTunables` with a documented reason about LED sysfs writes) --
  not "scraped together." The failure is very plausibly firmware/AppArmor-specific, not a defect in
  jnovack's own work, and untested on this exact UniFi OS + AppArmor combination.
  Core design bet, not yet empirically confirmed: AppArmor confinement is normally bound to one
  exact executable *path*, not "framebuffer access" as a general operation -- if the denial
  (surfaced by `checkApparmorDenial`, built earlier this session) names a profile scoped to
  `/usr/local/bin/cloudkey` specifically, a fresh, differently-pathed binary we write ourselves
  would very likely fall completely outside that profile's confinement and could open `/dev/fb0`
  freely -- sidestepping the AppArmor fight entirely rather than trying to win it on jnovack's
  binary. Verify this empirically against the real device before or during implementation (the
  diagnostic is already built and shipped -- it just hasn't been run against real hardware yet).
  Folding into FDT.Scout rather than a separate program: it already runs as root, already knows the
  hostname, and this removes the *last* runtime GitHub dependency in the whole project (currently
  the one exception to "100% self-sustained"). Scope for a first version: hostname + maybe IP,
  drawn as text directly to the framebuffer -- not attempting to replicate jnovack's full
  multi-screen cycling feature set (tunnel/app status rows, stealth mode, etc.) on day one.
- **Step 9, Harden Access (`phase1-security-setup.sh` + `security-lock.sh`)**: keep jnovack-sourced
  as-is, same reasoning as Purge -- this is security-sensitive *sequencing* (create user -> set up
  keys -> only then lock password auth, not the reverse), where getting the ordering wrong risks
  locking yourself out of your own device, not just leaving a mess. Confirmed by the user: stays
  optional (already correctly skippable). **New addition**: add an explanation directly on this
  step's page -- what Harden Access actually does and how it works (creates a dedicated `cloudkey`
  user, forces key-only SSH, locks password auth), so the user is deciding Skip-vs-Apply from real
  information on the page itself, not just the step's one-line title.
- **Step 10, Remove Orphaned Accounts (`phase1-account-cleanup.sh`)**: **rebuild app-authored AND
  fold into Step 3 (Purge)** as an automatic follow-on action, rather than keeping it a separate
  wizard step. Reasoning for the fold: orphaned accounts only exist *because* of what Purge did --
  packages get removed but Debian packaging doesn't auto-delete the Unix accounts a package's
  postinst created, so cleanup is really the second half of "actually remove the UniFi app layer,"
  not an independent concern. Folding it removes a step from the wizard's flow entirely rather than
  just relocating complexity. Deliberately NOT folded into Step 9 (Harden Access) despite both being
  "final account state" concerns -- Harden Access is optional/skippable and conceptually unrelated
  (adds new hardened access vs. removes old cruft); making cleanup contingent on opting into
  hardening would wrongly skip housekeeping for anyone who declines it.
  Rebuild scope: lower-risk than Base Tooling's category even -- the real work here isn't ordering
  or sequencing, it's knowing the exact UniFi service account names to target so cleanup doesn't
  accidentally sweep up a real user account. Pull that list from what the actual current script
  targets, not guessed. Preserve the existing `--dry-run` safety property when rebuilt -- if it
  runs automatically as part of Purge's own Simulate/Apply distinction, the dry-run behavior should
  follow Purge's existing Simulate mode rather than needing a separate confirmation flow.

**Now moving into the individual Optional Extras, category by category** (Finish & Summary saved
for last per the user's explicit sequencing choice):

- **General Extra: FDT.Scout web console**: stays as-is -- General category, on by default,
  one-click-required posture unchanged. **New bug queued**: `ADMIN_PASSWORD` (and any other
  password-like `ExtraParam`, e.g. WireGuard's `WG_CONFIG` which contains a private key) currently
  renders in **plain, fully visible text** -- confirmed by reading `MainWindow.xaml`: every
  `ExtraParam` goes through the same generic `ExtraItemTemplate` TextBox, there's no masked-input
  path at all for Extras' Params (unlike the Connect page's own password field and the Finish
  page's password-change fields, which already correctly use `PasswordBox`). Fix: add an
  `IsSecret` flag to `ExtraParam`, mark `ADMIN_PASSWORD` (and `WG_CONFIG`) with it, render those as
  a masked `PasswordBox` with an eye-icon toggle to reveal plaintext on demand -- reuse the
  PasswordBox-plus-code-behind-sync pattern already established three times in this app
  (`PasswordInput`, `NewPasswordBox`, `ConfirmPasswordBox`) rather than inventing a new one.

0. **FDT.Scout expansion, 5 items queued 2026-08-23 -- all 5 now shipped** (see What's New for
   full detail). The last 2 (Apps real installs, USB mount+browse, static IP/DHCP) were initially
   deferred citing size, which the user correctly called out as inconsistent with "build promptly"
   -- built immediately after, same session, no further deferral:
   - Shared on-device state file, Users tab login history + lockout, Health tab network
     throughput, Certificates tab port + 80->443 redirect.
   - **Apps tab now has real installs**, full parity with CloudKey Wizard's Optional Extras
     (fail2ban, unattended-upgrades, Plex, Home Assistant, the full Servarr stack, AutoSSH,
     Tailscale, WireGuard with its full config form) -- reuses the exact same bundled scripts,
     embedded into fdtscout too and run locally rather than reimplemented.
   - **USB drives**: mount/unmount + a real read-only file browser (navigate, view, download).
   - **Static IP/DHCP**: built exactly to the user's own specified safety design -- apply, 5-minute
     provisional window, explicit "Accept IP changes" confirmation (shown as a splash the instant
     the dashboard loads while a change is pending) or automatic revert. Sidesteps the unresolved
     "which config system does this OS use" question by working directly against the live kernel
     routing table with `ip` (confirmed present via the bundled WireGuard scripts) rather than
     guessing at netplan/ifupdown/systemd-networkd/NetworkManager file syntax -- persists the
     pending-change state to disk too, so a mid-window service restart doesn't lose the safety net.
1. **Front-panel-shows-only-the-UniFi-logo -- real root cause found, fix attempted, not yet
   confirmed working.** User supplied the actual `journalctl -u cloudkey` output from the real
   device (2026-08-23, screenshot) -- first real diagnostic evidence for this bug, not a guess.
   The earlier "ck-ui still holding the framebuffer" hypothesis was wrong: the log shows
   `cloudkey.service` (confirmed, by reading its actual unit file, to run as root with NO device-
   cgroup restrictions) still getting `open /dev/fb0: operation not permitted`. Root being refused
   something plain file permissions would allow is the signature of a mandatory access control
   layer (AppArmor/SELinux), not a permissions or ownership bug. Built: `checkApparmorDenial` scans
   the kernel log for the exact DENIED entry naming fb0/event1/cloudkey, surfaced plainly on the
   Front Panel tab with the exact denial line, plus a one-click "Attempt fix" that sets *only that
   one profile* to complain mode (`aa-complain`, logs but allows -- not a system-wide AppArmor
   disable) and restarts `cloudkey.service`. **Not yet confirmed this actually fixes it** -- the
   diagnosis is solid (real log evidence, not a guess), but whether `aa-complain` is even installed
   on this image, and whether complain mode is sufficient to let the framebuffer open succeed, are
   both unverified without watching it run on the real device.
2. **None of FDT.Scout's newer tabs are click-through-confirmed against real hardware yet**: Apps
   (status, the real installs, and the new process Kill button), About, Users login history/lockout,
   Certificates port/redirect, USB mount+browse, static IP/DHCP, System specs, and the AppArmor
   fix attempt. All reasoned through carefully and built
   cleanly, but "builds cleanly" isn't "watched it work." Static IP is the one to test most
   cautiously first, given what a wrong result there actually costs.
3. **Danger/Caution severity banners in Light theme, specifically.** The Connect page is confirmed
   good (user screenshot). MenuItem's highlight trigger got a small hardening pass this session
   (Foreground was never pinned during the highlight state, same gap the ComboBoxItem fix started
   from) -- but not escalated to a full ControlTemplate override like ComboBoxItem got, since no
   live report has actually shown a Menu contrast problem (only the KnownHosts ComboBox did). What's
   still genuinely unconfirmed: the Danger/Caution severity banners (fixed dark accent colors by
   design, unchanged across themes) floating on a now-white Light-mode panel background, and
   whether the Menu's dropdowns hold up in Light mode specifically. Low priority -- the core theme
   system is confirmed working.

**Resolved, not just deferred (WireGuard)** -- WireGuard fail-closed egress shipped as a real, fully-functional
Extra (see What's New): the "hand-edited template" problem was solved by turning the template
substitution itself into the app -- the 7 bundled scripts are uploaded verbatim and their
`<placeholder>` tokens are filled in from a real Params form (namespace name, provider DNS IP,
ports, service names, provider check URL/name, and the pasted WireGuard config itself), plus an
app-authored orchestrator script that also writes the systemd units (fetched verbatim from the
upstream wiki page, since they aren't standalone repo files) and runs the real verification script
at the end. Self-hosted Headscale remains the one genuinely out-of-scope placeholder: its server
script explicitly targets a *different* VPS with Docker Swarm + Traefik already running, not the
Cloud Key -- this app only ever SSHes into the Cloud Key, so there's structurally nothing for it to
drive there. Tailscale *client* (the tractable, Cloud-Key-side half of Phase 5) also shipped as a
real Extra.

**Note**: `%LocalAppData%\CloudKeyWizard\hosts.json` (the known-hosts list backing the Connect
page's "Previously connected" dropdown) was accidentally deleted during test cleanup this session,
mistaken for disposable test output rather than the user's real saved connection history. No lasting
harm -- it rebuilds automatically on the next successful connect -- but flagging it here since it's
why a fresh launch currently shows no saved hosts despite the feature working as designed.

**Closed, not reopening**: an automated Debian *release* upgrade. Confirmed by the user
(2026-08-23) as a bad idea, not just excess caution -- this app's own bundled scripts document
this exact operation bricking this exact hardware once already. What shipped instead, and what's
staying: a diagnostic-only Extra ("Check current Debian release") that shows the current
codename/apt sources and explains why this app stops there rather than automating further.

---

## Feature Map

Where things live, for picking this back up without re-deriving it.

| Area | What it does | Key files |
|---|---|---|
| Connect | SSH (password + key + keyboard-interactive fallback — most UniFi OS sshd configs need it) | `Services/SshSessionService.cs`, `ViewModels/MainViewModel.cs::ConnectAsync` |
| Identify & Health Check | Positive hardware ID (Gen2 vs Gen2+ from base-files package), root/sshd/dpkg health, idempotent detection of what's already applied | `Services/PreflightService.cs` |
| Remove UniFi App Layer | Real `phase1-purge.sh` run, Simulate = genuine safe no-op. Confirmation screen now shows a live preview of exactly what's about to be removed on *this* device (not just prose); a pre-purge package snapshot is captured first as a safety record; a "Batch N of 5" progress indicator shows while Apply streams (derived from the script's own output markers, not a script change); automatically removes orphaned UniFi accounts right after as a follow-on (see below -- no longer a separate step) | `Scripts/runbook/phase1-purge.sh`, GO/NO-GO via `Services/OutcomeInterpreter.cs`, `ViewModels/MainViewModel.cs::LoadPurgePreviewAsync/PurgeBatchProgress` |
| Reboot | Fires reboot, polls port 22, auto-reconnects | `ViewModels/MainViewModel.cs::RebootAndWaitAsync` |
| Post-Reboot Verify | `phase1-verify.sh` | `Scripts/runbook/phase1-verify.sh` |
| Install Base Tooling | App-authored -- rsync, nfs-common, curl, wget, git, nano, unzip, ca-certificates, tmux, htop | `Scripts/runbook/phase1-base-tools.sh` |
| Format & Mount Storage | App-authored -- Gen2+ only; live `lsblk` device picker with ✓ Suggested / ⚠ DO NOT SELECT badges, never a typed path; whole-disk ext4 + a systemd `.mount` unit (not `/etc/fstab`, which this hardware's vendor bootup-hook framework silently resets on every boot) | `Scripts/runbook/phase1-format-mount-volume.sh`, `Services/BlockDeviceService.cs` |
| Harden Access | Optional (Skip button); creates `cloudkey` user, key-only SSH, locks passwords; the step's own page now explains what it does and how, so Skip vs. Apply is an informed choice | `Scripts/runbook/phase1-security-setup.sh` + `security-lock.sh` |
| Optional Extras | General (FDT.Scout web console + 6 other real items) + Media/Download Server (Plex, NZBGet, Sonarr, Radarr, Prowlarr -- all real, latter 4 real upstream jnovack Phase 2 scripts with prerequisite/sibling handling) + Home Automation (Home Assistant via Docker, real) + Remote Access (AutoSSH, Tailscale client, and WireGuard fail-closed egress all real; the self-hosted Headscale placeholder was removed entirely -- structurally out of scope, targeted a separate VPS, see What's New) + **Backup** (new category: a Restic `rest-server`, real upstream binary pinned + checksum-verified, client-side-encrypted backups from other machines land on `/volume`, retention controlled client-side). Extras can declare multiple named env-var `Params` (not just one text field, including a multiline paste-friendly field via `IsMultiline`), a `PrerequisiteBundledScriptFileName` run-to-completion-first, `SiblingBundledScriptFileNames` uploaded-but-not-run (text content), and `BinaryUploads` (raw bytes to an absolute remote path -- for content that isn't UTF-8 text, e.g. FDT.Scout's compiled binary). Password/secret Params (FDT.Scout's admin password, WireGuard's pasted config) are masked by default with a Show/Hide toggle, typing into the masked field works correctly and is live-confirmed (see What's New for the real root cause -- a WPF class-handler fix, not the template-restructure theory tried and ruled out along the way), and the value is redacted (`'***'`) in the terminal's command-echo line rather than shown in plaintext. Params can also be a dropdown (`IsDropdown`/`Options`) -- used by "Set timezone," a curated ~50-zone list. Each Param's editor (plain/dropdown/secret/multiline) is built by `ExtraParamTemplateSelector`, one of four separate templates rather than one shared template with Visibility toggling | `Models/ExtraItem.cs`, `Services/ExtraCatalog.cs`, `PasswordBoxHelper.cs`, `ExtraParamTemplateSelector.cs` |
| FDT.Scout web console | App-authored HTTPS admin console (not jnovack/cloudkey content) -- password-gated login (5 min idle timeout + 24h absolute cap, auto-redirect on expiry; 5 failed logins locks a username out 15 min; recent login activity + last-login-per-account on the Users tab), a real web terminal (PTY over WebSocket), TLS cert generate/install + configurable HTTPS port + port-80-redirect toggle, a **Front Panel tab**: FDT.Scout draws the display itself (`framebuffer.go` -- direct `/dev/fb0` writes, no dependency on a separate service that was never rendering correctly on this hardware) cycling through **user-defined screens** (add as many as you want, up to 4 widgets each -- hostname/IP/time/CPU/RAM/disk/uptime/custom text via `displayconfig.go`), on/off toggle, configurable cycle interval, a live preview, plus the real AppArmor-denial diagnostics + one-click scoped fix attempt kept from before, a **Settings tab** consolidating hostname, IP/DHCP, DNS, and NTP (time sync -- `netsettings.go`, detects systemd-resolved vs. plain resolv.conf and systemd-timesyncd rather than assuming), a System specs block (CPU/RAM/disk/kernel) at the top of Health, 7-day CPU/mem/disk/network-throughput charts, a `top`-style live process table with a Kill button per row (CPU%/MEM%/RSS per
process, load average, uptime, auto-refreshing every 3s), USB drive mount/unmount + a read-only file browser, an **Apps tab** with real installs now (full parity with CloudKey Wizard's own Extras -- reuses the exact same bundled scripts, embedded here too, run locally rather than reimplemented), and an **About tab** (version, references, disclaimer, changelog). A separate Go module, cross-compiled to a single linux/arm64 binary and embedded into the exe the same way the runbook scripts are; installed by its own Extra as a systemd service on port 443 (default; configurable). Re-running the Extra to *update* an existing install works correctly (temp-path + atomic-rename upload, explicit `systemctl restart`). Listed first in General, "on by default" in the sense of being the recommended top item with sane pre-filled defaults -- still requires the one Run click every Extra requires. Versioned independently (currently 2.0.1, see `tools/fdtscout/version.go`) | `tools/fdtscout/` (source), `src/CloudKeyWizard/Scripts/fdtscout/` (embedded binary + systemd unit), `Services/ExtraCatalog.cs`'s `fdtscout` item, `Services/SshSessionService.cs::UploadBinaryFileAsync` |
| Monitoring tab (FDT.Scout) | Separate from Health on purpose (Health = the device itself; Monitoring = what it watches for the user). Ping/TCP/HTTP/DNS host monitors with uptime %/latency history; an automatic WAN speed test (Cloudflare speed-test endpoints, no external binary) every 6h; public IP change tracking + an optional Cloudflare Dynamic DNS updater; an on-demand local-subnet IP range scan and open-port scan (both strictly user-triggered, scoped to the device's own subnet, never scheduled); a passive LAN device list (ARP table) with a Wake-on-LAN send button; log aggregation (`journalctl` for a fixed set of key units) onto `/volume` with 90-day retention, browsable from the dashboard; scheduled tasks backed by real tagged crontab entries. Metrics history also now extends to 90 days (was 7) when `/volume` is mounted | `tools/fdtscout/monitors.go`, `wanspeed.go`, `publicip.go`, `scanner.go`, `logs.go`, `scheduledtasks.go`, `netcheck.go`, `handlers_monitors.go`, `handlers_scanner.go`, `handlers_logs.go`, `handlers_tasks.go` |
| Pushbullet (FDT.Scout) | Proactive alerts (disk space, a monitored service going down, login lockout, IP change, optional daily digest -- each an individually toggleable Settings-tab checkbox) plus a two-way command channel: FDT.Scout polls Pushbullet for pushes starting with its own configurable callsign (defaults to hostname) and replies, silently ignoring everything else -- including its own replies (tracked by Pushbullet `iden`, fixed after a real self-reply-loop bug) and any command it doesn't recognize (no reply at all, not an error message). Read-only queries (`status`/`disk`/`ip`/`uptime`/`apps`/`health`/`ping <host>`) answer immediately; actions (`restart`/`start`/`stop`/`enable`/`disable` an app) are bounded to the same fixed catalog the Apps tab's own start/stop/enable/disable already enforces (never a free-form command) and require a second `<CALLSIGN> CONFIRM` push within 5 minutes before executing -- deliberately designed over 3 rounds of discussion with the user, see the private memory file for the full history | `tools/fdtscout/notify.go`, `pushbullet_listen.go`, `handlers_settings.go`'s Pushbullet handlers |
| Shared device state | `fdtscout-state.json`, written to the existing SSH audit-trail directory on the device -- both CloudKey Wizard and FDT.Scout read/write it, so Extras' status (installed via either app) stays in sync between them, and a *different* copy of CloudKey Wizard connecting later picks up real state instead of starting blank. Device-authoritative: overrides local `KnownHosts.json` data for the same host when both exist | `Services/DeviceStateStore.cs`, `tools/fdtscout/devicestate.go`, wired into `MainViewModel.ConnectAsync/PersistExtraStatus` |
| Docker tab (FDT.Scout) | General-purpose container manager, its own top-level tab -- separate from the Apps tab's pre-existing, narrower Docker awareness (`kindDocker` in `apps.go`, still scoped to just Home Assistant's named container, untouched). Install/verify Docker with one click (idempotent `apt-get install docker.io` + `systemctl enable --now` -- deliberately one-way, not a real toggle, since something already using Docker would break if it were "turned off"); full lifecycle control on existing containers (start/stop/pause/restart/remove) with per-container size shown (`docker ps -a --format json`); a run-from-image form (image, name, ports, volumes, env vars, restart policy, optional memory limit) that runs `docker pull` as its own visible step before `docker run`, so a bad image name or network hiccup fails clearly; tailable log viewing (`docker logs --tail`); and an explicit storage-location control -- shows Docker's current data directory (reads `data-root` from `/etc/docker/daemon.json`, falls back to the real documented default) and, when `/volume` is mounted and not already in use, offers to move Docker's data there (`rsync`s the existing tree first, only switches `daemon.json` over and restarts once the copy succeeds, reverts automatically if the daemon fails to come up on the new path). Deliberately out of scope, named rather than silently dropped: compose stacks, exec-into-container shell, Docker Hub image search, network/volume management UI, bulk operations | `tools/fdtscout/docker.go`, `handlers_docker.go`, `static/index.html`'s `panel-docker`, `static/app.js`'s Docker section |
| CloudKeyWizard.exe About / Changelog | Help menu -> About (version, references incl. jnovack/cloudkey and the original FullDuplexTech.com article, disclaimer) and View Changelog, both a scrollable `TextWindow` (was a plain MessageBox for About, nothing for Changelog before). Version currently 2.14.0 | `Version.cs`, `TextWindow.xaml(.cs)`, `MainWindow.xaml.cs::AboutMenuItem_Click/ChangelogMenuItem_Click` |
| FDT.Scout version check + update highlighting | Connecting to an already-set-up device checks the live-installed FDT.Scout version (`fdtscout -version` over SSH) against `AppVersion.BundledFdtScoutVersion` (this build's own embedded copy); a mismatch shows a yellow "Update available" badge on the FDT.Scout Extra, switches its Run button to Update, and states both versions in the detail line. Scoped to FDT.Scout only -- the one Extra with a live-reportable, bundled-tracked version | `ExtraItem.InstalledVersion/UpdateAvailable`, `MainViewModel.CheckFdtScoutVersionAsync`, `tools/fdtscout/main.go`'s `-version` flag |
| First-run disclaimer | "Before you start" overlay now also carries a plain "use at your own risk, informational purposes only, not affiliated with Ubiquiti" liability line alongside the existing technical-risk warning | `MainWindow.xaml` ~line 468 |
| Terminal pane copy/paste | Right pane is a read-only `RichTextBox` (was a non-selectable `ItemsControl`+`TextBlock`) -- text can now be selected and copied out with Ctrl+C, same per-line coloring as before | `MainWindow.xaml` right pane, `MainWindow.xaml.cs::TerminalLines_CollectionChanged` |
| Finish & Summary | Live report (host/model/auth/every step's outcome) + Copy button + password-change utility (unlock → `chpasswd` → re-lock, password never shown in plaintext in the log) | `ViewModels/MainViewModel.cs::BuildSummaryText/SetPasswordAsync/RelockAsync` |
| Terminal pane | Every command's real output streamed live; "Drop to shell" opens a real interactive session alongside, Ctrl+C/D/Z sent as raw control bytes to interrupt/EOF/suspend the running command | `MainWindow.xaml` right pane, `SshSessionService.OpenInteractiveShell/SendRawShellByte` |
| Safety | Cancel button (works mid-run); auto-reconnect-and-retry (max 2 attempts) on a genuine dropped connection, distinct from a script failure | `ViewModels/MainViewModel.cs::TryReconnectLoopAsync` |
| Known hosts | Persisted list of every host connected to (never the password) shown on Connect for one-click prefill; restores Extras' status on reconnect (the one thing that doesn't self-resume from the device); Forget/Clear/Export/Import via File menu (import merges, newer-wins on conflict) | `Models/KnownHost.cs`, `Services/KnownHostsStore.cs` |
| Menu bar | File (Forget/Clear/Export/Import hosts, Exit), View (Dark Mode toggle), Help (About / View Disclaimer Again / Troubleshooting Tips) | `MainWindow.xaml` Menu, `MainWindow.xaml.cs` click handlers |
| Dark/Light theme | Toggle in View menu, persisted across launches; every ambient color (backgrounds/borders/text/accent) is `DynamicResource` against a swappable merged dictionary; status/severity/badge colors (Danger/Caution/GO-NO-GO/etc.) are deliberately theme-invariant fixed accents, not swapped; terminal pane stays dark in both themes on purpose | `Themes/DarkPalette.xaml`, `Themes/LightPalette.xaml`, `Services/AppSettingsStore.cs`, `MainViewModel.IsDarkTheme/ApplyTheme` |
| ComboBoxItem contrast | Global (app-wide) style gives every ComboBoxItem a full `ControlTemplate` override -- not just property Setters, which weren't enough to fully override the default template's own hover/selection visuals underneath (confirmed live: "Previously connected" host list was hard to read in Dark Mode). MenuItem got a smaller matching fix (Foreground pinned during highlight, not escalated to a full template override since no live report has shown a Menu contrast issue) | `App.xaml` ~line 122 (ComboBoxItem), ~line 167 (MenuItem) |
| Script provenance | All Phase 1 scripts bundled as embedded resources at build time, pinned to a specific jnovack/cloudkey commit, SHA-256 hash shown in-app — zero runtime GitHub dependency | `Services/BundledScriptProvider.cs`, `Scripts/runbook/README.md` |
| Portable build | Self-contained single-file win-x64 exe, no installer | `scripts/Publish.ps1` |

---

## What's New

### Session 1 — 2026-08-23 (build day + first live hardware test)
- Built the app from scratch: three-pane WPF wizard (step nav / current step / live terminal),
  full Phase 1 runbook automation.
- **Switched from live-fetching scripts off GitHub to fully bundled/embedded** — user wanted the
  app "100% self sustained." Zero runtime GitHub dependency now except the optional LCD step,
  which reaches GitHub from the device itself.
- Fixed a real SSH auth bug: this hardware's sshd doesn't accept plain `password` auth, only
  `publickey`/`keyboard-interactive` — added the missing auth method.
- Fixed a `dpkg --audit` false positive (harmless "missing md5sums" on vendor packages) that was
  wrongly blocking preflight, plus a real gating bug where a failed blocking check didn't actually
  lock downstream steps.
- Added the GO/NO-GO outcome banner, built specifically from the real bundled scripts' known
  marker text (not just exit codes — a safe Simulate run is *designed* to exit non-zero).
- **Chased and fixed a genuine hang** across three attempts: first suspected a two-stream SSH.NET
  read deadlock (real bug, fixed, but not the actual cause here), then a trailing `EndOfStream`
  check after command completion (real bug, fixed, still not it) — the actual cause was that an
  SSH exec channel never closes stdin on its own, so every script's confirmation prompt was
  blocking forever waiting for input that would never arrive. Fixed with `< /dev/null`.
- Added a Cancel button and real cancellation support (the cancellation token existed but was
  never actually checked before this).
- Added auto-reconnect-and-retry when a connection genuinely drops mid-step (distinct from a
  script failure), capped at 2 auto-retries.
- Device picker: added safe-device recommendation logic (✓ Suggested / ⚠ DO NOT SELECT badges),
  fixed a blank "Type ___ to enable Apply" hint (missing change notification), fixed "unknown
  size" on every drive (this hardware's `lsblk` emits size as a JSON number, not a string), fixed
  an unreadable white-on-white dropdown.
- Made Harden Access skippable (access hardening is a policy choice, not load-bearing for the
  conversion itself).
- Added the Optional Extras step and rebuilt Finish & Summary (live report, Copy button,
  password-change utility that works even after Harden Access disables password login).
- First live test against a real Cloud Key Gen2 Plus (`10.0.100.19`): reached and completed
  Identify & Health Check, Remove UniFi App Layer, Reboot, Post-Reboot Verify, Install Base
  Tooling, and Format & Mount Storage successfully.
- **Same day, later pass — built out several queued items**: added Ctrl+C/Ctrl+D/Ctrl+Z support in
  the "drop to shell" pane (raw control bytes written straight to the `ShellStream`, intercepted at
  `PreviewKeyDown` since TextBox's built-in Ctrl+C-copy binding would otherwise eat it first);
  added the known-hosts list (persisted, never the password, prefills Connect, restores Extras'
  status on reconnect); added a File/Help menu bar (Forget/Clear hosts, Exit, About, re-show
  disclaimer, troubleshooting tips) — styled defensively against the same white-popup-on-dark-theme
  bug hit earlier with the ComboBox, not yet visually confirmed live; added the safe half of "OS
  updates" as a new Extra (`apt update && apt upgrade`, within the current Debian release only).
  Deferred Home Assistant/Plex, the risky Debian-release-upgrade half, and Dark/Light mode at that
  point, pending live testing / a decision.
- **User then said "do them all" explicitly** — built the rest, with one deliberate exception:
  - **Plex**: real Extra now (official ARM64 apt repo, points at `/volume` for media if mounted,
    hands off to the web setup for account claiming).
  - **Home Assistant**: real Extra now, new "Home Automation" category (Docker Container install,
    not Home Assistant OS; host networking; config on `/volume` if mounted; prints `free -h` first
    since this hardware is tight on RAM).
  - **Debian release upgrade — deliberately built as diagnostic-only, not automated.** This is the
    one item where "do them all" didn't get implemented literally. Reasoning: this app already has
    documented, specific evidence (in the very scripts it bundles) that this exact category of
    operation bricked this exact hardware once before. I don't know this device's current Debian
    codename or a verified-safe target release, so scripting the actual jump would mean guessing at
    exactly the kind of decision that caused the original failure. What shipped instead: a
    diagnostic Extra that prints the current codename/apt sources and explains clearly why this app
    stops there rather than automating further — see Queue #1 if the automated version is wanted
    anyway despite the risk.
  - **Dark/Light mode toggle**: full implementation. Two real palettes (`Themes/DarkPalette.xaml`,
    `Themes/LightPalette.xaml`, contrast-checked separately rather than a straight color invert),
    every ambient color converted from `StaticResource` to `DynamicResource` so a runtime dictionary
    swap propagates with no per-binding refresh hack needed. Scope simplification that made this
    tractable: badge/status/severity colors (Done/Failed/Danger/GO-NO-GO/etc.) stayed fixed accents
    rather than becoming theme-aware, since they're deliberately high-contrast indicators meant to
    read the same regardless of ambient theme -- avoided touching six converter classes' hardcoded
    colors entirely. Terminal pane background also stays dark in both themes on purpose. Persisted
    via a new `Services/AppSettingsStore.cs`; toggle lives in a new View menu. Smoke-tested by
    forcing light mode via a hand-edited settings file before launch (both the loose Debug build and
    the single-file published exe) -- confirmed the resource-dictionary swap doesn't crash, but the
    actual visual contrast/quality of the light palette has not been eyeballed yet.
  - **Known-hosts export/import**: File menu, merges on import (newer `LastConnected` wins on a
    host that exists in both lists) rather than replacing the current list outright.

### New session -- resumed, "do 2 and 3 now"
- Attempted a screenshot to visually verify the menu/theme work (queue item #3). It captured the
  wrong thing -- the app wasn't in foreground, so the capture caught the actual desktop, including a
  browser window with personal content (YouTube, bookmarks, subscriptions). Deleted immediately;
  flagged to the user; not retried without a properly targeted approach and their go-ahead.
- Discovered the real scope of "the placeholder categories": not 4 files, **19** across Phase 2-5.
  Paced accordingly rather than rushing all of it:
  - **AutoSSH (Phase 3, 1 file)** -- built as a real Extra, first one sourced from a genuine
    upstream jnovack script rather than app-authored. This required extending the Extras model
    properly: `ExtraItem` now supports multiple named `ExtraParam` env-var inputs (RELAYS,
    CLIENT_NAME, TIER1_PORT, WEBUI_PORTS) instead of the old single-text-field mechanism, and a new
    `BundledScriptFileName` path (loaded via `BundledScriptProvider`, same hash/provenance
    treatment as Phase 1) alongside the existing app-authored `ScriptContent` path. The script
    itself only sets up the client side and deliberately leaves the tunnel disabled, printing a
    relay-side setup block (public keys only) for the user to run by hand on a separate host --
    this app has no way to reach that host, so it doesn't try.
  - **Servarr media stack (Phase 2, 6 of 9 files)** -- NZBGet, Sonarr, Radarr, Prowlarr all built as
    real Extras. Required two more model additions: `PrerequisiteBundledScriptFileName` (run to
    completion first, abort the main script if it fails -- phase2-00-provision-drive.sh, needed by
    3 of the 4 apps, runs automatically rather than being its own separate click) and
    `SiblingBundledScriptFileNames` (uploaded alongside but never run directly -- phase2-05-servarr-
    sqlite-heal.sh, which Radarr/Prowlarr look up via `$(dirname "$0")`, same pattern as Phase 1's
    security-lock.sh dependency). The remaining 3 Phase 2 files (06 liveness-probe, 07
    nzbget-fix-obfuscated, 08 media-incoming-sync + its 4 support files) are maintenance/automation
    add-ons on top of a working stack, not blocking core functionality -- deferred, not yet fetched.
  - **WireGuard (7 files) and Headscale/Tailscale (2 files) deliberately not started yet** --
    real VPN/network-security configuration where a wrong template has real consequences; wanted to
    check in on pacing before diving into those specifically, rather than rush them in the same pass
    as everything above.
- **Screenshot mistake, second data-handling incident this session**: during theme-system testing,
  deleted `%LocalAppData%\CloudKeyWizard\hosts.json` as "test cleanup," not realizing it held the
  user's real saved connection history from earlier genuine use, not something the test run had
  created. No lasting harm (it rebuilds on next connect) but flagged plainly, and recorded as a
  standing lesson (see the private memory file) about not deleting anything during test cleanup
  without first confirming it's actually disposable.
- **Automated Debian release upgrade: user confirmed (not just my own caution) this was correctly
  left unbuilt** -- removed from the open queue entirely, not just deferred.
- **WireGuard/Headscale, finished the investigation**: asked the user two clarifying questions
  first (what should the VPN/remote-access capability actually do; does a Headscale server already
  exist somewhere) -- answer was "not sure, expose the options rather than picking one for me,"
  "give both, I've never used this before." Fetched and read the actual upstream scripts before
  building anything further, which changed the plan:
  - **WireGuard fail-closed egress (Phase 4, 7 files)**: confirmed a hand-edited *template*, not a
    runnable script -- multiple files share literal placeholders (`<vpn>`, `<uplink>`,
    `<provider-dns-ip>`, a provider-specific connectivity-check URL) that only make sense once
    filled in against a specific VPN provider's own WireGuard config (private key, peer endpoint) --
    material only the user can supply, which they don't currently have. Left as an accurately-worded
    placeholder explaining exactly what's needed, rather than building a form for values nobody has.
  - **Self-hosted Headscale server (phase5-00, 1 file)**: turned out to explicitly target a
    *separate VPS* with Docker Swarm and Traefik already deployed as their own stack -- not the
    Cloud Key at all, and structurally outside what an app that only ever SSHes into the Cloud Key
    can do anything with. Left as a placeholder explaining this plainly (not "coming soon").
  - **Tailscale client (phase5-01, 1 file)**: this half genuinely runs on the Cloud Key and needs
    zero config -- installs `tailscaled` via apt, real upstream script, shipped as a real Extra.
    Joining a tailnet afterward is a manual step even in the original script (needs a login server +
    auth key), explained in the Extra's own description; works against Tailscale's own free
    coordination service with no self-hosted server required at all, which is probably the simpler
    path for someone who's never used any of this before.
  - Net result (at that point in the session): Remote Access category was AutoSSH (real) +
    Tailscale client (real) + WireGuard egress (accurately-explained placeholder) + Headscale
    server (accurately-explained placeholder, not applicable to this device).

### Same session, later pass -- WireGuard built for real
- User's reaction to the WireGuard placeholder was blunt and direct: build it, don't just explain
  why it's hard. Correct call on WireGuard specifically -- the "needs a provider config" reasoning
  was true but didn't need to block building the *plumbing*; being too conservative there was a
  miscall, fixed this pass. Headscale-server is not reconsidered -- it targets a different machine
  entirely, not a matter of effort.
- The blocker that had actually stalled WireGuard turned out to be solvable: the systemd units that
  wire the 7 scripts together (`netns-vpn.service`, `wg-quick-vpn.service`, `vpn-heal.service` +
  `.timer`, the per-service `.service.d/vpn.conf` drop-in) aren't in the 7 script files themselves
  -- they're documented on the upstream wiki page
  (`github.com/jnovack/cloudkey/wiki/Phase-4-WireGuard`), which explicitly invites exactly this:
  implementing the design from the doc rather than guessing. Fetched that page and copied the unit
  definitions verbatim rather than inventing the orchestration order myself -- getting a kill-switch
  design wrong silently would be a bad place to guess.
- Shipped **WireGuard fail-closed egress** as a real Extra: uploads all 7 bundled Phase 4 scripts
  as siblings, substitutes their `<placeholder>` tokens via `sed` from a new 7-field Params form
  (namespace name pre-filled `vpn`, the pasted WireGuard provider config via the new `IsMultiline`
  field, provider DNS IP, ports to keep reachable, systemd service names to protect, provider
  connectivity-check URL + expected response text), writes the systemd units verbatim from the
  wiki, enables everything in the right order, restarts the protected services, and finishes by
  running the real `vpn-verify-netns.sh` check and printing pass/fail plainly rather than just
  declaring success. The orchestrator script itself is app-authored (no single upstream file does
  this assembly) but every placed script and every systemd unit is upstream/wiki content, unchanged
  except for the token substitution the wiki itself describes.
- `UPLINK` (the one placeholder needing this device's actual uplink interface) is auto-detected at
  runtime (`ip route show default`) rather than asked as a form field -- one less thing for someone
  who's "never used this before" to get wrong.
- Build verified clean (0 warnings/errors), all 7 `phase4-*.sh` embedded resources confirmed
  present via reflection, loose Debug build and the new self-contained single-file exe both
  smoke-tested (launch, no `crash.log`). Published as `publish-v18`.
- **Not live-tested against real hardware yet** -- no VPN provider config was available this
  session to actually run it end-to-end. The orchestrator, the sed substitutions, and the systemd
  unit content have all been checked carefully against the source scripts and the wiki verbatim,
  but "checked carefully" isn't the same as "watched it come up clean on the device" -- treat the
  first real run as the actual first test of this feature, and check its own verification output
  (it's designed to tell you plainly if something didn't come up right) rather than assuming success.
- Net result: Remote Access category is now AutoSSH + Tailscale client + WireGuard egress, all
  real. Headscale server remains the sole placeholder, and remains correctly out of scope (separate
  VPS, not the Cloud Key).

### Same session, later pass -- terminal copy/paste fix + FDT.Scout web console (user request: "build all of this in 1 shot")
- **Terminal copy/paste fixed.** The right-pane terminal log was an `ItemsControl` of plain
  `TextBlock`s -- WPF `TextBlock` text can't be selected or copied at all, which was the actual
  bug. Replaced with a read-only `RichTextBox` (`IsReadOnly` still allows selection + Ctrl+C, just
  blocks typing), fed from code-behind since a `FlowDocument`'s `Blocks` can't be data-bound
  directly -- mirrors the VM's existing `ObservableCollection<TerminalLine>` Add/RemoveAt(0) rolling
  cap into paragraphs rather than rebuilding the whole document per line. Same per-line coloring as
  before.
- **Built FDT.Scout: a real password-gated HTTPS web console for the Cloud Key**, per the user's
  explicit spec (browser login, user management, web terminal, SSL cert install/generate, front-
  panel text change, health metrics over time), in the Optional Extras section, on by default.
  App-authored from scratch (no upstream project for this) -- a new Go module at `tools/fdtscout/`,
  cross-compiled to a single static linux/arm64 binary (confirmed via the wiki that this hardware's
  SoC genuinely runs a 64-bit aarch64 userland, not 32-bit -- the LCD app's own 32-bit release
  binary is a choice of jnovack's, not a requirement of the hardware), embedded into the exe exactly
  like the runbook scripts (`ExtraItem` gained a new `BinaryUploads` mechanism for this -- raw bytes
  to an absolute remote path, since a compiled binary can't round-trip through the existing text-
  only script-upload path; `SshSessionService.UploadBinaryFileAsync` is the new SFTP primitive
  behind it).
  - **Auth**: username/password login, bcrypt-hashed, session cookie (in-memory only -- a service
    restart logs everyone out rather than needing a persisted signing key, a deliberate simplicity
    trade for an admin console used occasionally, not continuously).
  - **Web terminal**: a REAL PTY (`github.com/creack/pty`) over a WebSocket
    (`github.com/gorilla/websocket`), not a hand-rolled command box -- Ctrl+C/D, job control,
    interactive programs all work natively, unlike the Windows app's own "drop to shell" pane which
    has to fake Ctrl+C via raw bytes. Browser side renders raw bytes into a scrolling `<pre>` and
    strips ANSI CSI/OSC sequences rather than a full VT100 emulator -- same deliberate scope
    limitation as the Windows app's own shell pane, stated plainly in the UI.
  - **TLS**: self-generates an ECDSA P-256 self-signed cert on first boot if none exists (covers
    every local IP + hostname), swappable live via `tls.Config.GetCertificate` so "generate new" /
    "install your own" (multipart upload, validated to actually parse and match before ever
    touching disk) take effect on the next connection without a service restart.
  - **Front panel text**: turned out there's no separate free-text field on this hardware -- verified
    against jnovack/cloudkey's own README that the LCD's title is always the system hostname. Built
    honestly as "change the hostname" (via `hostnamectl`, restarts the LCD app if installed) rather
    than a fictional text field the firmware doesn't support.
  - **Health metrics**: CPU (from `/proc/stat` deltas)/mem (`/proc/meminfo`)/disk
    (`syscall.Statfs` on `/` and `/volume` if mounted) sampled every 60s, 7-day ring buffer,
    persisted to disk every 5 minutes so a restart doesn't lose much history. Rendered as hand-
    rolled canvas sparklines client-side -- no chart library, no CDN dependency (this is served by
    the device itself, possibly with no other internet-facing dependency at all).
  - **User management**: add/update/remove accounts from the Users tab; can't remove the last
    remaining account (would lock everyone out) or the account you're currently logged in as.
  - Frontend is vanilla HTML/CSS/JS, embedded via `go:embed`, no framework/CDN dependency --
    consistent with this whole project's "self-contained, nothing fetched at runtime" philosophy.
  - **"On by default"**: listed first in General, pre-filled sensible defaults (username "admin"),
    worded plainly as recommended -- but still requires the one Run click every Extra requires. Did
    NOT make it silently auto-execute without any click: that would be the first thing in this app
    to run without an explicit user action, and this specific Extra opens a real root-capable
    network-exposed HTTPS listener, a meaningfully bigger step than the other "on by default"-
    adjacent things in this app. Flagged this one deliberate scope choice rather than silently
    picking a side.
  - **Verified**: `go build` cross-compiles cleanly (`GOOS=linux GOARCH=arm64 CGO_ENABLED=0`), C#
    build clean (0 warnings/errors), embedded binary confirmed byte-exact via reflection
    (6,946,978 bytes matching the source file exactly), static frontend sanity-checked in a browser
    (login page renders correctly, dashboard's 5 tabs all present and tab-switching CSS/JS
    confirmed working via a real click, no JS console errors besides the expected "no backend here"
    network failures). Both the loose Debug build and the published exe smoke-tested (launch, no
    `crash.log`). Published as `publish-v19`.
  - **Not live-tested against real hardware** -- no Cloud Key session was available this pass to
    actually install it and click through Users/Terminal/Certs/LCD/Health live. See Queue #2.

### Same session, later pass -- FDT.Scout confirmed live + Apps/About tabs + LCD bug hardening + About/Changelog/disclaimer (user request: "Que this up... Do it, build it")
- **First real-world confirmation FDT.Scout actually works**: user installed it, logged in, and
  used the Front Panel tab against a real Cloud Key (screenshot). Everything up through that point
  -- install, auth, navigation, the LCD form itself -- genuinely works end-to-end, not just builds.
- **Bug reported**: hostname change reported success ("Hostname updated," `cloudkey.service`
  "installed, running") but the physical panel kept showing the UniFi logo, not the new hostname.
  No live device access to actually diagnose this directly (no `journalctl`/`ps` against the real
  box this pass) -- hardened the two most plausible causes instead of guessing at a fix blindly:
  - `setHostname` now sets the hostname three redundant ways (`hostnamectl`, a direct
    `syscall.Sethostname()` call, and a direct `/etc/hostname` write) instead of trusting
    `hostnamectl` alone -- plausible this stripped-down UniFi OS image doesn't have a fully
    functional `systemd-hostnamed`, in which case `hostnamectl` can silently write the file without
    touching the live kernel hostname.
  - Added real diagnostics surfaced directly in the Front Panel tab rather than requiring a shell:
    whether `ck-ui.service` (UniFi's *original* front-panel app) is still active -- if it is, it's
    very likely still holding the framebuffer regardless of what `cloudkey.service` is doing, which
    would fully explain this exact symptom -- plus a `journalctl -u cloudkey -n 15` tail.
  - **Explicitly not claimed fixed** -- flagged in QUE.MD as needing live confirmation, with
    instructions to check the new diagnostics first next time rather than assume it's the same
    unfixed bug.
- **Added an Apps tab to FDT.Scout**: status (installed/running/enabled) for every service or
  container CloudKey Wizard's Optional Extras can install -- a fixed, hand-verified catalog
  (`tools/fdtscout/apps.go`) cross-checked against the real unit/container names each bundled
  script actually uses (grepped the scripts directly rather than assuming standard names --
  confirmed `nzbget.service`/`sonarr.service`/`radarr.service`/`prowlarr.service`,
  `fail2ban.service`, `unattended-upgrades.service`, `plexmediaserver.service`,
  `tailscaled.service`, `wg-quick-vpn.service`, a Docker container named `homeassistant`, and
  AutoSSH's dynamically-named `autossh-tunnel-*.service` units handled via a pattern match since
  there can be more than one). Deliberately does NOT install anything new from the web UI -- for
  anything not installed, it says so and points back to CloudKey Wizard, rather than duplicating a
  dozen install scripts' worth of logic a second time in Go where the two copies would inevitably
  drift. What it DOES do for real: start/stop/enable/disable via a small fixed allow-list endpoint
  (`/api/apps/{id}/action`) that only ever touches a name from the server-side catalog, never a
  client-supplied unit string -- a deliberate security boundary on an already root-capable console.
- **Added an About tab to FDT.Scout** (`tools/fdtscout/version.go`, `handlers_about.go`): version
  (1.1.0), build date, the disclaimer/references text (jnovack/cloudkey, the original
  FullDuplexTech.com article this whole project started from), and an in-app changelog.
- **Added an About window and a View Changelog window to CloudKeyWizard.exe itself**
  (`Version.cs`, new `TextWindow.xaml(.cs)` -- a small reusable scrollable read-only text window,
  since a `MessageBox` can't scroll a growing changelog). About was previously a short
  `MessageBox`; now shows version (1.4.0), the same disclaimer/references content as FDT.Scout's
  About tab, and a link to the changelog. Versioned independently from FDT.Scout -- separate
  programs, separate release cadence, cross-referenced from each other's About text.
- **Added the liability disclaimer** queued last turn: the first-run "Before you start" overlay now
  also states "informational and convenience purposes only, as-is, no warranty, use at your own
  risk, not affiliated with or endorsed by Ubiquiti Inc." alongside the existing technical-risk
  warning (irreversible commands, no factory-revert path) -- additive, not a replacement.
- **Verified**: Go cross-build clean (confirmed the new code actually landed in the binary by
  grepping compiled strings, not just trusting an unchanged-looking file size), C# build clean (0
  warnings/errors) both before and after the About/Changelog/disclaimer additions, embedded
  fdtscout binary confirmed byte-identical to the freshly rebuilt source via `cmp`. Both the loose
  Debug build and the published exe smoke-tested (launch, no `crash.log`) at each stage. Attempted
  UI-Automation-driven clicking of Help -> About to verify the new window opens without a crash --
  the automation's reported click coordinates were clearly wrong (in the thousands of pixels, off
  any real screen position, almost certainly a DPI/multi-monitor coordinate-space mismatch in this
  environment's automation stack), so that attempt is inconclusive, not a real pass. Confidence in
  `TextWindow` not crashing rests on it being a small, standard WPF pattern (a plain owned dialog
  with a read-only `TextBox`) and on confirming every `DynamicResource` key it references
  (`BgWindow`, `TextPrimary`, `PrimaryButton`) actually exists in both theme dictionaries -- not on
  an actual click-through.
- Published as `publish-v20`.

### Same session, later pass -- FDT.Scout update-overwrite fix, session idle timeout, Dark Mode contrast fix (user request, verbatim: "BUILD THE NEXT VERSION, INCLUDE EVERYTHING IN THE QUE" + "session tracking when logged in needs to be better... time out after 5 min of idle time")
- User feedback, direct: not building queued items promptly enough was frustrating. Built everything
  actionable in the queue this pass rather than re-confirming or re-scoping first.
- **Fixed the FDT.Scout update bug** (root-caused from a terminal screenshot alone, no live device
  access): re-running the Extra to pick up a newer binary failed with `mkdir -p /opt/fdtscout` then
  a bare `Error: Failure`. `ex.Message == "Failure"` is exactly SSH.NET's message for a raw SFTP
  `SSH_FX_FAILURE` -- the upload was opening `/opt/fdtscout/fdtscout` directly for write while
  `fdtscout.service` was actively executing that exact file, which Linux refuses (ETXTBSY).
  `SshSessionService.UploadBinaryFileAsync` now uploads to a temp sibling path and atomically
  `mv -f`s it into place instead -- `rename()` doesn't need write access to the target inode's
  content, so this works regardless of what's currently executing the old one, and is strictly
  safer even when nothing's running (no partial-write window). Also switched the installer script
  from `systemctl enable --now fdtscout` to explicit `enable` + `restart`, since `--now` doesn't
  restart an already-active unit -- without this, even a successfully-swapped binary on disk
  wouldn't actually take effect until some later unrelated restart. Confirmed (by inspection, not
  live) that data preservation across an update was already correct and needed no code change --
  nothing in the install flow touches `/opt/fdtscout/data` or `/etc/fdtscout/tls`.
- **Added FDT.Scout session idle timeout**: `SessionStore` now tracks `LastActivity`, refreshed on
  every authenticated request, and expires a session after 5 minutes of inactivity -- plus a
  24-hour absolute cap regardless of activity (defense in depth against something keeping a session
  artificially alive). A periodic cleanup goroutine sweeps expired sessions from memory every
  minute (pure hygiene, `Validate()` itself always enforces expiry correctly between sweeps
  regardless). The frontend now wraps `window.fetch` once, globally, so ANY API call returning 401
  bounces to `/login` automatically -- rather than adding a 401-check to each of the ~15 scattered
  `fetch()` call sites individually and risking missing one.
- **Fixed the Dark Mode "Previously connected" dropdown contrast bug** (user screenshot): root
  cause was that `App.xaml`'s existing global `ComboBoxItem` style used plain property `Setter`s,
  which weren't enough to fully override the default WPF template's own hover/selection visuals
  underneath -- replaced with a full `ControlTemplate` override (one `Border` bound directly to an
  explicit background per state, no leftover system chrome) so there's nothing left for a stray
  system color to bleed through from. Also gave `MenuItem`'s highlight trigger an explicit
  `Foreground` pin (same underlying gap, smaller fix -- not escalated to a full template override
  since no live report has actually shown a Menu contrast problem, unlike the ComboBox).
- **Verified**: Go cross-build clean (confirmed new code landed via `strings | grep` on the
  compiled binary), C# build clean (0 warnings/errors) after each change, embedded fdtscout binary
  confirmed byte-identical to the freshly rebuilt source via `cmp`. Loose Debug build and the
  published exe smoke-tested (launch, no `crash.log`) at each stage.
- **Not live-tested against real hardware** -- same honest caveat as every FDT.Scout change so far
  this project: the upload-fix and restart-fix are reasoned through carefully (`rename()`'s
  semantics under an active exec image are well-understood POSIX behavior, not a guess) but
  genuinely running an update against a live install is still the real test. Same for the session
  idle-timeout and the ComboBoxItem template fix -- no click-through this pass.
- Published as `publish-v21`.

### Same session, later pass -- 5 new FDT.Scout items queued, clarifying questions asked and answered, 3 of 5 built (user asked "any questions on these? don't do work yet," then answered all 4 questions asked)
User asked for 5 fairly large FDT.Scout additions (real Apps installs, more Health metrics
including USB mounting + static IP, Users login history, Certificates port/redirect, an on-device
continuity file) and explicitly invited questions before building. Asked 4 targeted questions
(Apps install scope, what "USB mounting" means, how cautious the static-IP flow should be, whether
failed-login tracking should also lock accounts out) -- all four answered clearly, and building
started immediately after rather than sitting on the answers.

- **Shared on-device state file, built**: `fdtscout-state.json` under the existing
  `/root/.cloudkey-wizard/` audit-trail dir, read/written by both apps. New
  `Services/DeviceStateStore.cs` (C#) and `tools/fdtscout/devicestate.go` (Go) share the same JSON
  schema by hand (camelCase field names kept in sync manually -- no shared codegen between a C#
  and a Go project). `MainViewModel.ConnectAsync` now calls this after `RestoreExtrasFromKnownHost`
  and lets it override (device-authoritative); `PersistExtraStatus` now also calls
  `WriteDeviceStateAsync` (best-effort, never blocks/fails the local save it already does).
- **Users tab login history + lockout, built**: new `tools/fdtscout/authlog.go` -- every login
  attempt (success/fail, username, source IP) is logged and persisted (capped at 500 entries), a
  username locks out for 15 minutes after 5 consecutive failures (resets on success or if the gap
  between failures exceeds 15 min, so it's "5 in a row," not "5 ever"). `/api/auth-log` +
  `PublicUser.LastLogin` feed the new Users-tab history table and per-account last-login column.
  Lockout is enforced server-side in `handleLogin` before password comparison even runs, returning
  HTTP 429 with an approximate remaining-lockout time.
- **Health tab network throughput, built**: `Sample` gained `NetRxKBs`/`NetTxKBs`, computed the
  same delta-since-last-sample way as CPU% from `/proc/net/dev` (summed across every interface
  except `lo`), using actual elapsed wall-clock time between samples rather than assuming exactly
  `sampleInterval` (timer drift/skipped samples stay honest). `drawSparkline` on the frontend
  gained an auto-scaling Y-axis mode (`maxValue: null`) since throughput has no natural 0-100
  ceiling the way percentages do.
- **Certificates tab port + redirect, built**: new `RuntimeConfig` (`/etc/fdtscout/config.json`,
  port + redirectHttp) plus `/api/config` GET/POST. Changing it can't be applied live (rebinding a
  listening HTTPS socket without dropping every open connection isn't practical), so the POST
  handler saves the new config, responds first (explicitly flushed so the browser's fetch actually
  resolves), then schedules `systemctl restart fdtscout` after a short delay. The port-80 redirect
  (`startRedirectServer`) targets whatever HTTPS port is actually configured, not a hardcoded 443,
  so it keeps working after a custom port change.
- **Still open, deliberately paced rather than compressed into this pass** -- see Queue #0 for the
  full detail: Apps tab real installs (user chose full parity with the Windows app -- the largest
  remaining item, needs the same bundled scripts embedded into fdtscout too plus a Params-driven
  install UI mirroring `ExtraCatalog.cs`), USB drive mount **and** browse (user wants a real file
  browser, not just mount detection -- scope of write access not yet specified), and static
  IP/DHCP with the user's own carefully-designed apply-then-5-minute-timer-then-revert-or-confirm
  safety mechanism (the highest-risk item here -- still needs the device's actual networking
  backend identified, which neither the wiki nor any bundled script currently states).
- **Verified**: Go cross-build clean (confirmed via `strings | grep` on the compiled binary), C#
  build clean (0 warnings/errors), embedded binary confirmed byte-identical via `cmp`. Static
  frontend re-checked in the Browser pane after all the new UI (auth log table, network charts,
  port/redirect form) -- every new element present and structurally correct via `read_page`, no new
  JS errors beyond the expected missing-backend ones on this backend-less static preview. Both the
  loose Debug build and the published exe smoke-tested (launch, no `crash.log`) at each stage.
- **Not live-tested against real hardware** -- same standing caveat as every FDT.Scout change.
- Published as `publish-v22`.

### Same session, immediately after -- the 2 deferred items built, no further delay (user: "WHY ARE YOU NOT IMPLEMENTING WHAT I ASK YOU TO IMPLEMENT?!")
Called out directly for deferring the Apps-install and USB-browser items purely for size (not a
real constraint) right after committing to "build promptly." Answer given plainly, then built
immediately -- all 3 remaining items from Queue #0, no further pacing.

- **Static IP/DHCP, built to the user's own spec.** New `tools/fdtscout/netconfig.go`. Deliberately
  does NOT try to identify/edit netplan/ifupdown/systemd-networkd/NetworkManager config files --
  works directly against the live kernel routing table with `ip addr`/`ip route` instead (confirmed
  present via the bundled WireGuard scripts, sidesteps the unresolved "which backend" question
  entirely for the safety-critical live change). `Apply` snapshots current addresses/gateway
  (`ip -4 addr show` / `ip route show default`), switches mode, arms a 5-minute `time.AfterFunc`
  revert timer, and persists the pending state to `/opt/fdtscout/data/pending-ip-change.json` --
  **critically**, this is re-read on process startup and the timer re-armed for whatever time
  actually remains (or reverted immediately if the window already elapsed), so a service restart
  mid-window (e.g. from the Certificates tab's own self-restart, or a crash) can't silently drop
  the safety net. "Accept IP changes" is a full-screen splash checked on every authenticated page
  load (not gated to a specific tab), covering "presented at login" naturally since that's the
  first thing that runs after landing on the dashboard. Best-effort reboot persistence
  (`persistBestEffort`) only fires after a *confirmed* change and only for `/etc/network/interfaces`
  if present -- deliberately does NOT touch netplan/systemd-networkd config, since guessing wrong
  there only risks next boot (recoverable) but guessing wrong on the live path risks a lockout
  (not recoverable without physical access).
- **USB mount + real file browser, built.** New `tools/fdtscout/storage.go` (`lsblk -b -J`
  detection -- inherited the exact same JSON-number-vs-string SIZE fix
  `Services/BlockDeviceService.cs` already needed for this hardware's `lsblk` build; a partition's
  transport type is read from its *parent* disk since `lsblk` only reports `tran` at the disk
  level) + `files.go` (browsing strictly scoped to currently-mounted USB mount points --
  `resolveSafePath` re-validates the device is actually mounted on every request and confirms the
  resolved path still lives under that exact mount point after `filepath.Clean`+`Join`, not just a
  string check, closing off `..`-based escape attempts). Read-only for this pass (browse + download
  only) per the stated default -- no upload/delete/rename yet.
- **Apps tab real installs, built -- full parity, as the user specified.** New
  `tools/fdtscout/installcatalog.go` + `handlers_install.go`. Copied the exact same bundled script
  files CloudKey Wizard uses (`phase2-*`, `phase3-autossh-client.sh`, `phase4-*` x7,
  `phase5-01-tailscale-client.sh`) into `tools/fdtscout/scripts/` and embedded them there too --
  one source of truth per script, not a second hand-maintained copy. The 4 app-authored scripts
  (fail2ban/unattended-upgrades/Plex/Home Assistant) and the WireGuard orchestrator are copied
  verbatim from `Services/ExtraCatalog.cs` as Go string constants. Installs run locally (no SSH
  hop needed -- FDT.Scout is already root on the device) via a synchronous, blocking HTTP handler
  that writes the script to `/root/.cloudkey-wizard/` (same conventional path the SSH-driven flow
  uses) and runs it with `bash`, capturing combined output; a real live-streamed install (matching
  the terminal's WebSocket pattern) is a known nicety not built this pass, noted rather than
  hidden. Every install updates the shared on-device state file on completion, so status is
  immediately consistent with what the Apps tab's own status view shows. IDs deliberately match
  `apps.go`'s existing status-catalog IDs so each app is one row with both status and (if not yet
  installed) an inline install form, not two disconnected concepts.
- **Verified**: Go cross-build clean, confirmed the embedded scripts actually landed via
  `strings | grep` matching 7 distinctive filenames/strings in the compiled binary. C# build clean.
  Embedded binary confirmed byte-identical via `cmp`. The new install-form DOM construction
  (`buildInstallFormRow`, including the WireGuard config's special-cased multiline textarea) was
  exercised directly in the Browser pane with synthetic data and confirmed to build correctly with
  no errors -- the real `/api/install` endpoint itself couldn't be exercised without a live device,
  same limitation as every other FDT.Scout backend check this project. Both builds smoke-tested at
  each stage.
- **Not live-tested against real hardware** -- same standing caveat, most consequential for the
  static IP mechanism specifically. Recorded a standing lesson in private memory about not
  self-imposing pacing/deferral this user didn't ask for (see `feedback_queue_mode_build_promptly.md`).
- Published as `publish-v23`.

### Same session, immediately after -- `top`-style process view on the Health tab (user request)
- New `tools/fdtscout/processes.go` + `handlers_processes.go`: `GET /api/processes` returns a
  `top`-style snapshot -- load average (`/proc/loadavg`), uptime (`/proc/uptime`), memory used/total
  (`/proc/meminfo`), process count, and the top 25 processes by CPU via `ps -eo
  pid,user,%cpu,%mem,rss,etime,comm --sort=-%cpu` (shelled out to `ps` rather than hand-computing
  per-process CPU% from `/proc/<pid>/stat` deltas -- `ps`'s own %CPU is the same figure `top` itself
  shows, no reason to reimplement that math a second time for a feature explicitly meant to feel
  like `top`).
- Frontend: a process table on the Health tab, auto-refreshing every 3 seconds while that tab is
  open (a checkbox to turn it off), explicitly stopped when navigating to any other tab so it
  doesn't keep polling in the background.
- Verified: Go build clean (confirmed via `strings | grep`), C# build clean, embedded binary
  byte-identical via `cmp`, new Processes section confirmed present and structurally correct via
  `read_page` in the Browser pane, both builds smoke-tested. Not live-tested against real hardware.
- Published as `publish-v24`.

### Same session, immediately after -- process Kill button, Health-tab system specs, real AppArmor root-cause + fix attempt for the front-panel bug, and corrected authorship in About
- **Process Kill button**: each row on the Processes table gets a Kill action (SIGTERM via a
  direct `syscall.Kill`, not shelling out to `kill(1)` -- no argument-injection surface). Server-
  side guardrails refuse PID 1 (init -- would take the whole system down) and this binary's own
  PID (self-signaling mid-request would just leave the console half-dead; `systemctl stop
  fdtscout` on the Apps tab is the clean way to stop the console itself). Everything else is
  allowed -- this is already a root-capable console, not a candidate for a "safe to kill"
  allow-list.
- **System specs on the Health tab**: CPU model + core count (`/proc/cpuinfo`, deliberately tries
  `model name`/`Hardware`/`Model` in that order since ARM boards frequently leave the x86-centric
  `model name` field empty), total RAM, total disk (`/` and `/volume` if mounted), kernel version
  and architecture (`uname -r`/`-m`), hostname -- shown once at the top, distinct from the
  sampled-over-time charts below it.
- **Front-panel bug: real root cause found from user-supplied logs, not guessed.** The user's
  actual `journalctl -u cloudkey` output (screenshot) showed `open /dev/fb0: operation not
  permitted` -- and fetching the real `cloudkey.service` unit file confirmed it runs as root with
  *no* device-cgroup restrictions (`DeviceAllow=`, `PrivateDevices=`, etc. all absent). Root being
  refused something plain file permissions would allow is the textbook signature of a mandatory
  access control layer (AppArmor/SELinux) blocking it, not a permissions bug -- and not the earlier
  "ck-ui still holding the framebuffer" guess, which this log rules out (no ck-ui involvement in
  the failure at all). Built `checkApparmorDenial` (scans `journalctl -k` / `dmesg` for a DENIED
  entry naming fb0/event1/cloudkey, extracts the exact `profile="..."` value) and a scoped
  "Attempt fix" action that sets *only that one profile* to complain mode (`aa-complain`, logs but
  allows -- deliberately not a system-wide AppArmor disable) and restarts `cloudkey.service`.
  Reports plainly if `aa-complain` isn't installed rather than attempting a riskier workaround.
- **About corrected**: built by Jay Anderson of FullDuplexTech.com -- the actual author, previously
  unstated. jnovack/cloudkey is credited plainly as the upstream project this app automates (real,
  full credit for that underlying research/tooling), not as the author of this app -- both apps'
  About sections and the README's opening now say this explicitly rather than leaving it
  ambiguous.
- **Verified**: Go build clean (confirmed new code landed via `strings | grep` despite a
  coincidentally-unchanged binary size -- same lesson as earlier this session, checked anyway
  rather than assumed), C# build clean, embedded binary byte-identical via `cmp`. The AppArmor fix
  button, kill function, and sysinfo loader were all exercised directly in the Browser pane with
  synthetic data and confirmed to render/exist correctly. Both builds smoke-tested.
- **Not live-tested against real hardware** -- most consequential for whether the AppArmor fix
  attempt actually resolves the display issue in practice, which needs to be watched on the real
  device to know for sure.
- Published as `publish-v25`.

### Same session, next day -- the full step-by-step refactor review, built (user: "this next version - include all items in que")
Every agreed decision from the 2026-08-23 step-by-step walkthrough shipped in one pass:

- **The front panel display works now.** It shows the hostname and IP, drawn directly by FDT.Scout
  (`tools/fdtscout/framebuffer.go`, `lcddisplay.go`) rather than depending on a separate app that
  was never rendering on this hardware -- confirmed by the user across every attempt this whole
  project. "Replace Front-Panel LCD App" is no longer a separate wizard step; it just works once
  FDT.Scout is installed, and updates live whenever the hostname changes. Pure Go, no cgo (kernel
  framebuffer ioctl structs hand-defined and sanity-checked at every read, rather than trusted
  blindly, given this can't be tested against real hardware from here) -- supports 16bpp RGB565 and
  32bpp XRGB8888, logs plainly and keeps the HTTPS console running normally if the display isn't
  available at all. Real diagnostics (kernel-log-confirmed, not guessed) plus a one-click scoped
  fix attempt remain on the Front Panel tab for the specific failure mode found live; a legacy
  install from before this change is detected and stopped automatically rather than left to
  conflict with the new display code.
- **Install Base Tooling** now installs a more complete toolkit -- curl, wget, git, nano, unzip,
  ca-certificates, tmux, htop, alongside rsync/nfs-common.
- **Format & Mount Storage** rebuilt, preserving the one genuinely load-bearing detail: mounts
  persist via a systemd `.mount` unit, never `/etc/fstab` (this hardware's vendor bootup-hook
  framework silently resets fstab to a template on every boot -- confirmed by direct testing
  against this exact hardware, verified again before rebuilding rather than assumed).
- **Remove Orphaned Accounts** is no longer its own step -- it now runs automatically right after
  Remove UniFi App Layer succeeds, since the accounts only become orphaned because of what that
  step just did. Same account list, same per-account safety guards (running process, unlocked
  password, authorized_keys, still-recreated-by-an-installed-package all skip removal).
- **Remove UniFi App Layer**: the confirmation screen now shows a live preview of exactly which
  packages on *this* device are about to be removed (a quick read-only `dpkg -l` check, not a
  guess), instead of requiring a separate manual Simulate click first. A package-list snapshot is
  also captured automatically before Apply runs, as a safety record.
- **Harden Access** now explains directly on its own page what it actually does (creates a
  `cloudkey` user, key-only SSH, locks password logins) so Skip vs. Apply is an informed choice.
- **Extras: sensitive fields are masked now.** FDT.Scout's admin password and a pasted WireGuard
  config were rendering in fully visible plain text -- both now show masked by default with a
  Show/Hide toggle (`PasswordBoxHelper.cs`, a small reusable attached-property binding since these
  come from a dynamic per-Extra list, not a fixed field like the app's other password boxes).
- **About corrected**, both apps: authorship, references, disclaimer all accurate.
- **Verified**: Go cross-build clean (`go vet` also run against the real linux/arm64 target this
  time, not just the native Windows one, which had produced false positives earlier this session --
  clean both ways), confirmed the new framebuffer code actually landed in the binary via
  `strings | grep`. C# build clean (0 warnings/errors) through several rounds of changes. Embedded
  binary confirmed byte-identical via `cmp`. Both loose Debug and published builds smoke-tested
  (launch, no `crash.log`) at each stage.
- **Not live-tested against real hardware** -- true of everything in this pass, most consequentially
  the framebuffer driver itself (the struct layouts are hand-verified against the known kernel ABI
  and defensively sanity-checked at runtime, but "checked carefully" isn't "watched it draw on the
  actual panel").
- Published as `publish-v26`.

### Same session, next day -- front panel screens/widgets + Settings tab (user asked "where's the setup for the front display," then requested this directly, "do it -- build it, do entire que")
The previous pass folded the LCD replacement into FDT.Scout but left it with no configuration
surface at all -- it just started automatically. The user noticed ("Wheres the setup for the front
display?") and then asked for a real one: multiple user-defined screens, each holding whatever
metrics the user wants, plus pulling hostname-editing out of the Front Panel tab into a dedicated
Settings tab alongside other system-identity controls.

- **Front Panel tab rebuilt around user-defined screens/widgets.** New `displayconfig.go`: a
  `DisplayConfig` (enabled toggle, cycle-interval seconds, a list of `Screen`s, each up to 4
  `Widget`s) persisted to `/etc/fdtscout/display-config.json`. Widget types: hostname, IP, time,
  CPU%, mem%, disk (/ and /volume), uptime, or a fully custom text line -- each with an optional
  label. `RenderWidget` evaluates a widget against the live values the Health tab already collects
  (reuses the existing `MetricsCollector`, nothing recomputed twice). Default config (shipped
  on-by-default, per the user's request) is 2 screens: hostname/IP/time, then CPU/mem/disk.
  `lcddisplay.go`'s cycle loop re-reads the config every tick (cheap) rather than needing an
  explicit reload signal, so a save takes effect on the very next cycle without a service restart.
- **New Settings tab** consolidates what used to be scattered: hostname (moved out of Front Panel --
  it now just says what it affects, doesn't do the changing), IP/DHCP (moved out of Health), plus
  two genuinely new controls: **DNS** and **NTP (time sync)**. Both are new `netsettings.go` +
  `handlers_settings.go`: NTP detects `systemd-timesyncd` and reads/writes its config plus
  `timedatectl`; DNS detects `systemd-resolved` (via `resolvectl`) and falls back to a direct
  `/etc/resolv.conf` rewrite if that's not active -- same "detect the actual mechanism, don't guess
  which config system this image uses" discipline as the existing static-IP feature. Lower risk
  than static IP on purpose: a bad NTP server just means clock drift, and a bad DNS server affects
  only the *device's* own outbound lookups, not the browser's ability to reach this console by IP --
  so neither gets static IP's elaborate apply-then-revert safety mechanism, by the user's own
  explicit call ("acceptable risk").
- **Front Panel tab keeps its existing diagnostics** (AppArmor denial detection + one-click scoped
  fix, legacy-cloudkey-service detection, recent log) unchanged underneath the new screens editor --
  this pass added configuration on top, it didn't touch the parts already confirmed working.
- Frontend: a screens/widgets editor (add/remove screens with no hard cap, each up to 4
  add/remove/reorder-free widget rows, a type dropdown plus optional label and custom-text fields),
  an approximate live preview panel per screen (rendered from the same sysinfo/metrics/network data
  already being fetched for the page, not a separate round-trip), and the new Settings tab's four
  forms (hostname, IP/DHCP relocated verbatim, DNS, NTP) -- all vanilla JS, no new dependencies.
- **Also built from the still-open queue**: the Purge step's **batch progress indicator**
  (`MainViewModel.PurgeBatchProgress`) -- see the Queue section above for detail. Small, but part of
  "do entire que" for this pass.
- **Verified**: Go cross-build clean (`GOOS=linux GOARCH=arm64 CGO_ENABLED=0`) and `go vet` clean
  against the same real target. C# build clean (0 warnings/errors) after fixing one more `--` in a
  new XAML comment -- a recurring gotcha in this codebase, caught immediately by the build rather
  than shipped. Embedded fdtscout binary rebuilt with the project's standard `-ldflags="-s -w"` (the
  first build of this pass used a plain `go build` and came out ~3MB larger purely from missing that
  flag -- caught by comparing against the previous binary's size before assuming anything was wrong,
  fixed by rebuilding with the documented command in `Scripts/fdtscout/README.md`) and confirmed
  byte-identical between the freshly built source and what got embedded via `cmp`. Both the loose
  Debug build and the published exe smoke-tested (launch, no `crash.log`). Static frontend
  re-checked in the Browser pane: nav intact, Front Panel and Settings panels both render every new
  element (toggle, cycle-interval field, screens editor, Add screen, Save, diagnostics; hostname/IP/
  DNS/NTP forms) with no new JS errors beyond the expected missing-backend 404s on this backend-less
  static preview.
- **Not live-tested against real hardware** -- same standing caveat as every FDT.Scout change this
  whole project. DNS and NTP specifically have never been exercised against this device's actual
  config layout; treat the first real save on each as the actual first test.
- Published as `publish-v27`.

### Same session, immediately after -- masked-password bug fix + timezone dropdowns (user report + request: "If you have all the info you need - build it. Do all items in que.")
User reported a real bug from a live screenshot: typing the FDT.Scout admin password into the
masked field on the Extras step silently didn't work (Run kept saying "Enter a value... first"
even after typing one) -- but worked once "Show" was clicked to reveal it as plain text. Also asked
for a timezone dropdown in two places: the "Set timezone to UTC" Extra, and FDT.Scout's own new
Settings tab.

- **Masked-password bug found and fixed.** Root cause: `PasswordBoxHelper.BoundPassword` (the
  attached property added earlier this session so a per-Extra-Param `PasswordBox` could bind at
  all) was registered with a plain `PropertyMetadata`, not `FrameworkPropertyMetadata(...,
  FrameworkPropertyMetadataOptions.BindsTwoWayByDefault, ...)`. Without that flag, and with the
  XAML binding itself not specifying `Mode=TwoWay` either, WPF's implicit binding mode for a custom
  attached property defaults to OneWay -- so typing into the box *did* update the DependencyProperty
  locally (via the `PasswordChanged` handler), it just never propagated back up to the bound
  `ExtraParam.Value`, which is exactly the "typed it, but Run says empty" symptom reported. This is
  a well-known, easy-to-miss WPF gotcha for exactly this kind of helper (only a handful of built-in
  properties like `TextBox.Text` default to two-way). One-line fix: register with
  `FrameworkPropertyMetadataOptions.BindsTwoWayByDefault`. Confirmed the revealed-TextBox path
  worked all along because ordinary `TextBox.Text` *does* default to two-way -- consistent with
  exactly what the user observed.
- **Timezone dropdown, both apps.** CloudKeyWizard.exe: `ExtraParam` gained `IsDropdown`/`Options`
  (a new mutually-exclusive display state alongside plain/masked/multiline, rendered as a
  `ComboBox`), and the old "Set timezone to UTC" Extra is now "Set timezone" with a `TIMEZONE` env
  var Param, defaulting to UTC, offering a curated ~50-zone list covering every major region/UTC
  offset. Curated rather than live-queried deliberately -- Extras render all at once on one page
  rather than one-at-a-time like the wizard steps, so there's no natural "just selected this" moment
  to trigger an SSH round trip the way `LoadPurgePreviewAsync`/`LoadBlockDevicesAsync` do for their
  own steps. FDT.Scout's Settings tab got the same control done the *better* way instead, since it's
  a natural fit there: a new `netsettings.go` `listTimezones()` shells out to `timedatectl
  list-timezones` on the device itself (it already runs as root there) for the real, complete list
  rather than a guess, validates a submitted zone against that same live list before ever calling
  `timedatectl set-timezone` with it.
- **Verified**: Go build + `go vet` clean against the real linux/arm64 target, confirmed the new
  timezone code actually landed in the compiled binary via `strings | grep` (the binary's size
  happened to come out byte-identical to the previous build, which would have been easy to
  misread as "nothing changed" -- checked anyway rather than trusted). C# build clean (0
  warnings/errors). Embedded fdtscout binary confirmed byte-identical between the freshly built
  source and what got embedded via `cmp`. Both loose Debug and published exe smoke-tested (launch,
  no `crash.log`). Static frontend re-checked in the Browser pane: Settings tab's new Timezone
  section present and structurally correct, no new JS errors.
- **The password fix specifically has NOT been re-verified against a live click-through** -- the
  root cause is a well-understood, unambiguous WPF binding-mode gotcha (not a guess at a fuzzy
  cause), and the fix is the standard one-line correction for it, but "diagnosed correctly from a
  screenshot" isn't the same as "watched it actually save a password now." Worth a quick
  confirmation next time this page is open. Timezone dropdowns share the standing "not live-tested
  against real hardware" caveat like everything else in FDT.Scout.
- Published as `publish-v28`. CloudKeyWizard now 2.2.0, FDT.Scout now 1.9.0.

### Same session, immediately after -- BOTH fixes above turned out incomplete, found from user re-test screenshots, actually fixed this time
User re-tested `publish-v28` and reported (screenshot) the exact same "Enter a value... first" symptom on the password field, PLUS a second bug: the new Timezone dropdown rendered completely blank/unreadable.

- **Password field, second and actual root cause.** The 2.2.0 fix (`FrameworkPropertyMetadataOptions.BindsTwoWayByDefault`) was real and necessary but not sufficient -- there was a SECOND, independent bug stacked in the same 15-line helper: `PasswordBox_PasswordChanged` pushed the typed value via `DependencyObject.SetValue()`, which sets a genuine local value on a property that ALSO has an active Binding -- this effectively supersedes/disconnects the Binding's own value source, silently breaking the TwoWay push regardless of Mode. Fixed by switching to `SetCurrentValue()`, WPF's purpose-built API for "push an interaction-driven update through a bound property without disturbing the Binding" -- the same one every built-in bindable control (Slider, CheckBox, etc.) actually uses internally for exactly this. Both bugs needed fixing together; either alone would have left it broken.
- **Timezone dropdown (and every ComboBox app-wide) rendering blank -- same bug class as the ComboBoxItem contrast fix from earlier this session, just on the outer control.** `App.xaml`'s `ComboBox` style only had plain property Setters (Background/Foreground/BorderBrush), which the ComboBoxItem fix's own comment already correctly identified as insufficient for this exact reason -- the default ControlTemplate's own "closed box" chrome pulls system/OS colors regardless of those setters, but that fix was only ever applied to ComboBoxItem, not to ComboBox itself, since nothing had shown a problem there before. Gave `ComboBox` the same full `ControlTemplate` override treatment: an explicit `Border` for background, a `ContentPresenter` wired directly to `Foreground` for the selected-item display (the default template's own selection part doesn't reliably inherit it), a borderless `ToggleButton` for the drop-down arrow, and a `Popup` reusing the already-correct `ComboBoxItem` template for the open list.
- **Actually verified visually this time**, not just by inspection -- captured real screenshots of the running app: `PrintWindow` targeting the app's own window handle (not a screen-coordinate grab, so no risk of catching unrelated desktop content) to confirm no crash and see the Connect page; then, since WPF `Popup`s are separate top-level windows PrintWindow can't reach, a second capture using a screen region tightly bounded to just the dropdown's own footprint (not a full-desktop grab) after opening the existing "Previously connected" host list (real saved host data, not synthetic). Confirmed directly: the dropdown that used to render blank now shows fully readable text ("10.0.100.19 (root, password) — last connected...") against the app's normal dark background -- the actual bug class reported, fixed and seen fixed, not just reasoned about.
- **The password fix is still not re-verified this same way** -- confirming it needs the Extras step, which needs a live device connection this environment doesn't have. The ComboBox fix, by contrast, WAS directly confirmed via screenshot this pass, a meaningfully stronger form of verification than everything else in this file's long string of "not live-tested" caveats.
- Published as `publish-v29`. CloudKeyWizard now 2.3.0 (FDT.Scout unchanged at 1.9.0 -- this pass was CloudKeyWizard.exe-only, no Go changes).

### Same session, immediately after -- the password field STILL broken (user live-tested v29 too), actual root cause finally found and structurally fixed
User connected to the real device, opened v2.3.0's About to confirm the build, then tried the password field again -- same exact "Enter a value... first" symptom, screenshot evidence again. Both prior fixes to `PasswordBoxHelper` (BindsTwoWayByDefault, then SetCurrentValue) were each individually correct, real WPF fixes for real problems -- and STILL didn't resolve it, which was the actual tell that the bug wasn't in that helper class at all.

- **Actual root cause, found by re-reading the whole shared DataTemplate rather than the helper class again.** Every `ExtraParam`'s plain TextBox, dropdown ComboBox, masked PasswordBox+Show button, revealed TextBox+Hide button, and multiline TextBox were ALL instantiated together in one shared `DataTemplate`, with only `Visibility` toggling which one showed -- meaning all five stayed bound (several TwoWay) to the exact same `ExtraParam.Value` simultaneously, even while Collapsed. The password field's ComboBox sibling -- bound to an empty `Options` list, since only dropdown params ever set one -- has no possible match for whatever gets typed into a DIFFERENT sibling control, and `ComboBox.SelectedItem`'s well-documented behavior in that situation is to silently reset to null and, because the binding is TwoWay, push that null straight back into the shared source property -- wiping out whatever had just been typed into the PasswordBox a moment before it could ever be read. This is exactly why fixing `PasswordBoxHelper` alone could never have worked: the helper was pushing correctly, something else immediately clobbered it right back.
- **Structural fix, not another patch to the same helper.** Split the one shared `DataTemplate` into four separate ones (`PlainParamTemplate`/`DropdownParamTemplate`/`SecretParamTemplate`/`MultilineParamTemplate`), each containing ONLY the control(s) relevant to that param, and added a new `ExtraParamTemplateSelector : DataTemplateSelector` (picks by `ExtraParam`'s fixed `IsMultiline`/`IsDropdown`/`IsSecret` flags) wired via the `ItemsControl`'s `ItemTemplateSelector`. A secret param's template still contains both the masked PasswordBox and the revealed TextBox together (that pairing is intentional and safe -- both genuinely represent the same field, neither has ComboBox's "no match resets to null" behavior), but the ComboBox itself now exists ONLY inside `DropdownParamTemplate`, for params that are actually dropdowns. This removes the interference at its structural root instead of continuing to guess at WPF's exact binding-resolution ordering.
- **Added a cheap diagnostic safety net regardless of confidence in the fix**: the "Enter a value... first" message now appends the field's actual current character count (`(currently: 0 chars)`), so if this SOMEHOW still fails, the next report tells us immediately whether Value is still genuinely empty (propagation still broken somewhere) versus something else entirely (e.g. a different Param than expected) -- real signal instead of another guess from "same thing."
- **Verified**: C# build clean (0 warnings/errors) after fixing yet another `--`-in-XAML-comment instance (now a well-worn, expected step in this codebase, not a surprise). Loose Debug and published exe both smoke-tested (launch, no `crash.log`). **Not re-verified live against the actual Extras page** -- same limitation as before, this environment has no live device to connect to. This is genuinely the strongest candidate root cause found so far (it explains why BOTH previous real fixes still failed, which the previous two explanations never accounted for), but "explains the prior failures" isn't the same as "confirmed fixed" -- worth a clean re-test.
- Published as `publish-v30`. CloudKeyWizard now 2.4.0.

### Same session, immediately after -- STILL broken (user confirmed live, third time), stopped guessing, shipped hard instrumentation instead
User connected to the real device again, retested with 2.4.0, and got the exact same result -- this time with the new diagnostic detail visible: `(currently: 0 chars)`, proving Value genuinely is still empty at click-time. Three straight fixes (BindsTwoWayByDefault, SetCurrentValue, the DataTemplateSelector restructure) were each real, defensible engineering -- and none of them were confirmed to actually be the cause; each was reasoned through from the code rather than proven from evidence. Continuing to ship a fourth theory the same way would be the same mistake a fourth time.

- **Deliberately did NOT ship another fix this pass.** Added `DebugTrace` (a tiny static message bus any static/model-layer class can call into) wired to the existing terminal pane -- the same pane the user has been screenshotting every time already, so no new artifact for them to go find. Instrumented every point in the chain with `[debug]`-prefixed lines: `ExtraParam.Value`'s setter (object identity hash + old/new length, never actual content), both `PasswordBoxHelper` callbacks (`OnBoundPasswordChanged`, `PasswordBox_PasswordChanged`, including a `BindingOperations.GetBindingExpression` check right after the `SetCurrentValue` push to confirm the binding itself is still actually attached and its `Status`), and `RunExtraAsync`'s validation step (every Param's identity hash + length at the moment Run is clicked).
- **What this is actually testing**: whether the PasswordBox's own change handler ever fires at all; whether the value it computes ever reaches `ExtraParam.Value`; whether the *same* `ExtraParam` instance that received the typed value is the one `RunExtraAsync` reads back (an object-identity mismatch, e.g. from some rebuild elsewhere replacing `Extras` with fresh instances, would be an entirely different bug than anything fixed so far and wouldn't be visible without exactly this kind of identity tracking) -- covers every plausible remaining explanation with real, direct evidence instead of another guess.
- **Verified**: C# build clean, loose Debug and published exe both smoke-tested (launch, no `crash.log`). No functional fix in this build -- purely diagnostic, and said so plainly rather than passing it off as another attempted resolution.
- Published as `publish-v31`. CloudKeyWizard now 2.4.1. **Next step is squarely on real evidence**: type into the password field once more, click Run, and share the `[debug]`-prefixed terminal lines that appear -- that trace should make the actual cause unambiguous rather than needing another round of reasoning-from-the-code.

### Same session, immediately after -- the trace paid off: real root cause found from hard evidence, actually fixed
User tested `publish-v31` and shared the terminal screenshot: `[run]` lines showed `ADMIN_PASSWORD#...len=0` on every click, exactly as before -- but critically, **zero `[pwbox]` lines appeared anywhere**, across three separate test clicks. That absence *is* the finding.

- **Actual root cause.** `PasswordBoxHelper` attached its `PasswordChanged` handler lazily, from inside `BoundPassword`'s own `PropertyChangedCallback` (`OnBoundPasswordChanged`). WPF does not reliably invoke that callback when a freshly-applied binding's initial value equals the property's declared default -- and `ExtraParam.Value` starts as `""`, matching the DP's own default of `""`. So the callback never fired, even once, for the entire life of the app -- meaning the code that was supposed to bootstrap keystroke handling never ran. This fully explains why all three previous fixes (`BindsTwoWayByDefault`, `SetCurrentValue`, the `DataTemplateSelector` restructure) individually did nothing: none of that code was ever reachable in the first place. The dots shown while typing were always just `PasswordBox`'s own native rendering, entirely independent of any of this.
- **Fix**: replaced the lazy self-attaching design with `EventManager.RegisterClassHandler(typeof(PasswordBox), PasswordBox.PasswordChangedEvent, ...)`, registered once in a static constructor -- fires unconditionally on the very first real `PasswordChanged` event for every `PasswordBox` in the app, sidestepping the "does the callback fire on an unchanged initial value" question entirely rather than depending on it. Guarded with `box.ReadLocalValue(BoundPassword) == DependencyProperty.UnsetValue` so this app-wide handler correctly no-ops for the 3 pre-existing fixed-instance PasswordBoxes (Connect, New/Confirm password) that never touch this attached property at all.
- **This is the first fix in this whole saga actually derived from hard evidence rather than reasoning about the code** -- the `[debug]` tracing added the pass before is what made it possible; kept in place (now expected to show real activity) rather than stripped back out, in case anything else surfaces.
- Verified: C# build clean, both loose Debug and published exe smoke-tested (launch, no `crash.log`, confirmed still running after 6s not just an instant-launch check). A secondary regression check (typing into the pre-existing Connect-page password field via `SendKeys` + a `PrintWindow` capture) was attempted but the capture itself failed for tooling reasons (a fresh PowerShell process, `SendKeys` targeting timing) -- not treated as a real signal either way; the `ReadLocalValue` guard is a direct, provable no-op for that box regardless.
- Published as `publish-v32`. CloudKeyWizard now 2.5.0. **Still needs the user's live confirmation** -- high confidence this time specifically because it's the first explanation backed by hard evidence instead of code review, but "explains the trace" isn't "watched it work" until confirmed.

### Same session, immediately after -- STILL broken on 2.5.0 too, screenshot ambiguous (couldn't tell if [pwbox] lines existed above the visible crop), consolidated all diagnostic signal into the one line already being screenshotted
User tested `publish-v32` live -- same "Enter a value... first (currently: 0 chars)" result. The screenshot only showed the bottom of the terminal scrollback (the `[run]` lines), so it was impossible to tell whether the `[pwbox]` trace lines had fired further up and simply scrolled out of the visible crop, or genuinely never fired at all -- a real ambiguity, not resolved by asking for another screenshot of the same kind.

- **Consolidated the diagnostic into the failure message itself**, rather than depending on terminal scrollback position. `PasswordBoxHelper` now exposes two running counters: `ClassHandlerFireCount` (incremented on every `PasswordChanged` event the class handler receives, for ANY PasswordBox in the app) and `BoundPasswordChangedCount` (incremented only when that event was for a box that actually has `BoundPassword` set). The "Enter a value... first" message now appends both: `(currently: N chars, pwFires=N, pwMatched=N)`. This turns three previously-separate hypotheses into three distinguishable single-number outcomes from ONE screenshot of the one thing the user already captures every time:
  - `pwFires=0` -- the class handler mechanism itself never received an event for any PasswordBox all session (points at handler registration itself, or that no PasswordBox anywhere was ever typed into -- unlikely but checkable).
  - `pwFires>0, pwMatched=0` -- it's firing, but never for a box that has `BoundPassword` actually set (points at `ReadLocalValue`, or the XAML binding itself not applying as expected).
  - `pwFires>0, pwMatched>0, chars=0` -- the handler runs and matches correctly, but the value still never reaches `ExtraParam.Value` -- a third, different failure mode from anything considered so far.
- **Explicitly diagnostic-only again, no new fix attempt this pass** -- said so plainly in the changelog and to the user, consistent with the standing lesson in `feedback_verify_before_claiming_fixed.md`.
- Verified: C# build clean, both loose Debug and published exe smoke-tested (no `crash.log`).
- Published as `publish-v33`. CloudKeyWizard now 2.5.1. **Waiting on one more screenshot of just the failure message** -- should be unambiguous this time regardless of scroll position.

### Same session, immediately after -- user found a better verification channel (pasted terminal TEXT, not a screenshot), which resolved the ambiguity and pointed at a new specific suspect
User pasted the actual terminal text (copy/paste, not a screenshot) around a Run click on `publish-v33`. This is strictly better evidence than any screenshot so far -- no cropping, no scroll-position ambiguity. Result: the `[run]` lines are present and correct (proving `DebugTrace`'s pipe into the terminal works end-to-end), but there are still **zero `[pwbox]` lines anywhere** in the complete pasted text. Combined with `[run]` working, this rules out "the trace lines exist somewhere off-screen" for good -- the `[pwbox]` code path genuinely never completes far enough to log anything.

- **New specific suspect, testable this pass:** `OnPasswordChanged`'s guard (`box.ReadLocalValue(BoundPassword) == DependencyProperty.UnsetValue`) increments `ClassHandlerFireCount` before checking, but the very next `[pwbox]` log line only happens AFTER that guard passes -- so if `ReadLocalValue` isn't reliably detecting the attached-property Binding on a templated child the way `BindingOperations.GetBindingExpression` is specifically built to, the guard could be silently returning early on every keystroke, explaining zero `[pwbox]` lines even with the class handler itself firing correctly.
- **Swapped the guard to `BindingOperations.GetBindingExpression(box, BoundPassword) is null`** -- the canonical, purpose-built API for exactly this question, more likely correct than the more general-purpose `ReadLocalValue` for this specific templated-attached-property scenario. Added an unconditional trace line right before the (now-changed) guard decision, logging both `readLocalValueIsUnset` and `bindingExpressionPresent` side by side for every PasswordBox that fires at all -- this will either explain the whole mystery on the next test, or definitively rule this theory out too.
- **Mirrored the Run-failure diagnostic into the terminal text itself** (`DebugTrace.Log` right alongside setting `item.Detail`), not just the on-screen label next to Run -- since the user's own pasted-text approach turned out more reliable than a screenshot, made sure the SAME data is available that way going forward too.
- Verified: C# build clean, both loose Debug and published exe smoke-tested (no `crash.log`).
- Published as `publish-v34`. CloudKeyWizard now 2.5.2. **Still diagnostic-leaning, not a confirmed fix** -- the `GetBindingExpression` swap is a real, targeted change based on this pass's evidence (not a blind guess), but only the next test confirms whether it's the actual answer.

### Same session, immediately after -- CONFIRMED FIXED (real live test, complete trace pasted as text), plus a real security bug the fix itself exposed
User pasted the complete terminal text from a real Run attempt on `publish-v34`. Unambiguous this time: `[pwbox]` traces fired on every keystroke, `ADMIN_PASSWORD`'s length climbed 0->13 matching what was typed, `[run]` validation passed, and the Extra actually ran and succeeded (`OK: login page responded 200.` -- FDT.Scout genuinely came up). **The `GetBindingExpression` guard swap was the real fix.** `ReadLocalValue` was not reliably reporting the attached-property Binding for this specific templated-child scenario; `BindingOperations.GetBindingExpression` is the purpose-built API for the same question and it worked.

- **A second, real, separate bug surfaced in that same successful log**: the command-echo line for the Extra's run showed the actual password in plain text --
  `ADMIN_USERNAME='admin' ADMIN_PASSWORD='P@ckard2k%123' bash /root/.cloudkey-wizard/extra-fdtscout.sh` --
  meaning it was visible in the terminal pane AND in this chat conversation once pasted back. Every other password-touching flow in this app (chpasswd via `RunSimpleAsync`'s `displayOverride`) already redacts this; the Extras' generic env-prefix path never got the same treatment. **User should change that FDT.Scout admin password**, since it was exposed in this session's chat history.
- **Fixed**: `SshSessionService.RunStreamingAsync` gained a `displayOverride` param (same pattern as `RunSimpleAsync`'s existing one); `RunScriptStreamingAsync` gained `envPrefixDisplay`, forwarding a redacted display command when supplied. `MainViewModel.RunExtraAsync` now builds a parallel redacted `envPrefixDisplay` (any `IsSecret` Param's value replaced with `'***'`) alongside the real `envPrefix` -- the real values still go over SSH exactly as before; only what's echoed into the terminal pane changes. This single fix point covers every Extra with a secret Param (FDT.Scout's `ADMIN_PASSWORD`, WireGuard's `WG_CONFIG`), since they all route through the same generic code path.
- **Removed all the temporary `[debug]`-prefixed diagnostic tracing** added while chasing this (`DebugTrace.cs` deleted entirely; `PasswordBoxHelper`/`ExtraParam`/`MainViewModel` reverted to clean, non-instrumented code, keeping only the actual fix) -- no longer needed now that it's confirmed working, and it was real noise in the terminal pane going forward.
- **Verified**: C# build clean (0 warnings/errors), both loose Debug and published exe smoke-tested (launch, no `crash.log`). The password-field fix itself is now genuinely live-confirmed, not just reasoned through -- the strongest form of verification this whole saga has had, ending it on real evidence rather than another guess.
- Published as `publish-v35`. CloudKeyWizard now 2.6.0.

### Same session, immediately after -- the full "active scouting + remote reporting" batch, built in one pass (user: "Build it all. Do all items in que.")
After the discussion converged over several rounds (see prior sessions' notes for the full design conversation), the user asked for everything in the queue built at once. Two items scoped out deliberately, not silently dropped: **Tailscale needed no new code** (the client Extra already existed -- only the manual tailnet-join step remains, which needs the user's own login); **backup target (rsync/Restic) stayed deferred**, since it was flagged from the start as needing its own design conversation, not fleshed out like everything else.

Everything else shipped:
- **New Monitoring tab** in FDT.Scout, deliberately separate from Health (Health = the device itself; Monitoring = what it watches on the user's behalf):
  - **Ping/TCP/HTTP/DNS host monitors** -- add any host, get uptime % and latency graphed the same way the existing CPU/mem/disk sparklines work, plus a "down since" indicator. `monitors.go`'s `MonitorEngine` runs a shared 10s ticker checking each monitor's own interval, not one goroutine per monitor.
  - **WAN speed test**, automatic every 6h -- implemented as a direct HTTP throughput measurement against Cloudflare's own public speed-test endpoints (`speed.cloudflare.com`, the same backend real tools like librespeed use) rather than shelling out to a separate `speedtest-cli` binary, keeping this dependency-free.
  - **Public IP tracking** + an optional **Cloudflare Dynamic DNS updater** -- checks every 10 minutes, alerts on change if enabled, and can PATCH a Cloudflare DNS record to match.
  - **IP range scanner** and **open port scanner** -- both strictly user-triggered (a button, never scheduled), matching the user's explicit "a function you can turn on and off" framing. The range scanner is hard-scoped to the device's own detected local subnet (never an arbitrary typed range) -- pings every host concurrently (bounded to 32 at a time), then reads `/proc/net/arp` (populated by the sweep itself) for MAC addresses. The port scanner checks a curated ~70-port common-services list against one target host, concurrently.
  - **Passive LAN device list** (reads `/proc/net/arp` directly, no active probing) with a **Wake-on-LAN** send button per device with a known MAC -- a real, dependency-free magic-packet implementation.
  - **Log aggregation**: `journalctl` output for a fixed set of key units (fdtscout, fail2ban, unattended-upgrades, Plex, the Servarr stack, Tailscale, WireGuard, ssh) copied hourly onto `/volume` (or `DataDir` if not mounted) with 90-day retention, browsable from the dashboard by unit + date without needing the terminal.
  - **Scheduled tasks**: manages real root crontab entries (not a custom scheduler), each tagged with a trailing `# fdtscout:<id>:<name>` comment so FDT.Scout only ever touches its own lines, leaving anything else already in the crontab untouched. Deliberately NOT treated as the same risk tier as the Pushbullet action allow-list -- this lives entirely inside the already-authenticated session, which can already run any command via the real terminal tab; a scheduled command here is convenience, not a new capability.
- **Metrics history extended** to 90 days (was a hard 7-day cap) when `/volume` is mounted, persisted to `/volume/fdtscout-metrics.json` instead of the small root-partition `DataDir` -- falls back to reading the old location once if the new one is empty, so switching to real storage doesn't look like history silently reset to zero.
- **Pushbullet integration**, designed over 3 rounds of discussion with the user (see prior session notes for the full history) and built exactly to that converged design:
  - **Outbound alerts**: disk space crossing 85% (with hysteresis -- clears below 80%, so it doesn't re-fire every sample while stuck near the line), a monitored app going from running to stopped, a login lockout newly triggering, the public IP changing, and an optional daily digest -- each individually toggleable in Settings, all off by default (opt-in, not automatic the moment a token is entered).
  - **Inbound two-way command channel**: FDT.Scout polls Pushbullet every 20s for pushes starting with its own configurable callsign (defaults to hostname, stored separately so a later hostname change doesn't silently break it), silently ignoring everything else. Read-only queries (`status`/`disk`/`ip`/`uptime`/`apps`/`health`/`ping <host>`) reply immediately. Actions (`restart`/`start`/`stop`/`enable`/`disable` an app) are bounded to the exact same fixed catalog `apps.go`'s own start/stop/enable/disable already enforces -- never a free-form command, since a leaked Pushbullet token turning into "someone restarted Plex" is recoverable, the same leak turning into full remote root is not. Actions require a second `<CALLSIGN> CONFIRM` push within 5 minutes before executing -- and per the user's own catch during design, the confirm message must ALSO carry the callsign (a bare `CONFIRM` does nothing), keeping the "nothing without the callsign is ever acted on" rule consistent through the whole exchange. A reserved `ALL` broadcast callsign lets one push reach every Scout at once.
  - Settings tab gained a Pushbullet section (masked access token -- from Pushbullet's own Settings -> Account -> Access Tokens page, never the user's login -- + callsign + per-trigger toggles) and a Dynamic DNS section (Cloudflare token/zone/record, masked the same way). Both token fields use the same "never echo a saved secret back in full" masking pattern (a placeholder string round-trips correctly if left unedited, same idea as the Extras' own masked-password fields).
- **Found and fixed two real pre-existing frontend bugs while verifying this batch**: `loadCertInfo` and `loadLCDStatus` both called `res.json()` without checking `res.ok` first, which throws an uncaught `SyntaxError` on any non-JSON error response (a real gap, not specific to this batch's code -- found because a static-file-server misconfiguration in this session's own verification setup meant no prior check this session had actually exercised these code paths for real; see the note below).
- **Verification note, worth being honest about**: for several checks earlier this session, the static file server used to sanity-check the frontend was started from the wrong directory (`static/` itself instead of its parent), which made `/static/app.js` and `/static/app.css` 404 silently -- meaning JavaScript never actually executed in those earlier "no JS errors" checks, so there was nothing to error. Caught and fixed this pass (confirmed via `typeof someFunction === 'function'` checks that the script now genuinely loads and runs), and the real bug above was found specifically because of that fix, then reverified clean in a genuinely fresh browser tab to rule out stale console-log carryover.
- **Verified**: Go build + `go vet` clean against the real linux/arm64 target, confirmed all new routes present in the compiled binary via `strings | grep`. C# build clean (0 warnings/errors). Embedded fdtscout binary confirmed byte-identical via `cmp`. Both loose Debug and published exe smoke-tested (launch, no `crash.log`). Frontend genuinely exercised this time (JS confirmed loading and running, every tab clicked through, only expected missing-backend 404s remaining after the two fixes above).
- **Not live-tested against real hardware** -- same standing caveat as every FDT.Scout feature this whole project, now covering a much larger surface than before. The Pushbullet command channel and the two active-scanning features are the highest-value targets for a first real test.
- Published as `publish-v36`. CloudKeyWizard now 2.7.0, FDT.Scout now 2.0.0.

### Same session, immediately after -- Pushbullet self-reply loop fixed, IP range scan opened up to a custom start/end range
User reported real, specific problems: unrecognized Pushbullet input wasn't being ignored, and the device appeared to be "reading its own reply." Both symptoms traced to the same root cause.

- **Actual root cause**: a command reply's push has this device's own callsign as its TITLE (`sendPush(cfg, mine, reply)`). Since FDT.Scout polls for ALL new pushes on the account (including its own outbound ones) and treats anything starting with its callsign as a potential command, its own reply satisfied the callsign-prefix check on the very next poll -- the device was trying to parse its own reply text as a new command, hitting the "unrecognized" fallback, and replying to THAT, looping.
- **Fixed**: `sendPush` now returns the Pushbullet-assigned `iden` of the push it just created; every push FDT.Scout sends (alerts and command replies alike) gets its iden registered in a small in-memory `wasSentByUs` set (10-minute expiry, well past the ~20s poll interval). The poller now skips any incoming push whose iden is in that set, before it ever reaches the callsign check -- closing the loop at its actual source rather than trying to out-guess title/body content.
- **Unrecognized commands are now truly silent** (no reply at all) instead of replying "Unrecognized command..." -- both because the user asked for exactly that, and as a second layer of defense: even if some other self-reply-loop-shaped bug ever slipped past the iden check, an unrecognized command no longer manufactures a fresh reply to loop on.
- **IP range scan opened up**: `ScanIPRange(start, end)` added alongside the existing local-subnet scan -- user asked directly to be able to specify a start/end address (e.g. to reach a Tailscale/WireGuard/VLAN-visible range beyond the device's own immediate subnet), a deliberate relaxation of the original "hard-limited to local subnet" design now that the device's actual owner is asking for it on their own network. Still bounded (4096 addresses max) and still only ever pings -- same non-invasive posture as before, just user-directed instead of auto-derived. The Monitoring tab's range-scan form gained optional start/end fields; leaving both blank still scans the local subnet as before.
- **Verified**: Go build + `go vet` clean, confirmed the fix landed via `strings | grep` on the compiled binary. C# build clean. Embedded fdtscout binary byte-identical via `cmp`. Both loose Debug and published exe smoke-tested (no `crash.log`). Frontend re-verified with a real preview server (correct directory this time) -- JS confirmed loading, the new start/end fields render, and the "enter both or neither" validation was exercised directly and works.
- Published as `publish-v37`. CloudKeyWizard now 2.7.1, FDT.Scout now 2.0.1.

### Same session, immediately after -- Restic backup server built, new Backup category
User asked about lightweight self-hosted backup software fitting this project's "build in a real
upstream project, like we did Plex" pattern; discussed Restic's own official `rest-server` vs.
Syncthing, picked `rest-server` as the closer fit to what "backup target" originally meant, queued
it with the open design questions named plainly, then said "Build it."

- **New `Backup` category** added to `ExtraCategory` and wired through everywhere the existing four
  categories already were (the dispatch switch, a new `BackupExtras` collection, all four
  `.Concat(...)` chains that sync Extras' status to/from device state, and a new XAML section in the
  Extras page) -- mechanical, modeled directly on how `HomeAutomation` was added.
- **Restic backup server (`rest-server`)**, a real Extra, real upstream project (not jnovack/cloudkey,
  not hand-rolled): installs the official pre-built ARM64 binary, pinned to a specific version
  (v0.14.0, confirmed via the real GitHub releases API rather than guessed) and checksum-verified
  against upstream's own `SHA256SUMS` before it's trusted -- same discipline this project already
  applies to jnovack's own bundled scripts. Resolves the three open design questions from the queue:
  - **Repository data**: `/volume` if mounted, with a clear warning (and instructions to re-run
    after Format & Mount Storage) if it isn't -- same "use the big drive" discipline as log
    aggregation and extended metrics history.
  - **Per-client credentials**: HTTP Basic Auth via a real `.htpasswd` file (`apache2-utils`
    installed if needed), `--private-repos` so each authenticated user gets their own isolated
    repository directory automatically rather than one shared repo everyone can see into. The Extra
    takes a first username+password as Params (password masked, `IsSecret=true`); more users can be
    added later via `htpasswd -B` from the terminal, documented in the post-install summary.
  - **Retention/pruning**: explicitly left to the client side (`restic forget --prune`), matching how
    Restic's own architecture actually works -- the server is dumb encrypted storage, not a policy
    engine, and the Extra's description and post-install summary both say so plainly rather than
    implying this device manages retention.
  - **Exposure**: its own port (`:8000`, rest-server's own convention), not folded into FDT.Scout's
    HTTPS listener -- a real self-signed TLS cert generated once (not regenerated on every re-run,
    which would otherwise silently break every already-configured client's trust). Verified the
    exact CLI flags (`--path`/`--private-repos`/`--htpasswd-file`/`--tls-cert`/`--tls-key`/`--listen`)
    and the client-side connection syntax (`rest:https://user:pass@host:port/`, `--cacert` for
    trusting the self-signed cert) against rest-server's own real README rather than guessed.
  - Backups are encrypted **client-side** before they ever reach this device -- this box only ever
    stores encrypted blobs, never sees the actual data, a real security property worth stating
    plainly in the Extra's own description.
- **Verified**: C# build clean (0 warnings/errors) both before and after the version bump. Both loose
  Debug and published exe smoke-tested (launch, no `crash.log`) at each stage. The new heredoc-based
  systemd unit generation follows the exact same proven pattern the WireGuard Extra already uses
  successfully elsewhere in this same file, not a new untested approach.
- **Not live-tested against real hardware** -- same standing caveat as everything in this project;
  this one specifically hasn't been watched actually installing, taking a real client backup, or
  restoring one.
- Published as `publish-v38`. CloudKeyWizard now 2.8.0 (FDT.Scout unchanged at 2.0.1 -- this pass
  was CloudKeyWizard.exe-only, no Go changes).

### Same session, immediately after -- Headscale removal + Docker feature, both built ("BUILD EVERYTING IN THE QUE!")

After the Restic pass above shipped, the user asked what was still queued and pointed out (their own
words) that this was at least the 4th time queued items had been discussed and converged on but not
actually built promptly. Both remaining queue items -- removing the Headscale placeholder and the
Docker feature -- were built in this same pass, no further discussion.

- **Removed the "Self-hosted Headscale server" placeholder Extra**, exactly as decided and verified
  safe earlier in the queue entry above: deleted the one `ExtraItem { IsAvailable = false, ... }`
  block from `Services/ExtraCatalog.cs`. Confirmed via `grep` that no other `IsAvailable = false`
  entries remain in the catalog -- it really was the last placeholder. Updated the file's own doc
  comment, which had gone stale (still described WireGuard/Headscale as "staged placeholders," but
  WireGuard shipped as a real Extra a while ago and Headscale is now gone rather than staged).
- **Built the Docker feature in FDT.Scout**, full trimmed scope from the queue entry above:
  - `tools/fdtscout/docker.go`: `getDockerStatus()` (installed/running/version/container count/
    storage root/`/volume` mounted+in-use), `installDocker()` (idempotent `apt-get install docker.io`
    + `systemctl enable --now`, deliberately no matching uninstall), `ListContainers()` (parses
    `docker ps -a --format '{{json .}}'` line-by-line, including each container's own writable-layer
    size, not the shared/virtual size), `ContainerAction()` (start/stop/pause/unpause/restart/remove,
    a fixed verb set, container ID always passed as a single `exec.Command` argument -- never
    interpolated into a shell string), `ContainerLogs()` (`docker logs --tail`), `RunContainer()`
    (pulls the image as its own visible step before `docker run`, assembles `-p`/`-v`/`-e`/
    `--restart`/`--memory` flags from the request), and `SetDockerStorageRoot()`/
    `writeDockerDataRoot()` (stops Docker, `rsync -a`'s the existing data tree to the new path, updates
    `data-root` in `/etc/docker/daemon.json`, restarts -- reverts to the previous root and restarts
    there automatically if the daemon fails to come up on the new path, so a bad move never leaves
    Docker down).
  - `tools/fdtscout/handlers_docker.go`: seven HTTP handlers wired into `main.go` at `GET /api/docker`,
    `POST /api/docker/install`, `GET /api/docker/containers`, `POST
    /api/docker/containers/{id}/{action}`, `GET /api/docker/containers/{id}/logs`, `POST
    /api/docker/run`, `POST /api/docker/storage` -- same `requireAuth`/`jsonEscape`/manual-path-parse
    conventions already used by every other handler file (matched against `handlers_apps.go` and
    `handlers_storage.go` before writing, not guessed).
  - New "Docker" tab in `static/index.html` + `static/app.js` (`loadDockerTab`, `loadDockerContainers`,
    `doDockerAction`, `showDockerLogs`, the run-container form handler) -- vanilla JS, same style as
    the rest of the dashboard (`escapeHtml`, `setMsg`, `.secondary`/`.term-output`/`.data-table` CSS
    classes reused, not new ones invented). Deliberately separate from the Apps tab's own narrower
    `kindDocker` handling (still just Home Assistant's one named container, untouched).
  - **Verified, not just written**: `gofmt -w` on the two new files; `go vet -buildvcs=false` and
    `go build` both clean under the real target (`GOOS=linux GOARCH=arm64 CGO_ENABLED=0`); `strings`
    on the compiled binary confirms the new routes, `main.ContainerAction`/`main.handleDockerStatus`/
    etc., and `FDT.Scout 2.1.0` are actually present, not just present in source. Frontend checked in
    the Browser pane served correctly (parent-of-`static/` lesson applied from earlier in this
    project, not repeated) -- `typeof loadDockerTab === 'function'` etc. all true, no new JS errors
    beyond the expected 404s from there being no real backend behind a plain static-file server, and
    the rendering logic itself (status text, storage-move-button visibility, per-container action
    buttons) was exercised by mocking `fetch` for `/api/docker` and `/api/docker/containers` and
    confirming the DOM came out right.
  - Binary rebuilt with `-ldflags="-s -w"` and copied into
    `src/CloudKeyWizard/Scripts/fdtscout/fdtscout-arm64`, confirmed byte-identical via `cmp`.
- **Version bumps**: FDT.Scout 2.0.1 -> **2.1.0** (`tools/fdtscout/version.go`, changelog entry
  added). CloudKeyWizard.exe 2.8.0 -> **2.9.0** (`Version.cs`, one changelog entry covering both the
  Headscale removal and the bundled FDT.Scout upgrade).
- **Verified**: C# build clean (0 warnings/errors). Both loose Debug and published exe smoke-tested
  (launch, wait, confirm no `crash.log`) before and after publish.
- **Not live-tested against real hardware** -- same standing caveat as everything in this project;
  the Docker tab specifically hasn't been watched installing Docker, running a real container, or
  moving storage to a real `/volume` mount on an actual Cloud Key.
- Published as `publish-v39`. CloudKeyWizard now 2.9.0, bundled FDT.Scout now 2.1.0.

### Same session, next day -- real live purge run, batch-cascade auto-retry built (2026-08-25)

User ran the wizard live against a real Cloud Key Gen2 Plus at `10.0.100.139` and pasted the terminal
transcript, asking "what happened here" then specifically "any way to build in logic to handle this
better?" about the purge step's final `EXIT CODE 1`.

- **What happened**: this was the exact previously-documented batch-vs-dependency-cascade race (see
  the "First real Apply run" note earlier in this file) -- purging `mongodb-server` in Batch 2
  cascade-removed the `unifi` package before Batch 3 got to purge it by name, and `unifi-core` had
  similarly already gone with Batch 1's `unifi-assets-uckp` group. Batch 3 then tried `apt-get purge
  unifi unifi-core` and got `E: Unable to locate package` for both -- apt can't resolve a package by
  name once it's already gone from its own index. `OutcomeInterpreter.InterpretPurgeApply` already
  recognized this exact signature and returned a friendly NoGo explanation ("re-run Apply, this is
  designed to be safely retried") -- but that still left a Failed step in the UI waiting for a human
  to notice the explanation and click Apply a second time.
- **The real fix would be inside `phase1-purge.sh`** (each batch re-checking live `dpkg` state right
  before purging, instead of filtering against a `$REMAINING_PACKAGES` snapshot computed once at
  Step 0) -- but that file is a **verbatim jnovack/cloudkey copy at a pinned commit**
  (`Scripts/runbook/README.md`), and this project's whole discipline is to never silently patch
  upstream content. So the fix instead lives entirely in app-authored code, same pattern already used
  for `phase1-account-cleanup.sh` (an app-authored follow-on wrapped around jnovack content, not an
  edit to it):
  - `Models/StepOutcome.cs` gained `SafeAutoRetry` (bool, default false) -- true only for this
    specific recognized condition, never for a genuine ABORT (forbidden package, liveness check,
    unexpected simulate mismatch), which still stops and waits for a human same as before.
  - `OutcomeInterpreter.InterpretPurgeApply` now returns `SafeAutoRetry = true` for the
    "Unable to locate package" + "ABORT: apt-get purge failed for batch" signature.
  - `MainViewModel.RunSelectedStepAsync` gained a `safeAutoRetryAttempt` parameter (capped at 1 via
    new `MaxSafeAutoRetries`, deliberately separate from the existing `MaxAutoRetriesAfterReconnect`
    reconnect-retry counter -- a different failure class with a different, smaller cap) -- on this
    specific outcome it now automatically re-runs the whole purge script once instead of marking the
    step Failed and stopping, with a terminal line explaining why. A single retry is genuinely as
    safe as the operator clicking Apply again by hand: the script recomputes `$REMAINING_PACKAGES`
    from scratch at the top of every run and re-runs Step 0's simulate/exact-match safety gate fresh
    each time too, so the retry gets the exact same safety check the original run did, and Batch 3
    will simply see nothing left to do and skip cleanly.
- **Verified**: C# build clean (0 warnings/errors). Both loose Debug and published exe smoke-tested
  (launch, confirm no `crash.log`) before and after publish. Not re-tested against the actual failure
  live (would need a fresh device back at the same cascade point) -- the fix follows the exact retry
  pattern (bounded recursive call, `return` so the recursive call owns its own `finally`/`IsBusy`)
  already proven working live for the reconnect-retry case earlier in this same method.
- Published as `publish-v40`. CloudKeyWizard now 2.10.0 (FDT.Scout unchanged at 2.1.0 -- this pass
  was CloudKeyWizard.exe-only).

### Same session, later that day -- two more live-found bugs fixed: purge-Done flag, reboot-wait race

User continued the same live run and reported two more real issues, both diagnosed fully and queued
first, then built together after user confirmed via AskUserQuestion ("Build both now").

- **Purge step never shows Done, even after a fully successful purge**: `PreflightService
  .DetectStepStatusAsync`'s purge-completeness check (`Services/PreflightService.cs`) was reusing the
  same broad `dpkg -l | grep -iE 'unifi|ubnt|uos-|ucs-|uid-agent|ulp-go'` that `phase1-verify.sh`
  prints purely for a human to eyeball -- but that grep also matches packages the purge script
  deliberately keeps (`ck-ui`/`ubnt-tools`, the hard `FORBIDDEN` set) or never targeted at all
  (`ubnt-archive-keyring`, `ubnt-zram-swap`, etc.), so it could never actually come back clean, on any
  device, ever. Sequence: `RunSelectedStepAsync` sets the step Done on a real success -> immediately
  calls `DetectStepStatusAsync` to resync all steps against live state (real, useful behavior on its
  own) -> that resync's purge check sees `ck-ui` still installed (correctly, on purpose) and
  overwrites the just-set Done back to Pending. Fixed by adding `PreflightService
  .PurgeTargetPackages`, a duplicate of `phase1-purge.sh`'s own `$PACKAGES` removal list, and checking
  against that instead -- same `dpkg-query -W -f='${db:Status-Abbrev}'` prefix-matching logic the
  script's own `remaining_packages()` uses, so "purge Done" means the same thing in both places.
- **Reboot step's wait could lose a race and not actually wait**: `MainViewModel.RebootAndWaitAsync`
  sent a backgrounded `sleep 1; reboot`, disconnected, and immediately started polling for the port to
  come back UP -- but nothing confirmed it went DOWN first. sshd is often still fully up for a beat
  after the reboot command is triggered (it hasn't even reached the `reboot` call yet, still inside
  the 1-second sleep), so the very first "is it back" probe could simply reconnect to the
  not-yet-rebooted box and report a false "it's back" almost instantly -- exactly what got reported
  live ("it just went to the next step"). A genuine race, not deterministic, which is why it appeared
  to wait correctly in an earlier transcript from the same session and then visibly didn't a bit
  later. Fixed by adding `SshSessionService.WaitForPortDownAsync` (polls for a connection attempt that
  clearly fails/refuses/times out, up to 90s) and calling it before the existing `WaitForPortAsync` --
  a real down-then-up transition is now required to count as "rebooted"; if the port never goes down,
  the step fails clearly instead of silently declaring success. Matches the design intent already
  stated in the Reboot page's own caption text.
- **Verified**: C# build clean (0 warnings/errors). Both loose Debug and published exe smoke-tested
  (launch, confirm no `crash.log`) before and after publish. Neither fix re-tested live against the
  actual failure conditions yet (would need a fresh device back at the same points) -- both follow
  established, already-proven patterns in this codebase (the purge check now mirrors the script's own
  `remaining_packages()` logic exactly; the reboot fix is a standard down-then-up liveness check, the
  same category of check `phase1-purge.sh`'s own `check_liveness` already does mid-purge).
- Published as `publish-v41`. CloudKeyWizard now 2.11.0 (FDT.Scout unchanged at 2.1.0 -- this pass
  was CloudKeyWizard.exe-only).

### Same session -- jnovack/cloudkey license compliance check (attribution vs. actual license terms)

User asked whether this project's About/help/README content actually meets jnovack/cloudkey's real
license requirements, linking their `pkg/` directory. Fetched the real license (not assumed):
jnovack/cloudkey's `LICENSE.md` is **Creative Commons Attribution-NonCommercial-ShareAlike 4.0
International (CC BY-NC-SA 4.0)** -- confirmed via the actual repo, not guessed. Confirmed
`scripts/runbook/phase1-purge.sh` etc. really do exist verbatim in jnovack's own repo, matching this
project's own claim that its `Scripts/runbook/*.sh` are unmodified copies of that content.

- **Finding**: grepped this entire repo for "Creative Commons"/"CC BY"/"NonCommercial"/"ShareAlike"/
  "LICENSE" -- zero hits anywhere. Attribution to the *project* (jnovack/cloudkey by name, linked,
  pinned commit) was consistently good throughout README.md, `Scripts/runbook/README.md`, and both
  apps' About text -- but the actual *license* governing the bundled scripts was never named or
  linked anywhere, and no LICENSE/NOTICE file existed in this repo at all. Attribution to the author
  ≠ disclosing the license terms, which CC BY-NC-SA explicitly requires.
- **The NonCommercial question**: this app bundles jnovack's NC-licensed scripts and is built under
  "Jay Anderson of FullDuplexTech.com," a named business/brand -- flagged this as a real open
  question I couldn't resolve myself (not a code problem, a business/legal one). Asked the user
  directly rather than assuming: (1) is this app sold or given away -- **given away, not sold**; (2)
  is FullDuplexTech.com itself commercial (paid services/ads) or a personal/hobby site -- **personal/
  hobby site, no monetization**. Both answers land cleanly on the noncommercial side of CC BY-NC-SA's
  actual definition ("not primarily intended for or directed toward commercial advantage or monetary
  compensation") -- not a legal guarantee, but a clean, well-attributed, free personal-project use is
  exactly what that clause is meant to allow.
- **Fixed the objective gap** (the missing license disclosure, independent of the NC question above):
  - Added `Scripts/runbook/LICENSE-jnovack-cloudkey.md` -- a verbatim copy of jnovack/cloudkey's own
    `LICENSE.md`, with a short header clarifying it governs only the unmodified jnovack-sourced files
    in that directory (not the 3 app-authored files there, not CloudKey Wizard as a whole).
  - `Scripts/runbook/README.md` now explicitly names the license (CC BY-NC-SA 4.0), links to it, and
    points to the new LICENSE file.
  - Root `README.md` now states the license + link, and that this app is free/personal/non-commercial.
  - Both apps' About text (`Version.cs`, `tools/fdtscout/version.go`) now name the license, link to
    it, reference `LICENSE-jnovack-cloudkey.md`, and state the app is given away free on a personal,
    non-commercial basis.
- **Verified**: FDT.Scout `go vet`/`go build` clean under the real target, `strings` on the compiled
  binary confirms `FDT.Scout 2.1.1` and the license text/link are actually present, binary re-embedded
  and confirmed byte-identical via `cmp`. C# build clean, both loose Debug and published exe
  smoke-tested (no `crash.log`).
- **What this is NOT**: not a legal opinion or guarantee -- I'm not a lawyer, and CC licenses are
  notoriously not well-suited to software in the first place (Creative Commons' own guidance says so).
  This closes the *objective, mechanical* gap (naming and linking the actual license, same as the
  existing attribution-to-the-project practice) and records the user's own factual answers on the
  NonCommercial question; it doesn't replace getting real legal advice or jnovack's own explicit
  sign-off if this project's distribution model ever changes (e.g. if it stops being free, or
  FullDuplexTech.com becomes a commercial venture).
- Published as `publish-v42`. CloudKeyWizard now 2.11.1, bundled FDT.Scout now 2.1.1.

**Follow-up, same session**: user pointed out the first-run disclaimer overlay ("Before you start")
only said "see Help -> About for references and full details" -- the license itself wasn't named on
the one screen guaranteed to be seen before anything runs. Not strictly required by the license (a
notice just needs to be reasonably accessible, and About already had it), but a good instinct worth
acting on. Added a second italic line to that overlay in `MainWindow.xaml` naming CC BY-NC-SA 4.0 and
linking to it directly, right next to the existing no-warranty/not-affiliated line. Build clean, both
loose Debug and published exe smoke-tested (no `crash.log`). Published as `publish-v43`. CloudKeyWizard
now 2.11.2 (FDT.Scout unchanged at 2.1.1).

**Follow-up, same session**: user asked whether the overlay should also credit the FullDuplexTech.com
article that inspired the project. Initial recommendation was to leave it off that specific screen
(it's a legal/risk notice gate, not an about page, and the article's already credited in README.md
and the About text) -- user overrode that and asked for it anyway ("Lets add something to that splash
screen"), so added a third italic line right below the license line naming the article and its URL.
Build clean, both loose Debug and published exe smoke-tested (no `crash.log`). Published as
`publish-v44`. CloudKeyWizard now 2.11.3 (FDT.Scout unchanged at 2.1.1).

**Follow-up, same session -- full feature breakdown added to About (presentation only, no
functional change)**. After the "how many functions" / "how many are original vs 3rd party"
exchange, user framed it clearly: for them personally, the real point of this app is getting
FDT.Scout installed -- everything else exists to get a Cloud Key to a state where that's possible --
but acknowledged other users may have a different primary reason, and asked whether the About section
should list all ~37 features with their actual source (FullDuplexTech vs. jnovack/cloudkey). Added a
new block to the end of `Version.cs`'s `AboutText`: a short first-person framing paragraph (why this
app exists for its author, explicitly noting that reason may not match the reader's own), then the
full itemized breakdown -- 10 core wizard steps, 17 Optional Extras, and FDT.Scout's 10 tabs, each
tagged FullDuplexTech (original) or jnovack/cloudkey (sourced), with the 3 hybrid/wrapper cases
(Plex, Home Assistant, WireGuard) noted as "our installer, third-party software" rather than glossed
over. Closing total: 27 original, 10 jnovack-sourced, 37 total. Verified against the actual code
(`ExtraCatalog.cs`'s `ScriptContent` vs `BundledScriptFileName`/`SiblingBundledScriptFileNames`
markers, `ScriptCatalog.cs`'s step list, `index.html`'s `data-tab` list) before writing it, not
recalled from memory. Build clean, both loose Debug and published exe smoke-tested (no `crash.log`).
Published as `publish-v45`. CloudKeyWizard now 2.12.0 (FDT.Scout unchanged at 2.1.1).

**Follow-up, same session -- caught a real inconsistency in that feature list.** User asked "are we
sure on those numbers 27 vs 10?" -- good instinct, there was a real bug: WireGuard's line in the
About text was worded to lead with "(FullDuplexTech orchestrator around jnovack/cloudkey's own
scripts...)", but the "27 original / 10 jnovack" total right below it only adds up if WireGuard
counts as jnovack-sourced. Counting the *exact tags as written*, the itemized list actually read as
28 FullDuplexTech / 9 jnovack -- one off from the stated total. Root cause: earlier in this same
conversation I'd explicitly classified WireGuard as jnovack-sourced ("the actual VPN mechanics are
theirs") when explaining the split verbally, but phrased its About-text line inconsistently with that
when actually writing it. Fixed by rewording the line to lead with jnovack/cloudkey (the 7 scripts +
systemd units that do the actual VPN work are theirs, verbatim; only the orchestrator wiring them
together is original) -- now the itemized tags and the "27/10" total agree. Build clean, both loose
Debug and published exe smoke-tested (no `crash.log`). Published as `publish-v46`. CloudKeyWizard now
2.12.1 (FDT.Scout unchanged at 2.1.1).

### Same session -- repo published to GitHub, first Release, then a real Pushbullet bug fixed

User asked whether to fork jnovack/cloudkey to upload their own code -- explained why that's the
wrong move (implies CloudKeyWizard is a derivative of jnovack's own project, and a fork inherits
their CC BY-NC-SA license file, which would bleed onto this project's own original code) and
recommended a dedicated, separate repo instead. User created
[`janderson2k/CloudKeyWizard`](https://github.com/janderson2k/CloudKeyWizard) (empty, public, no
README/gitignore/license template) via the GitHub web UI.
- **Published**: fixed `.gitignore` gaps first (`publish-v*/` wasn't excluded -- would have committed
  ~1GB+ of duplicate compiled exes; also excluded two stray leftover Go binaries sitting in
  `tools/fdtscout/` from earlier build/test sessions, never the real embedded copy). `git init`,
  staged (139 files, 8.9MB, verified clean via `git status --short` before committing), first commit,
  pushed via GitHub Desktop (command-line `git push` initially failed on a stale cached credential in
  Windows Credential Manager that `cmdkey` couldn't clean up programmatically -- GitHub Desktop's own
  sign-in flow bypassed it entirely, and running `gh auth setup-git` afterward repaired the
  command-line credential too, so plain `git push` works from the terminal again going forward).
- **GitHub CLI installed** (`winget install GitHub.cli`) so Releases can be created/managed directly
  -- authenticated via the device-code browser flow (user completed the actual sign-in themselves,
  as it must be).
- **First Release published**: `v2.12.1` with `CloudKeyWizard.exe` attached as a downloadable asset
  (not committed to git -- Releases are the right home for built binaries), verified byte-count
  matches the local file exactly. Added a download link to the top of `README.md` pointing at
  `releases/latest` so it never goes stale across future releases.
- **Blog content drafted** for a FullDuplexTech.com WordPress post about the app -- full draft
  delivered in chat (not saved to a file), covering what it does, the Extras ecosystem, credit to
  jnovack/cloudkey, and the free/non-commercial framing. Not saved anywhere in this repo; if wanted
  later, ask the user whether they kept/edited it.
- **Real Pushbullet bug found and fixed**: user reported getting summary/digest notifications from
  multiple devices with no way to tell them apart (no machine identifier in the message). Traced to
  `notify.go`: every proactive alert (`notifyDiskFull`, `notifyServiceDown`, `notifyLockout`,
  `notifyIPChange`, `notifyDigest`) sent a generic title ("Disk space low", "Daily digest", etc.) with
  zero device identifier -- unlike command *replies*, whose title already IS the callsign, so they
  were never ambiguous. This is a different bug from the earlier self-reply-loop fix (that one made
  the device correctly ignore its own messages; this one is about a human being able to tell WHICH
  device sent a legitimate message) -- user's own proposed fix (quoting/reformatting the callsign in
  status messages) was explained as unnecessary for the loop problem specifically (already closed via
  `iden`-tracking, not text pattern matching) but the underlying alert-identification gap they'd
  actually run into was real and got fixed properly: `notifyAsync` now prefixes every alert title with
  `callsignPrefix(cfg)` (e.g. "SCOUT001: Daily digest") -- one change point, fixes all 5 alert types at
  once, doesn't touch the already-correct command-reply path.
- **Verified**: `go vet`/`go build` clean under the real target, `strings` on the compiled binary
  confirms `FDT.Scout 2.1.2` and the new changelog text are present, binary re-embedded and confirmed
  byte-identical via `cmp`. C# build clean, both loose Debug and published exe smoke-tested (no
  `crash.log`).
- Committed and pushed via plain `git push` (credential fix from earlier in this session held).
  Published as `publish-v47` and as GitHub Release
  [`v2.13.0`](https://github.com/janderson2k/CloudKeyWizard/releases/tag/v2.13.0) with the exe
  attached. CloudKeyWizard now 2.13.0, bundled FDT.Scout now 2.1.2.

### Same session -- FDT.Scout version check + "Update available" highlighting on the Extras page

User asked to add a version check for individual components (FDT.Scout specifically) and highlight
available updates when scanning an already-set-up Cloud Key. Built and shipped same pass.
- **`fdtscout` gained a `-version` flag** (`tools/fdtscout/main.go`) -- prints the version and exits,
  checked before `ensureDirs()` so it works even without proper data-dir permissions. This is the
  actual mechanism: CloudKeyWizard runs `/opt/fdtscout/fdtscout -version` over SSH, one-shot, no
  HTTP/TLS trust needed, works even if the systemd service isn't currently running. FDT.Scout
  2.1.2 -> 2.1.3.
- **`AppVersion.BundledFdtScoutVersion`** (`Version.cs`) -- a plain string constant naming the
  FDT.Scout version actually embedded in THIS build (`Scripts/fdtscout/fdtscout-arm64`). Deliberately
  a hand-maintained literal, not auto-derived from the Go source at build time -- documented plainly
  as something to bump by hand alongside `tools/fdtscout/version.go` whenever that binary changes,
  same discipline already used for the changelog mirroring between the two version files.
- **`ExtraItem` gained `InstalledVersion` and `UpdateAvailable`** (`Models/ExtraItem.cs`) -- bindable
  properties, empty/false until a check actually runs (no "assume outdated" default).
- **`MainViewModel.CheckFdtScoutVersionAsync`** -- runs right after `RestoreExtrasFromDeviceStateAsync`
  in the connect flow (exactly the "scanning an already setup Cloud Key" moment), but only when the
  fdtscout Extra is already marked Done (installed) -- nothing to check on a fresh device, since a
  fresh install always gets the current bundled version anyway. Best-effort: an older FDT.Scout
  predating `-version`, or any SSH hiccup, just leaves `InstalledVersion` blank rather than failing
  the whole connect flow over a version check.
- **UI** (`MainWindow.xaml`): a yellow "Update available" badge next to the FDT.Scout item's title
  (bound to `UpdateAvailable`, using the existing `CautionYellow` resource -- same severity-color
  convention already used elsewhere in the app), the Run button's `Content` switches to "Update" via
  a `DataTrigger` when applicable, and the detail line shows both versions plainly ("Installed:
  v2.1.1 -- update available (this Wizard bundles v2.1.3). Re-run to update.").
- **Scoped to FDT.Scout only, deliberately** -- it's the one Extra with a component that both reports
  its own version live and has a version this app tracks as "what's bundled." Other Extras (Plex,
  Sonarr, Docker, etc.) install whatever's current via apt/vendor scripts with no comparable
  bundled-vs-installed version concept, so extending this pattern to them isn't a mechanical copy --
  revisit only if a real need for that shows up.
- **Verified**: `go vet`/`go build` clean under the real target, binary re-embedded and confirmed
  byte-identical via `cmp`. C# build clean (0 warnings/errors), both loose Debug and published exe
  smoke-tested (no `crash.log`) -- launch alone would have thrown on a broken XAML binding/trigger,
  which it didn't. Not live-tested against a real already-converted device with an older FDT.Scout
  installed (would need one at hand to watch the badge actually appear) -- same standing caveat as
  everything else in this project.
- Committed, pushed, published as `publish-v48` and GitHub Release
  [`v2.14.0`](https://github.com/janderson2k/CloudKeyWizard/releases/tag/v2.14.0) with the exe
  attached. CloudKeyWizard now 2.14.0, bundled FDT.Scout now 2.1.3.
