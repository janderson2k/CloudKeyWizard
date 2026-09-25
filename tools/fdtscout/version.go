package main

// Versioned independently from CloudKeyWizard.exe itself -- this is a separate program with its
// own release cadence, even though both ship from the same repo and the Windows app is what
// installs this one. Bump these together with the changelog below whenever this Go source changes
// and gets recompiled/re-embedded.
const (
	Version   = "2.9.0"
	BuildDate = "2026-09-25"
)

type ChangelogEntry struct {
	Version string   `json:"version"`
	Date    string   `json:"date"`
	Notes   []string `json:"notes"`
}

var Changelog = []ChangelogEntry{
	{
		Version: "2.9.0",
		Date:    "2026-09-25",
		Notes: []string{
			"Fixed a real bug: editing a job (unchecking Enabled, changing its label/source/schedule) didn't show up in the jobs list until a manual page refresh. Enable/Disable is now also its own quick-toggle button on each job, next to Run now, instead of only being changeable from inside the edit form.",
			"The Run now button now turns into a Stop button while that job is running, instead of just greying out next to a separate Stop button.",
			"New: the Apps tab can check GitHub for a newer FDT.Scout release and update itself directly -- no need to go back to the Windows wizard and reconnect over SSH. Always user-initiated (a Check button, then an Update button that only appears if something's actually newer); this is the one place this console reaches the internet at all.",
		},
	},
	{
		Version: "2.8.0",
		Date:    "2026-09-25",
		Notes: []string{
			"Fixed a real bug: SMB jobs ignored the configured \"Path within the share\" entirely and always backed up the whole share from its root. Fixed to actually scope to the configured subfolder, matching how FTP jobs already worked.",
			"New: a \"Test connection\" button on the job form checks the exact host/share/path/credential currently typed in -- before you ever save the job -- using the same connect-and-list logic a real run uses, so a pass here means a real run will get past the connection step too.",
			"New: a Stop button on a running job. Takes effect between files (not instantly mid-transfer), and a stopped run is recorded honestly as \"Stopped,\" not as a failure -- whatever it already backed up before stopping is kept.",
			"Fixed a real bug: the jobs list rebuilt its entire contents every 2 seconds while any job was running, which could make a nearby form field feel impossible to type into. Now only the parts that actually change (status, progress, metrics) update in place.",
		},
	},
	{
		Version: "2.7.1",
		Date:    "2026-09-25",
		Notes: []string{
			"Fixed the actual bug behind runs still vanishing after 2.7.0's fix: a job's run-history write silently failed for any job that had never once connected successfully, because its data folder didn't exist yet and nothing created it before writing to it. A job whose only attempts fail at the connection step (bad credentials, unreachable host, share permission denied) can now show that failure instead of showing nothing at all. Proven against a real filesystem, not just re-read.",
		},
	},
	{
		Version: "2.7.0",
		Date:    "2026-09-25",
		Notes: []string{
			"Fixed a real bug found live: a run that got interrupted (FDT.Scout restarting mid-backup, a crash) used to vanish with zero trace -- not failed, not still running, just gone. Every run now writes a durable \"running\" record the instant it starts and updates it in place when it finishes, so an interrupted run stays visible (labeled \"Interrupted\") instead of disappearing. A panic anywhere in a run's own connection/transfer code (SMB/FTP protocol handling this app doesn't control) is now caught and recorded as a normal failed run with the actual error, instead of silently crashing the whole console.",
			"LifeRaft now refuses to start a run when the destination is nearly out of space, with one clear error, instead of failing every single file with the same disk-full error. Leftover temp files from a previous interrupted copy are now cleaned up automatically at the start of the next run instead of accumulating forever.",
		},
	},
	{
		Version: "2.6.0",
		Date:    "2026-09-25",
		Notes: []string{
			"Fixed a real bug: a LifeRaft source that stalled after the initial connection (a firewall silently dropping packets, a server that hung mid-negotiate) could block a run -- and the global run queue behind it -- forever with nothing ever recorded. Connections now abort with a clear error after 2 minutes without progress.",
			"LifeRaft's jobs list is now a real dashboard instead of a plain table: each job is a card showing its status, and a running job shows a live, honest progress bar (files actually processed out of the source's real file count) plus live-updating metrics (added/changed/deleted/transferred/elapsed) as it goes -- not a fake percentage. A finished job's card shows the same metrics from its last run, and a failed run's actual error text right on the card.",
		},
	},
	{
		Version: "2.5.0",
		Date:    "2026-09-25",
		Notes: []string{
			"LifeRaft jobs list now shows a \"Running\" indicator on a job that's mid-sync, a per-job Alert checkbox (also settable in the job form) to get a Pushbullet notification whenever that job's run finishes failed or with errors, and the run history now shows each run's actual duration and the real error text -- not just a count -- for a failed or partial run.",
		},
	},
	{
		Version: "2.4.0",
		Date:    "2026-09-25",
		Notes: []string{
			"LifeRaft: SMB jobs can now be set up by pasting a UNC path (\\\\host\\share\\folder) instead of typing host/share/path separately, and a new \"Browse this share...\" button lets you pick the share and folder interactively (connects read-only with the selected credential, lists shares, click through subfolders, \"Use this folder\") rather than typing a path blind.",
			"LifeRaft: the schedule field is now a plain-language picker (Daily/Weekly/Monthly, a day, a time) instead of raw cron syntax, translated to cron automatically -- an \"edit raw cron\" link is still there for anything the picker can't express.",
		},
	},
	{
		Version: "2.3.0",
		Date:    "2026-09-25",
		Notes: []string{
			"New tab: LifeRaft -- read-only pull backups from SMB/FTP/FTPS sources onto this device's own storage, so if the worst happens you can grab this box out of the rack and still have your stuff. Credentials are saved once and reusable across any number of jobs. Every job keeps its live mirror forever; a changed or source-deleted file is protected (moved aside, never overwritten) for a retention window you pick per job (1 day up to 3 months). Backups run one at a time device-wide so multiple jobs never overwhelm a source or trigger an account lockout. A guided wizard sets up dedicated storage at /volume if it isn't already there -- same safety picker (mounted/boot-flash/secure-partition drives are never selectable) CloudKeyWizard's own storage step already uses. Includes a read-only file browser for downloading your stuff back out -- LifeRaft never writes back to a source, so restoring there is on you, by design.",
		},
	},
	{
		Version: "2.2.0",
		Date:    "2026-08-28",
		Notes: []string{
			"New: join a tailnet right from the Settings tab -- previously only possible from the terminal. Two ways in: an auth key (from Tailscale's own admin console, or a self-hosted Headscale server) joins instantly with no browser step, or \"Join via browser\" starts the normal interactive login and shows the approval link plus a scannable QR code, auto-detecting once you finish. A \"Leave tailnet\" button is there too. Installing Tailscale itself is still done from the Apps tab -- this is specifically the configure-it-afterward step.",
			"Monitoring tab: each watched host can now opt into a Pushbullet notification when it goes down or recovers, independent of the account-wide alert toggles on the Health tab -- edge-triggered, so a host that's simply been down for a while doesn't spam a push every check.",
		},
	},
	{
		Version: "2.1.3",
		Date:    "2026-08-28",
		Notes: []string{
			"Added a `-version` flag (prints the version and exits) so CloudKeyWizard can check what's actually installed on an already-converted device over SSH -- no HTTP/TLS trust needed, works even if the service isn't running. Supports CloudKeyWizard's new version-check-and-highlight-updates feature on the Extras page.",
		},
	},
	{
		Version: "2.1.2",
		Date:    "2026-08-27",
		Notes: []string{
			"Fixed a real bug reported live: proactive Pushbullet alerts (disk space, service down, login lockout, IP change, the daily digest) carried no device identifier at all -- \"Daily digest\" looked identical from every device on the same Pushbullet account, with no way to tell which one sent it. Every alert's title is now tagged with the same callsign the two-way command channel already uses (e.g. \"SCOUT001: Daily digest\"), so it's finally possible to tell devices apart. Command replies were never affected by this -- their title already was the callsign.",
		},
	},
	{
		Version: "2.1.1",
		Date:    "2026-08-25",
		Notes: []string{
			"About tab: now explicitly names jnovack/cloudkey's actual license (CC BY-NC-SA 4.0) and links to it, and states this tool is given away for free on a personal, non-commercial basis -- previously only credited the project by name without stating the license governing the bundled runbook scripts.",
		},
	},
	{
		Version: "2.1.0",
		Date:    "2026-08-24",
		Notes: []string{
			"New Docker tab: a general-purpose container manager, separate from any single app's own Docker use (Home Assistant's container, if installed, still shows on the Apps tab as before). Install/verify Docker with one click (one-way -- once installed, other things may come to depend on it, so there's no uninstall here); full lifecycle control on existing containers (start/stop/pause/restart/remove) with size shown per container; a run-from-image form (image, name, ports, volumes, env vars, restart policy, optional memory limit) that pulls the image as its own visible step before running it; tailable log viewing; and an explicit storage-location control that shows Docker's current data directory and offers to move it to /volume (copies the existing data first, only switches over if the move actually succeeds, reverts automatically if Docker fails to start on the new path). Deliberately left out: compose stacks, an exec-into-container shell, Docker Hub image search, network/volume management UI, and bulk operations.",
		},
	},
	{
		Version: "2.0.1",
		Date:    "2026-08-24",
		Notes: []string{
			"Fixed a real bug in the Pushbullet command channel: the device was re-reading its own replies as new incoming commands (since a reply's own text starts with its callsign too), which is why unrecognized commands weren't being ignored -- it was actually replying to itself in a loop. Fixed by tracking each push this device sends and skipping it on the next poll.",
			"Unrecognized commands sent to the device's callsign are now truly ignored (no reply at all) instead of getting an \"unrecognized command\" response.",
			"IP range scan can now target a custom start/end address range instead of only this device's own local subnet -- leave both blank to scan the local subnet as before.",
		},
	},
	{
		Version: "2.0.0",
		Date:    "2026-08-24",
		Notes: []string{
			"New Monitoring tab: watch any host with a ping/TCP/HTTP/DNS check and see its uptime and latency graphed over time; a WAN speed test that runs automatically every 6 hours to document your real connection performance; public IP change tracking with an optional Cloudflare Dynamic DNS updater.",
			"Active scouting, on demand: scan this device's own local network for live hosts, or check a single host for open ports -- both strictly user-triggered, never running in the background on their own.",
			"A passive LAN device list (reads what's already known, no scanning) and a Wake-on-LAN button to remotely wake another device on your network.",
			"Logs from key services are now aggregated onto real storage with 90-day retention and browsable right from the dashboard, instead of vanishing with the system journal's own short rotation.",
			"Scheduled tasks: manage real cron entries from the dashboard instead of needing an SSH session for routine maintenance.",
			"Pushbullet integration: proactive alerts (disk space, a service going down, login lockouts, IP changes, an optional daily digest) plus a two-way command channel -- text this device's callsign (e.g. \"SCOUT001 status\") from your Pushbullet app and it replies, with a confirm step before any action actually runs.",
			"Metrics history now extends to 90 days (was 7) when real storage is mounted.",
			"Fixed two pre-existing bugs in the dashboard's own error handling (Certificates and Front Panel tabs) that could throw an unhandled error instead of failing quietly on a bad server response.",
		},
	},
	{
		Version: "1.9.0",
		Date:    "2026-08-24",
		Notes: []string{
			"Settings tab: added a Timezone dropdown, populated straight from this device's own real list of zones (not a guessed/curated list) -- affects log timestamps and cron/scheduled-task timing, separate from NTP (which keeps the clock accurate, not which zone it displays in).",
		},
	},
	{
		Version: "1.8.0",
		Date:    "2026-08-24",
		Notes: []string{
			"Front Panel tab overhauled: the display now cycles through your own screens (add as many as you want), each holding up to 4 lines you pick from hostname, IP, time, CPU/RAM/disk usage, uptime, or your own custom text -- with a live preview and an on/off toggle plus a configurable cycle interval, all editable right on the page. On by default with a sensible 2-screen starting layout.",
			"New Settings tab: hostname, IP/DHCP, DNS, and NTP (time sync) now live together in one place instead of hostname being buried in Front Panel and IP address being buried in Health.",
			"DNS and NTP are now controllable from the dashboard for the first time -- detects whichever mechanism the device is actually using (systemd-resolved vs. a plain resolv.conf; systemd-timesyncd) rather than assuming one.",
		},
	},
	{
		Version: "1.7.0",
		Date:    "2026-08-24",
		Notes: []string{
			"The front panel display now actually works -- it shows the hostname and IP address, drawn directly by this app rather than depending on a separate service that was never rendering correctly on this hardware. The Front Panel tab's diagnostics and hostname control work the same as before.",
		},
	},
	{
		Version: "1.6.0",
		Date:    "2026-08-23",
		Notes: []string{
			"Processes tab now has a Kill button per row (sends SIGTERM; refuses to kill PID 1 or this console's own process).",
			"Health tab: added a System specs block at the top (CPU model/cores, RAM, disk, kernel, hostname) above the historical charts.",
			"Front Panel tab: real diagnosis for \"panel only shows the UniFi logo\" -- confirmed from an actual device's journal (cloudkey.service runs as root with no device restrictions in its own unit file, yet gets \"operation not permitted\" opening /dev/fb0 -- the signature of AppArmor blocking it, not a permissions bug or ck-ui conflict). Now checks the kernel log for the exact denial and offers a one-click attempt to set just that profile to complain mode.",
			"About tab: corrected authorship -- built by Jay Anderson of FullDuplexTech.com; jnovack/cloudkey credited as the upstream source this app automates, not the author.",
		},
	},
	{
		Version: "1.5.0",
		Date:    "2026-08-23",
		Notes: []string{
			"Health tab: added a top-style process table (PID, user, CPU%, MEM%, RSS, elapsed time, command) via `ps`, plus a summary line (load average, uptime, process count, memory used/total). Auto-refreshes every 3 seconds while the Health tab is open, stops polling when you leave it.",
		},
	},
	{
		Version: "1.4.0",
		Date:    "2026-08-23",
		Notes: []string{
			"Apps tab can now actually install things, not just show status -- full parity with CloudKey Wizard's Optional Extras (fail2ban, unattended-upgrades, Plex, Home Assistant, the full Servarr stack, AutoSSH, Tailscale, and WireGuard fail-closed egress with its full config form). Reuses the exact same bundled scripts CloudKey Wizard runs over SSH, embedded here too and run locally instead -- one source of truth per script, not two copies drifting apart.",
			"Health tab: USB drives can be mounted, unmounted, and browsed (read-only browse + download for now) directly from the dashboard.",
			"Health tab: static IP / DHCP control, with a real safety mechanism -- apply a change and it's provisional for 5 minutes; either confirm it (a splash appears the moment you can reach the dashboard again) or it automatically reverts to the previous address. Survives a service restart mid-window -- the pending change is persisted to disk, not just held in memory.",
		},
	},
	{
		Version: "1.3.0",
		Date:    "2026-08-23",
		Notes: []string{
			"Health tab: added network in/out throughput charts (KB/s, auto-scaling).",
			"Users tab: shows each account's last login time, and a recent login-activity log (success/fail, username, source IP). 5 consecutive failed logins for a username now locks it out for 15 minutes.",
			"Certificates tab: the HTTPS port is now configurable, plus a toggle to redirect port 80 to it. Changing either restarts the service (open connections drop briefly; reconnect at the new address if the port changed).",
			"Added a shared on-device state file (fdtscout-state.json) that CloudKey Wizard also reads/writes -- installing or toggling something from either app's Apps/Extras view is now visible to the other, and a different copy of CloudKey Wizard connecting to this device later picks up real state instead of starting blank.",
		},
	},
	{
		Version: "1.2.0",
		Date:    "2026-08-23",
		Notes: []string{
			"Logged-in sessions now idle-timeout after 5 minutes of inactivity (sliding -- any authenticated request resets the clock), plus a 24-hour absolute cap regardless of activity. The dashboard now bounces to the login page automatically on any expired-session response instead of just failing silently.",
			"Fixed updating an already-installed FDT.Scout: CloudKey Wizard's install Extra now uploads a new binary to a temp path and atomically renames it into place instead of writing directly over the currently-running executable (which Linux refuses -- ETXTBSY), and explicitly restarts the service afterward instead of using enable --now (which doesn't restart an already-active unit).",
		},
	},
	{
		Version: "1.1.0",
		Date:    "2026-08-23",
		Notes: []string{
			"Added the Apps tab: status for every service/container CloudKey Wizard's Optional Extras can install, with start/stop/enable/disable for whatever's already installed but not running.",
			"Added this About tab (version, references, disclaimer, changelog).",
			"Hardened hostname changes: now sets it three ways (hostnamectl, a direct sethostname(2) call, and a direct /etc/hostname write) instead of relying on hostnamectl alone, plus real diagnostics (cloudkey.service's recent log, whether UniFi's original ck-ui.service is still active and likely holding the display) surfaced directly in the Front Panel tab -- in response to a real report that the physical panel didn't update after a hostname change.",
		},
	},
	{
		Version: "1.0.0",
		Date:    "2026-08-23",
		Notes: []string{
			"First version: password-gated login, real web terminal (PTY over WebSocket), TLS certificate generate/install, front-panel hostname control, 7-day CPU/memory/disk health charts.",
		},
	},
}

const AboutText = `Built by Jay Anderson of FullDuplexTech.com (https://fullduplextech.com) -- the original ` +
	`article that started this whole project is at https://fullduplextech.com/turn-unifi-cloud-key-gen-2-into-a-headless-linux-server/. ` +
	`FDT.Scout itself is app-authored (not from jnovack/cloudkey) -- a web console for a Cloud Key ` +
	`converted by CloudKey Wizard, also built by the same author.

It is provided for informational and convenience purposes only, as-is, with no warranty of any ` +
	`kind -- use at your own risk. Neither this tool nor its author is affiliated with Ubiquiti Inc. ` +
	`This tool is given away for free, built and distributed on a personal, non-commercial basis.

References and resources this project builds on:
- jnovack/cloudkey (https://github.com/jnovack/cloudkey) -- not the author of this app, but the source of the Phase 1-5 runbook CloudKey Wizard automates, and the front-panel LCD app this console's hostname control is built around. Full credit to that project for the underlying de-Ubiquitizing research and tooling. Licensed under CC BY-NC-SA 4.0 (https://creativecommons.org/licenses/by-nc-sa/4.0/) -- the runbook scripts CloudKey Wizard bundles verbatim from it remain under those terms; see Scripts/runbook/LICENSE-jnovack-cloudkey.md in that project's repo.

This is a real, root-capable admin console reachable over your network once installed, gated only ` +
	`by the password you set. Treat it like any other root credential.`
