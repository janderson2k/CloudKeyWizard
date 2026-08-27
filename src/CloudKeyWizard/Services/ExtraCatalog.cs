using CloudKeyWizard.Models;

namespace CloudKeyWizard.Services;

/// <summary>
/// Optional post-conversion extras, shown on the Extras step grouped by category. General items are
/// short, standard, idempotent commands authored directly for this app (ExtraItem.ScriptContent,
/// not sourced from jnovack/cloudkey). AutoSSH is real upstream content instead
/// (ExtraItem.BundledScriptFileName, same hash-and-provenance treatment as the Phase 1 steps) with
/// real config inputs via ExtraItem.Params. No visible "coming soon" placeholders remain here --
/// the last one (a self-hosted Headscale server) was removed rather than left half-built, since it
/// targets a separate VPS this app has no relationship with and isn't something this device's own
/// conversion can meaningfully offer; Tailscale's own free coordination service (or a self-hosted
/// Headscale the user stands up themselves) is still reachable manually via the Tailscale Extra's
/// own description.
/// </summary>
public static class ExtraCatalog
{
    /// <summary>A curated (not live-queried) list of common IANA zone names for the timezone
    /// dropdown, covering every major UTC offset and populated region. Curated rather than pulled
    /// from the device's own tzdata like FDT.Scout's Settings-tab equivalent does (`timedatectl
    /// list-timezones`, ~400 entries) -- Extras render all at once here rather than one-at-a-time
    /// like the wizard steps, so there's no natural moment to trigger an SSH round trip just to
    /// populate one dropdown before the user has even connected past Extras. `timedatectl
    /// set-timezone` accepts any of these verbatim.</summary>
    public static readonly List<string> CommonTimezones = new()
    {
        "UTC",
        "America/New_York", "America/Chicago", "America/Denver", "America/Phoenix",
        "America/Los_Angeles", "America/Anchorage", "Pacific/Honolulu",
        "America/Toronto", "America/Vancouver", "America/Mexico_City",
        "America/Sao_Paulo", "America/Argentina/Buenos_Aires", "America/Bogota",
        "Europe/London", "Europe/Dublin", "Europe/Lisbon", "Europe/Madrid", "Europe/Paris",
        "Europe/Berlin", "Europe/Amsterdam", "Europe/Brussels", "Europe/Rome", "Europe/Zurich",
        "Europe/Vienna", "Europe/Warsaw", "Europe/Athens", "Europe/Helsinki", "Europe/Bucharest",
        "Europe/Istanbul", "Europe/Moscow",
        "Africa/Cairo", "Africa/Johannesburg", "Africa/Lagos", "Africa/Nairobi",
        "Asia/Jerusalem", "Asia/Dubai", "Asia/Karachi", "Asia/Kolkata", "Asia/Dhaka",
        "Asia/Bangkok", "Asia/Jakarta", "Asia/Shanghai", "Asia/Hong_Kong", "Asia/Singapore",
        "Asia/Taipei", "Asia/Seoul", "Asia/Tokyo", "Asia/Manila",
        "Australia/Perth", "Australia/Adelaide", "Australia/Darwin", "Australia/Brisbane",
        "Australia/Sydney", "Australia/Melbourne",
        "Pacific/Auckland", "Pacific/Fiji", "Pacific/Guam",
    };

    public static List<ExtraItem> BuildExtras() => new()
    {
        new ExtraItem
        {
            Id = "fdtscout",
            Title = "FDT.Scout web console (recommended, on by default)",
            Category = ExtraCategory.General,
            Description = "A password-gated HTTPS admin console for this device, reachable from a browser instead of needing this app open -- user accounts, a real interactive web terminal (a genuine PTY, not a command box), TLS certificate management (generate a new self-signed cert, or install your own), changing the hostname shown as the front-panel LCD's title (that display has no separate free-text field -- the hostname IS its title, confirmed against jnovack/cloudkey's own docs), and CPU/memory/disk health charts over the last 7 days. App-authored (a small Go program, source under tools/fdtscout/ in this repo, not from jnovack/cloudkey), pre-compiled to a single arm64 Linux binary and embedded the same way the runbook scripts are -- no compiler needed on the device, no network access at install time beyond the SSH connection this app already has open. Runs as its own systemd service (fdtscout.service) as root and listens on port 443. Read this plainly: that means a real root-capable admin console reachable over your network the moment this finishes, gated only by the password you set below -- not a toy, and not something to install and forget. Uses a self-signed certificate until you install a real one from its own Certificates tab.",
            BinaryUploads =
            {
                new BinaryUpload("fdtscout-arm64", "/opt/fdtscout/fdtscout", "755"),
                new BinaryUpload("fdtscout.service", "/etc/systemd/system/fdtscout.service", "644"),
            },
            Params =
            {
                new ExtraParam { EnvVarName = "ADMIN_USERNAME", Label = "Admin username", Value = "admin", IsRequired = true,
                    HelpText = "The account you'll log in with at https://<this device>/. More accounts can be added later from the console's own Users tab." },
                new ExtraParam { EnvVarName = "ADMIN_PASSWORD", Label = "Admin password", IsRequired = true, IsSecret = true,
                    HelpText = "At least 8 characters. Stored only as a bcrypt hash on the device, never in plaintext, never shown again by this app -- write it down now." },
            },
            ScriptContent = """
                #!/bin/bash
                # App-authored installer for FDT.Scout (this app's own web console) -- not
                # jnovack/cloudkey content. The compiled binary and its systemd unit were already
                # placed at /opt/fdtscout/fdtscout and /etc/systemd/system/fdtscout.service by this
                # Extra's BinaryUploads step, before this script ran.
                set -euo pipefail

                : "${ADMIN_USERNAME:?}"
                : "${ADMIN_PASSWORD:?}"

                mkdir -p /opt/fdtscout/data
                mkdir -p /etc/fdtscout/tls

                echo "=== creating/resetting the admin account ==="
                /opt/fdtscout/fdtscout -bootstrap-admin

                echo "=== enabling the service ==="
                systemctl daemon-reload
                systemctl enable fdtscout
                # `restart`, not `enable --now` -- on a fresh install there's nothing running yet so
                # this just starts it (identical to `start`), but on a re-run to pick up an updated
                # binary (now uploaded via an atomic rename, see UploadBinaryFileAsync) `enable --now`
                # would silently leave the OLD process running unchanged, since `--now` only starts a
                # not-yet-running unit rather than reloading an already-active one. `restart` is
                # correct either way -- data under /opt/fdtscout/data and /etc/fdtscout/tls is
                # untouched by any of this, only the binary and this unit get replaced.
                systemctl restart fdtscout

                echo "=== waiting for it to come up ==="
                sleep 2
                if ! systemctl is-active --quiet fdtscout; then
                  echo "ABORT: fdtscout.service did not reach the active state -- check: journalctl -u fdtscout -n 50 --no-pager" >&2
                  systemctl status fdtscout --no-pager || true
                  exit 1
                fi

                echo "=== checking it actually answers HTTPS ==="
                sleep 1
                code=$(curl -sk -o /dev/null -w '%{http_code}' https://127.0.0.1:443/login || echo "000")
                if [ "$code" != "200" ]; then
                  echo "WARNING: expected HTTP 200 from the login page, got '$code' -- the service is running but may not be healthy yet. Check: journalctl -u fdtscout -n 50 --no-pager"
                else
                  echo "OK: login page responded 200."
                fi

                echo
                echo "=== FDT.Scout is up ==="
                echo "Browse to one of these (self-signed cert -- your browser will warn once; accept it, or install a real cert from the Certificates tab after logging in):"
                ip -4 -o addr show scope global 2>/dev/null | while read -r _ _ _ cidr _; do
                  echo "  https://${cidr%%/*}/"
                done
                echo "Log in as: $ADMIN_USERNAME"
                echo
                echo "This is a real root-capable admin console reachable over your network, gated only by the password you just set. Treat it like any other root credential."

                """,
        },
        new ExtraItem
        {
            Id = "tz-utc",
            Title = "Set timezone",
            Category = ExtraCategory.General,
            Description = "Sets the system clock's timezone. UTC (the default below) is the sane choice for a headless server so logs and cron timings stay consistent regardless of where you're connecting from -- pick your own local zone instead if you'd rather logs/cron read in local time.",
            Params =
            {
                new ExtraParam
                {
                    EnvVarName = "TIMEZONE", Label = "Timezone", IsDropdown = true,
                    Options = CommonTimezones, Value = "UTC",
                },
            },
            ScriptContent = "#!/bin/bash\nset -euo pipefail\ntimedatectl set-timezone \"$TIMEZONE\"\ntimedatectl show --property=Timezone\n",
        },
        new ExtraItem
        {
            Id = "unattended-upgrades",
            Title = "Enable automatic security updates",
            Category = ExtraCategory.General,
            Description = "Installs and enables unattended-upgrades so Debian security patches apply on their own, instead of relying on you to remember to run apt upgrade.",
            ScriptContent = "#!/bin/bash\nset -euo pipefail\nDEBIAN_FRONTEND=noninteractive apt-get update\nDEBIAN_FRONTEND=noninteractive apt-get install -y unattended-upgrades\ndpkg-reconfigure -f noninteractive unattended-upgrades\nsystemctl enable --now unattended-upgrades\nsystemctl is-active unattended-upgrades\n",
        },
        new ExtraItem
        {
            Id = "fail2ban",
            Title = "Install fail2ban (SSH brute-force protection)",
            Category = ExtraCategory.General,
            Description = "Installs fail2ban with its default sshd jail -- temporarily bans an IP after repeated failed SSH login attempts.",
            ScriptContent = "#!/bin/bash\nset -euo pipefail\nDEBIAN_FRONTEND=noninteractive apt-get install -y fail2ban\nsystemctl enable --now fail2ban\nfail2ban-client status\n",
        },
        new ExtraItem
        {
            Id = "apt-upgrade-now",
            Title = "Check for & install available updates",
            Category = ExtraCategory.General,
            Description = "Runs apt update + apt upgrade right now, within the box's current Debian release only -- not a release upgrade (see \"Set timezone\" section's neighbor, unattended-upgrades, for the automatic version of this). Safe, routine maintenance.",
            ScriptContent = "#!/bin/bash\nset -euo pipefail\nDEBIAN_FRONTEND=noninteractive apt-get update\nDEBIAN_FRONTEND=noninteractive apt-get upgrade -y\napt list --upgradable 2>/dev/null || true\n",
        },
        new ExtraItem
        {
            Id = "hostname",
            Title = "Set hostname",
            Category = ExtraCategory.General,
            Description = "Sets the system hostname -- worth doing once this box stops being \"the cloud key\" and becomes whatever you're actually using it for.",
            Params = { new ExtraParam { EnvVarName = "NEW_HOSTNAME", Label = "New hostname", IsRequired = true } },
            ScriptContent = "#!/bin/bash\nset -euo pipefail\n: \"${NEW_HOSTNAME:?}\"\nhostnamectl set-hostname \"$NEW_HOSTNAME\"\nif grep -q '^127\\.0\\.1\\.1' /etc/hosts; then\n  sed -i \"s/^127\\.0\\.1\\.1.*/127.0.1.1\\t$NEW_HOSTNAME/\" /etc/hosts\nelse\n  printf '127.0.1.1\\t%s\\n' \"$NEW_HOSTNAME\" >> /etc/hosts\nfi\nhostnamectl status\n",
        },

        new ExtraItem
        {
            Id = "os-release-check",
            Title = "Check current Debian release (diagnostic only)",
            Category = ExtraCategory.General,
            Description = "Prints the box's current Debian codename and configured apt sources -- and deliberately stops there. This app does not automate an actual Debian release upgrade: that exact category of operation (rewriting codenames in sources.list, then a full apt upgrade across releases) is what bricked a device during this project's own development -- see phase1-purge.sh's own comments about this board's initramfs package. If you choose to do a release upgrade anyway, it's a fully manual, unsupported, no-revert-path operation -- this just shows you where things currently stand first.",
            ScriptContent = "#!/bin/bash\nset -uo pipefail\necho \"=== Current OS release ===\"\nif [ -f /etc/os-release ]; then . /etc/os-release; echo \"Codename: ${VERSION_CODENAME:-unknown}  ($PRETTY_NAME)\"; fi\nlsb_release -a 2>/dev/null || true\necho\necho \"=== apt sources currently configured ===\"\ncat /etc/apt/sources.list 2>/dev/null || true\nfor f in /etc/apt/sources.list.d/*.list; do\n  [ -f \"$f\" ] && { echo \"--- $f ---\"; cat \"$f\"; }\ndone\necho\necho \"############################################################\"\necho \"This tool deliberately does NOT perform an automated Debian\"\necho \"release upgrade. A prior attempt at exactly that (rewriting\"\necho \"codenames in sources.list, then apt full-upgrade) on this\"\necho \"same board family is what bricked a device during this\"\necho \"project's own development -- see phase1-purge.sh's own\"\necho \"comments about this board's initramfs package.\"\necho\necho \"If you choose to do this anyway, it is entirely manual and\"\necho \"unsupported: back up anything you care about first, edit the\"\necho \"codenames in the files listed above yourself, then\"\necho \"apt full-upgrade -- at your own risk, with no revert path.\"\necho \"############################################################\"\n",
        },

        new ExtraItem
        {
            Id = "plex",
            Title = "Plex Media Server",
            Category = ExtraCategory.MediaServer,
            Description = "Installs Plex's official ARM64 package via their own apt repo. Uses /volume for media if the storage step ran. You'll need to open the web setup afterward to claim the server with your Plex account -- not something this app automates. No hardware transcoding on this ARM hardware, so direct play only in practice.",
            ScriptContent = "#!/bin/bash\nset -euo pipefail\nif ! dpkg -l plexmediaserver 2>/dev/null | grep -q '^ii'; then\n  DEBIAN_FRONTEND=noninteractive apt-get install -y curl gnupg\n  curl -fsSL https://downloads.plex.tv/plex-keys/PlexSign.key | gpg --dearmor -o /usr/share/keyrings/plex-archive-keyring.gpg\n  echo \"deb [signed-by=/usr/share/keyrings/plex-archive-keyring.gpg] https://downloads.plex.tv/repo/deb public main\" > /etc/apt/sources.list.d/plexmediaserver.list\n  DEBIAN_FRONTEND=noninteractive apt-get update\n  DEBIAN_FRONTEND=noninteractive apt-get install -y plexmediaserver\nelse\n  echo \"plexmediaserver already installed\"\nfi\n\nsystemctl enable --now plexmediaserver\n\nif [ -d /volume ]; then\n  mkdir -p /volume/media\n  echo \"Suggested media location: /volume/media -- create your library folders under here, then add them as libraries in Plex's web setup.\"\nfi\n\nsleep 2\nsystemctl status plexmediaserver --no-pager | head -5 || true\nIP=$(hostname -I | awk '{print $1}')\necho\necho \"Open http://$IP:32400/web to claim this server with your Plex account and finish setup.\"\n",
        },
        new ExtraItem
        {
            Id = "media-nzbget",
            Title = "NZBGet (download client)",
            Category = ExtraCategory.MediaServer,
            Description = "Real upstream jnovack/cloudkey script (Phase 2). Installs NZBGet via its official apt repo, points it at /volume/downloads, and sets a random control-panel password -- shown once in the terminal log, write it down. Automatically provisions the shared /volume/media directory structure first if that hasn't run yet.",
            BundledScriptFileName = "phase2-01-nzbget.sh",
            PrerequisiteBundledScriptFileName = "phase2-00-provision-drive.sh",
        },
        new ExtraItem
        {
            Id = "media-sonarr",
            Title = "Sonarr (TV shows)",
            Category = ExtraCategory.MediaServer,
            Description = "Real upstream jnovack/cloudkey script (Phase 2). Installs Sonarr via its official installer script, patched to run non-interactively (the upstream installer reads its user/group prompts straight from the terminal, bypassing stdin, so it can't be answered the normal non-interactive way -- the bundled script handles this by patching two lines out of the downloaded installer before running it). Provisions /volume/media first if needed.",
            BundledScriptFileName = "phase2-02-sonarr.sh",
            PrerequisiteBundledScriptFileName = "phase2-00-provision-drive.sh",
        },
        new ExtraItem
        {
            Id = "media-radarr",
            Title = "Radarr (movies)",
            Category = ExtraCategory.MediaServer,
            Description = "Real upstream jnovack/cloudkey script (Phase 2). Installs Radarr, then automatically detects and repairs a known GLIBC/SQLite mismatch on this OS if present (Radarr bundles a database helper built against a newer GLIBC than this box ships). Provisions /volume/media first if needed.",
            BundledScriptFileName = "phase2-03-radarr.sh",
            PrerequisiteBundledScriptFileName = "phase2-00-provision-drive.sh",
            SiblingBundledScriptFileNames = { "phase2-05-servarr-sqlite-heal.sh" },
        },
        new ExtraItem
        {
            Id = "media-prowlarr",
            Title = "Prowlarr (indexer manager)",
            Category = ExtraCategory.MediaServer,
            Description = "Real upstream jnovack/cloudkey script (Phase 2). Installs Prowlarr -- it talks to the other three over HTTP rather than touching /volume directly, so no drive provisioning needed -- and proactively repairs the same GLIBC/SQLite issue Radarr can hit.",
            BundledScriptFileName = "phase2-04-prowlarr.sh",
            SiblingBundledScriptFileNames = { "phase2-05-servarr-sqlite-heal.sh" },
        },

        new ExtraItem
        {
            Id = "home-assistant",
            Title = "Home Assistant",
            Category = ExtraCategory.HomeAutomation,
            Description = "Installs Docker if needed, then runs Home Assistant Container (not Home Assistant OS -- that needs a dedicated bootable image, incompatible with this device) with host networking and a persistent config volume on /volume if mounted. This hardware is resource-constrained; the script prints current memory before starting so you can sanity-check it. No Zigbee/Z-Wave without spare USB and a dongle -- cloud-integration-only on stock hardware. First-run setup is a web wizard, not something this app can script.",
            ScriptContent = "#!/bin/bash\nset -euo pipefail\nif ! command -v docker >/dev/null 2>&1; then\n  DEBIAN_FRONTEND=noninteractive apt-get update\n  DEBIAN_FRONTEND=noninteractive apt-get install -y docker.io\n  systemctl enable --now docker\nfi\n\necho \"=== current memory (Home Assistant is not light -- sanity-check this) ===\"\nfree -h\n\nCONFIG_DIR=/root/homeassistant\nif [ -d /volume ]; then\n  CONFIG_DIR=/volume/homeassistant\nfi\nmkdir -p \"$CONFIG_DIR\"\n\nif docker inspect homeassistant >/dev/null 2>&1; then\n  echo \"A 'homeassistant' container already exists -- leaving it alone. To reset: docker rm -f homeassistant\"\nelse\n  docker run -d --name homeassistant --restart=unless-stopped --network=host \\\n    -v \"$CONFIG_DIR:/config\" -e TZ=UTC \\\n    ghcr.io/home-assistant/home-assistant:stable\nfi\n\nsleep 3\ndocker ps --filter name=homeassistant\nIP=$(hostname -I | awk '{print $1}')\necho\necho \"Home Assistant is starting -- first boot can take a minute or two.\"\necho \"Once up, open: http://$IP:8123\"\n",
        },

        new ExtraItem
        {
            Id = "autossh",
            Title = "AutoSSH rescue tunnel",
            Category = ExtraCategory.RemoteAccess,
            Description = "Real upstream jnovack/cloudkey script (Phase 3), not app-authored. Installs autossh, generates a keypair per relay, and writes a systemd unit that keeps a reverse SSH tunnel open to a relay host you control -- so you can reach this box even if its normal network path goes down. Only sets up THIS side: it deliberately leaves the tunnel disabled and prints a relay-side setup block (public keys only) for you to run by hand on the relay/VPS, which needs a sudo-capable account this app has no way to reach. Enable the tunnel yourself once that's installed.",
            BundledScriptFileName = "phase3-autossh-client.sh",
            Params =
            {
                new ExtraParam { EnvVarName = "RELAYS", Label = "Relay host(s)", IsRequired = true,
                    HelpText = "Space-separated. Either bare hosts (\"relay.example.com\") or name=host pairs (\"home=relay1.example.com office=relay2.example.com\") for more than one." },
                new ExtraParam { EnvVarName = "CLIENT_NAME", Label = "Client name (optional)", IsRequired = false,
                    HelpText = "Identifies this box to the relay. Defaults to its hostname if left blank." },
                new ExtraParam { EnvVarName = "TIER1_PORT", Label = "Rescue SSH port on relay (optional)", IsRequired = false,
                    HelpText = "Relay-side port that forwards back to this box's SSH. Defaults to 2345." },
                new ExtraParam { EnvVarName = "WEBUI_PORTS", Label = "Extra web-UI ports to tunnel (optional)", IsRequired = false,
                    HelpText = "Space-separated, e.g. \"8989 7878\" or \"8080:80\" if the relay-side port must differ. Leave blank to skip -- most people only need the SSH rescue tunnel." },
            },
        },
        new ExtraItem
        {
            Id = "tailscale-client",
            Title = "Join a Tailscale network",
            Category = ExtraCategory.RemoteAccess,
            Description = "Real upstream jnovack/cloudkey script (Phase 5). Installs the Tailscale client (tailscaled) -- no config needed for this step. Joining a tailnet is a deliberate manual step afterward (this app can't automate it; it needs a login server and auth key only you can supply): open Drop to shell and run `tailscale up --login-server=https://<your-headscale> --authkey=<key>` for a self-hosted Headscale server, or just `tailscale up` on its own to use Tailscale's own free coordination service instead -- no self-hosted server needed at all for personal use.",
            BundledScriptFileName = "phase5-01-tailscale-client.sh",
        },
        new ExtraItem
        {
            Id = "wireguard",
            Title = "WireGuard VPN (fail-closed egress)",
            Category = ExtraCategory.RemoteAccess,
            Description = "Puts a chosen set of services (e.g. NZBGet/Sonarr) inside an isolated network namespace whose ONLY route out is a WireGuard VPN tunnel -- if the tunnel drops, they lose all connectivity rather than silently falling back to your normal connection. Built from jnovack/cloudkey's real Phase 4 design: the 7 scripts bundled with this app (uploaded and placed verbatim, only the <placeholder> tokens substituted with what you enter below) plus the exact systemd unit definitions documented at github.com/jnovack/cloudkey/wiki/Phase-4-WireGuard, copied here verbatim -- not this app's own invention. Requires a WireGuard config file from your VPN provider (paste its whole content below, private key included -- it's written to disk mode 600, root-only, and never shown again in this app). Runs a real verification pass at the end and tells you plainly if the kill switch didn't come up clean. Idempotent -- safe to re-run if something needs fixing.",
            SiblingBundledScriptFileNames =
            {
                "phase4-netns-vpn-up.sh", "phase4-netns-vpn-down.sh", "phase4-vpn-peer-route.sh",
                "phase4-vpn-check.sh", "phase4-vpn-heal.sh", "phase4-vpn-verify-netns.sh", "phase4-vpn-verify-service.sh",
            },
            Params =
            {
                new ExtraParam { EnvVarName = "VPN_NS", Label = "Namespace/interface name", Value = "vpn", IsRequired = true,
                    HelpText = "What to call the isolated namespace and tunnel interface. \"vpn\" is fine unless you have a reason to change it." },
                new ExtraParam { EnvVarName = "WG_CONFIG", Label = "WireGuard config (from your VPN provider)", IsRequired = true, IsMultiline = true, IsSecret = true,
                    HelpText = "Paste the full contents of the .conf file your VPN provider gave you -- [Interface] and [Peer] sections, private key included. Written to /etc/wireguard/<name>.conf, mode 600, root-only." },
                new ExtraParam { EnvVarName = "PROVIDER_DNS_IP", Label = "VPN provider's DNS server IP", IsRequired = true,
                    HelpText = "Keeps DNS queries inside the tunnel too, instead of leaking query patterns to your normal resolver. Your provider's docs/config will list this." },
                new ExtraParam { EnvVarName = "PORTS", Label = "Ports to keep reachable from outside", IsRequired = true,
                    HelpText = "Space-separated, e.g. \"8989 7878\" -- every port a protected service listens on. Traffic to these stays reachable normally; only those services' own OUTBOUND traffic goes through the tunnel." },
                new ExtraParam { EnvVarName = "SERVICES", Label = "systemd service names to protect", IsRequired = true,
                    HelpText = "Space-separated, no \".service\" suffix, e.g. \"nzbget sonarr radarr\" -- must already be installed and running (see the Media/Download Server Extras above)." },
                new ExtraParam { EnvVarName = "PROVIDER_CHECK_URL", Label = "VPN provider's connectivity-check URL", IsRequired = true,
                    HelpText = "A URL that confirms you're actually on the VPN when you fetch it, e.g. your provider's own \"am I connected\" endpoint." },
                new ExtraParam { EnvVarName = "PROVIDER_NAME", Label = "Text confirming a successful check", IsRequired = true,
                    HelpText = "A substring expected in that URL's response when genuinely connected -- e.g. your provider's name, so a random web page can't be mistaken for a real check." },
            },
            ScriptContent = """
                #!/bin/bash
                # App-authored orchestration (NOT verbatim jnovack/cloudkey content) that assembles
                # jnovack/cloudkey's real Phase 4 design from the 7 bundled scripts.runbook/ files
                # (uploaded as siblings alongside this script, renamed/placed per the wiki's file
                # manifest) plus the exact systemd unit definitions documented at
                # https://github.com/jnovack/cloudkey/wiki/Phase-4-WireGuard -- copied here verbatim,
                # not invented. That page explicitly says it's written so "a reader or their agent"
                # can implement it fully from the doc; this is that.
                set -euo pipefail

                : "${VPN_NS:?}"
                : "${PROVIDER_DNS_IP:?}"
                : "${PORTS:?}"
                : "${SERVICES:?}"
                : "${PROVIDER_CHECK_URL:?}"
                : "${PROVIDER_NAME:?}"
                : "${WG_CONFIG:?}"

                WORKDIR="/root/.cloudkey-wizard"

                echo "=== detecting uplink interface ==="
                UPLINK=$(ip route show default | awk '{print $5}' | head -1)
                if [ -z "$UPLINK" ]; then
                  echo "error: couldn't auto-detect the uplink network interface (no default route on this box?)" >&2
                  exit 1
                fi
                echo "uplink: $UPLINK"

                echo "=== installing WireGuard tooling ==="
                DEBIAN_FRONTEND=noninteractive apt-get update
                DEBIAN_FRONTEND=noninteractive apt-get install --no-install-recommends -y wireguard-tools wireguard-go openresolv

                echo "=== writing WireGuard peer config ==="
                install -d -m 700 /etc/wireguard
                printf '%s\n' "$WG_CONFIG" > "/etc/wireguard/${VPN_NS}.conf"
                chmod 600 "/etc/wireguard/${VPN_NS}.conf"
                chown root:root "/etc/wireguard/${VPN_NS}.conf"

                echo "=== placing + substituting the bundled runbook scripts ==="
                declare -A RENAME=(
                  [phase4-netns-vpn-up.sh]=netns-vpn-up.sh
                  [phase4-netns-vpn-down.sh]=netns-vpn-down.sh
                  [phase4-vpn-peer-route.sh]=vpn-peer-route.sh
                  [phase4-vpn-check.sh]=vpn-check.sh
                  [phase4-vpn-heal.sh]=vpn-heal.sh
                  [phase4-vpn-verify-netns.sh]=vpn-verify-netns.sh
                  [phase4-vpn-verify-service.sh]=vpn-verify-service.sh
                )
                for src in "${!RENAME[@]}"; do
                  install -m 755 "$WORKDIR/$src" "/usr/local/sbin/${RENAME[$src]}"
                done

                sed -i "s|<vpn>|${VPN_NS}|g; s|<uplink>|${UPLINK}|g; s|<provider-dns-ip>|${PROVIDER_DNS_IP}|g; s|<port> <port>|${PORTS}|g" \
                  /usr/local/sbin/netns-vpn-up.sh
                sed -i "s|<vpn>|${VPN_NS}|g" /usr/local/sbin/netns-vpn-down.sh
                sed -i "s|<vpn>|${VPN_NS}|g" /usr/local/sbin/vpn-peer-route.sh
                sed -i "s|<vpn>|${VPN_NS}|g; s|<provider-check-url>|${PROVIDER_CHECK_URL}|g; s|<provider-name>|${PROVIDER_NAME}|g" \
                  /usr/local/sbin/vpn-check.sh
                sed -i "s|<service> <service>|${SERVICES}|g; s|<provider-dns-ip>|${PROVIDER_DNS_IP}|g" /usr/local/sbin/vpn-heal.sh
                sed -i "s|<vpn>|${VPN_NS}|g; s|<provider-check-url>|${PROVIDER_CHECK_URL}|g; s|<provider-name>|${PROVIDER_NAME}|g; s|<service> <service>|${SERVICES}|g" \
                  /usr/local/sbin/vpn-verify-netns.sh
                sed -i "s|<vpn>|${VPN_NS}|g; s|<provider-check-url>|${PROVIDER_CHECK_URL}|g; s|<provider-name>|${PROVIDER_NAME}|g; s|<service> <service>|${SERVICES}|g" \
                  /usr/local/sbin/vpn-verify-service.sh

                echo "=== writing systemd units (exact content from the Phase-4-WireGuard wiki page) ==="
                cat > /etc/systemd/system/netns-vpn.service <<'UNIT'
                [Unit]
                Description=Create isolated network namespace for VPN-only egress
                After=network.target
                Before=wg-quick-vpn.service

                [Service]
                Type=oneshot
                RemainAfterExit=yes
                ExecStart=/usr/local/sbin/netns-vpn-up.sh
                ExecStop=/usr/local/sbin/netns-vpn-down.sh

                [Install]
                WantedBy=multi-user.target
                UNIT

                SERVICE_UNITS=""
                for s in $SERVICES; do SERVICE_UNITS="${SERVICE_UNITS}${s}.service "; done
                SERVICE_UNITS="${SERVICE_UNITS% }"

                cat > /etc/systemd/system/wg-quick-vpn.service <<UNIT
                [Unit]
                Description=VPN tunnel (userspace, inside isolated netns)
                After=netns-vpn.service
                Requires=netns-vpn.service
                Before=${SERVICE_UNITS}

                [Service]
                Type=oneshot
                RemainAfterExit=yes
                NetworkNamespacePath=/var/run/netns/${VPN_NS}
                Environment=WG_QUICK_USERSPACE_IMPLEMENTATION=wireguard
                ExecStart=/usr/bin/wg-quick up ${VPN_NS}
                ExecStartPost=/sbin/ip route add default dev ${VPN_NS}
                ExecStartPost=/usr/local/sbin/vpn-peer-route.sh add
                ExecStop=/usr/bin/wg-quick down ${VPN_NS}
                ExecStopPost=/usr/local/sbin/vpn-peer-route.sh del

                [Install]
                WantedBy=multi-user.target
                UNIT

                cat > /etc/systemd/system/vpn-heal.service <<'UNIT'
                [Unit]
                Description=Check VPN tunnel health and heal it if down

                [Service]
                Type=oneshot
                ExecStart=/usr/local/sbin/vpn-heal.sh
                UNIT

                cat > /etc/systemd/system/vpn-heal.timer <<'UNIT'
                [Unit]
                Description=Periodically check/heal the VPN tunnel

                [Timer]
                OnBootSec=60
                OnUnitActiveSec=60
                AccuracySec=5

                [Install]
                WantedBy=timers.target
                UNIT

                echo "=== pinning protected services into the namespace ==="
                for s in $SERVICES; do
                  mkdir -p "/etc/systemd/system/${s}.service.d"
                  cat > "/etc/systemd/system/${s}.service.d/vpn.conf" <<UNIT
                [Unit]
                After=wg-quick-vpn.service
                Requires=wg-quick-vpn.service

                [Service]
                NetworkNamespacePath=/var/run/netns/${VPN_NS}
                TemporaryFileSystem=/run/systemd/resolve:ro
                BindReadOnlyPaths=/etc/netns/${VPN_NS}/resolv.conf:/run/systemd/resolve/stub-resolv.conf
                MountFlags=slave
                UNIT
                done

                echo "=== enabling ==="
                systemctl daemon-reload
                systemctl enable --now netns-vpn.service
                systemctl enable --now wg-quick-vpn.service
                sleep 3
                echo "--- wg show ---"
                wg show "$VPN_NS" latest-handshakes || true

                for s in $SERVICES; do
                  systemctl restart "$s"
                done

                systemctl enable --now vpn-heal.timer

                echo
                echo "=== verifying the kill switch actually holds -- read this carefully ==="
                sleep 2
                if /usr/local/sbin/vpn-verify-netns.sh $SERVICES; then
                  echo "Verification passed."
                else
                  echo "VERIFICATION REPORTED PROBLEMS ABOVE. Do not assume traffic is protected until these are resolved."
                fi

                echo
                echo "Re-run verification any time with:"
                echo "  /usr/local/sbin/vpn-verify-netns.sh"
                echo "  /usr/local/sbin/vpn-verify-service.sh"
                echo "Full reference: https://github.com/jnovack/cloudkey/wiki/Phase-4-WireGuard"

                """,
        },
        new ExtraItem
        {
            Id = "restic-server",
            Title = "Restic backup server (rest-server)",
            Category = ExtraCategory.Backup,
            Description = "Real upstream project (restic/rest-server on GitHub, not jnovack/cloudkey content) -- turns this device into a real backup destination for OTHER machines on your network. Installs the official pre-built ARM64 binary (checksum-verified before it's trusted), pinned to a specific version. Backups are encrypted on the CLIENT side before they ever reach this box, so this device never sees your actual data, only encrypted blobs. Data lives on /volume if that's mounted (strongly recommended -- re-run this after Format & Mount Storage if you skipped it), otherwise the small root partition. Retention/pruning is controlled entirely from the client side (`restic forget --prune` with whatever policy you choose) -- this server just stores what it's given.",
            Params =
            {
                new ExtraParam { EnvVarName = "BACKUP_USERNAME", Label = "First backup username", Value = "backup", IsRequired = true,
                    HelpText = "Each user gets their own isolated repository automatically -- add more later from the terminal (htpasswd -B /etc/rest-server.htpasswd <newuser>)." },
                new ExtraParam { EnvVarName = "BACKUP_PASSWORD", Label = "Password for that user", IsRequired = true, IsSecret = true,
                    HelpText = "Used for HTTP Basic Auth from restic clients -- write it down, it's never shown again by this app." },
            },
            ScriptContent = """
                #!/bin/bash
                # Real upstream project: restic/rest-server (https://github.com/restic/rest-server) --
                # not jnovack/cloudkey content, not app-authored logic beyond this install/setup
                # orchestration. Installs the real official binary release, pinned to a specific
                # version and checksum-verified before it's trusted, same discipline this project
                # already applies to jnovack's own bundled scripts.
                set -euo pipefail

                : "${BACKUP_USERNAME:?}"
                : "${BACKUP_PASSWORD:?}"

                REST_SERVER_VERSION="0.14.0"
                ASSET="rest-server_${REST_SERVER_VERSION}_linux_arm64.tar.gz"
                BASE_URL="https://github.com/restic/rest-server/releases/download/v${REST_SERVER_VERSION}"

                WORKDIR=$(mktemp -d)
                trap 'rm -rf "$WORKDIR"' EXIT
                cd "$WORKDIR"

                echo "=== downloading rest-server v${REST_SERVER_VERSION} (real upstream release, arm64) ==="
                curl -fsSL -o "$ASSET" "$BASE_URL/$ASSET"
                curl -fsSL -o SHA256SUMS "$BASE_URL/SHA256SUMS"

                echo "=== verifying checksum against upstream's own SHA256SUMS ==="
                grep " $ASSET\$" SHA256SUMS | sha256sum -c -

                tar xzf "$ASSET"
                install -m 755 rest-server /usr/local/bin/rest-server
                echo "installed: $(rest-server --version)"

                # Data location: prefer /volume (the real drive) over the tiny root partition -- same
                # "use the big drive for anything that grows without bound" discipline already applied
                # to log aggregation and extended metrics history elsewhere in this app.
                DATA_DIR=/root/restic-data
                if [ -d /volume ]; then
                  DATA_DIR=/volume/restic-data
                else
                  echo "WARNING: /volume isn't mounted -- backup data will live on the small root partition ($DATA_DIR). Run Format & Mount Storage first if you have the extra drive, then re-run this Extra to move onto it (data already backed up won't move itself)."
                fi
                mkdir -p "$DATA_DIR"

                # Auth: HTTP Basic Auth via .htpasswd. --private-repos gives each authenticated user
                # their own isolated repo directory automatically (DATA_DIR/<username>/) instead of
                # one shared repo everyone with a login could see into.
                if ! command -v htpasswd >/dev/null 2>&1; then
                  DEBIAN_FRONTEND=noninteractive apt-get update
                  DEBIAN_FRONTEND=noninteractive apt-get install -y apache2-utils
                fi
                HTPASSWD_FILE=/etc/rest-server.htpasswd
                if [ -f "$HTPASSWD_FILE" ] && grep -q "^${BACKUP_USERNAME}:" "$HTPASSWD_FILE"; then
                  htpasswd -bB "$HTPASSWD_FILE" "$BACKUP_USERNAME" "$BACKUP_PASSWORD"
                  echo "updated password for existing user '$BACKUP_USERNAME'"
                else
                  htpasswd -bBc "$HTPASSWD_FILE" "$BACKUP_USERNAME" "$BACKUP_PASSWORD"
                  echo "created user '$BACKUP_USERNAME'"
                fi
                chmod 640 "$HTPASSWD_FILE"

                # TLS: a real self-signed cert, generated ONCE and reused across re-runs of this Extra
                # -- regenerating it every time would mean every already-configured restic client
                # suddenly distrusts the server after any unrelated re-run (e.g. changing a password).
                CERT_DIR=/etc/rest-server-tls
                mkdir -p "$CERT_DIR"
                if [ ! -f "$CERT_DIR/cert.pem" ]; then
                  openssl req -x509 -newkey rsa:2048 -keyout "$CERT_DIR/key.pem" -out "$CERT_DIR/cert.pem" \
                    -days 3650 -nodes -subj "/CN=$(hostname)"
                fi

                cat > /etc/systemd/system/rest-server.service <<UNIT
                [Unit]
                Description=Restic REST backup server
                After=network.target

                [Service]
                ExecStart=/usr/local/bin/rest-server --path $DATA_DIR --private-repos --htpasswd-file $HTPASSWD_FILE --tls --tls-cert $CERT_DIR/cert.pem --tls-key $CERT_DIR/key.pem --listen :8000
                Restart=on-failure
                User=root

                [Install]
                WantedBy=multi-user.target
                UNIT

                systemctl daemon-reload
                systemctl enable rest-server
                systemctl restart rest-server

                sleep 2
                if ! systemctl is-active --quiet rest-server; then
                  echo "ABORT: rest-server.service did not reach the active state -- check: journalctl -u rest-server -n 50 --no-pager" >&2
                  systemctl status rest-server --no-pager || true
                  exit 1
                fi

                IP=$(hostname -I | awk '{print $1}')
                echo
                echo "=== rest-server is up, listening on :8000 (HTTPS, self-signed cert) ==="
                echo "From another machine with restic installed:"
                echo "  1. Copy this device's certificate so the client trusts it: $CERT_DIR/cert.pem"
                echo "  2. export RESTIC_REPOSITORY=rest:https://$BACKUP_USERNAME:<password>@$IP:8000/"
                echo "  3. restic --cacert /path/to/copied/cert.pem init"
                echo "  4. restic --cacert /path/to/copied/cert.pem backup /path/to/back/up"
                echo
                echo "Retention/pruning is controlled entirely from the CLIENT side -- run 'restic forget --prune' there with whatever policy you choose (e.g. --keep-daily 7 --keep-weekly 4). This server only stores what it's given; it doesn't decide what to keep or delete."
                echo "To add more backup users later: htpasswd -B $HTPASSWD_FILE <newuser>, then: systemctl restart rest-server"

                """,
        },
    };
}
