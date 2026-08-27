package main

import "embed"

// Real installs, reusing the exact same scripts CloudKey Wizard bundles (byte-identical copies
// under scripts/, embedded here too) rather than reimplementing install logic a second time in Go
// -- one source of truth per script, both apps just run it from a different transport (SSH+SFTP
// from Windows, direct local exec here since FDT.Scout already runs ON the device as root).
// IDs match apps.go's status-catalog IDs exactly, so the Apps tab shows one unified row per app
// with both "is it installed/running" and "install it" in the same place.

//go:embed scripts
var bundledInstallScripts embed.FS

type InstallParam struct {
	EnvVar   string `json:"envVar"`
	Label    string `json:"label"`
	HelpText string `json:"helpText"`
	Required bool   `json:"required"`
	Default  string `json:"default"`
}

type InstallDef struct {
	ID                      string         `json:"id"`
	Title                   string         `json:"title"`
	Description             string         `json:"description"`
	Params                  []InstallParam `json:"params"`
	ScriptContent           string         `json:"-"` // app-authored, inline
	BundledScriptFile       string         `json:"-"` // real jnovack/cloudkey content, under scripts/
	PrerequisiteScriptFile  string         `json:"-"`
	SiblingScriptFiles      []string       `json:"-"`
}

var installCatalog = []InstallDef{
	{
		ID:          "fail2ban",
		Title:       "fail2ban",
		Description: "App-authored, not from jnovack/cloudkey. Installs and enables fail2ban with its stock defaults.",
		ScriptContent: "#!/bin/bash\nset -euo pipefail\nDEBIAN_FRONTEND=noninteractive apt-get install -y fail2ban\nsystemctl enable --now fail2ban\nfail2ban-client status\n",
	},
	{
		ID:          "unattended-upgrades",
		Title:       "Automatic security updates",
		Description: "App-authored, not from jnovack/cloudkey. Installs and enables unattended-upgrades.",
		ScriptContent: "#!/bin/bash\nset -euo pipefail\nDEBIAN_FRONTEND=noninteractive apt-get update\nDEBIAN_FRONTEND=noninteractive apt-get install -y unattended-upgrades\ndpkg-reconfigure -f noninteractive unattended-upgrades\nsystemctl enable --now unattended-upgrades\nsystemctl is-active unattended-upgrades\n",
	},
	{
		ID:          "plex",
		Title:       "Plex Media Server",
		Description: "App-authored, not from jnovack/cloudkey. Installs Plex's official ARM64 package via their own apt repo. Uses /volume for media if mounted. Claim the server with your Plex account afterward at the URL this prints.",
		ScriptContent: "#!/bin/bash\nset -euo pipefail\nif ! dpkg -l plexmediaserver 2>/dev/null | grep -q '^ii'; then\n  DEBIAN_FRONTEND=noninteractive apt-get install -y curl gnupg\n  curl -fsSL https://downloads.plex.tv/plex-keys/PlexSign.key | gpg --dearmor -o /usr/share/keyrings/plex-archive-keyring.gpg\n  echo \"deb [signed-by=/usr/share/keyrings/plex-archive-keyring.gpg] https://downloads.plex.tv/repo/deb public main\" > /etc/apt/sources.list.d/plexmediaserver.list\n  DEBIAN_FRONTEND=noninteractive apt-get update\n  DEBIAN_FRONTEND=noninteractive apt-get install -y plexmediaserver\nelse\n  echo \"plexmediaserver already installed\"\nfi\n\nsystemctl enable --now plexmediaserver\n\nif [ -d /volume ]; then\n  mkdir -p /volume/media\n  echo \"Suggested media location: /volume/media -- create your library folders under here, then add them as libraries in Plex's web setup.\"\nfi\n\nsleep 2\nsystemctl status plexmediaserver --no-pager | head -5 || true\nIP=$(hostname -I | awk '{print $1}')\necho\necho \"Open http://$IP:32400/web to claim this server with your Plex account and finish setup.\"\n",
	},
	{
		ID:          "home-assistant",
		Title:       "Home Assistant",
		Description: "App-authored, not from jnovack/cloudkey. Installs Docker if needed, then runs Home Assistant Container (not Home Assistant OS) with host networking. This hardware is resource-constrained; prints free memory first.",
		ScriptContent: "#!/bin/bash\nset -euo pipefail\nif ! command -v docker >/dev/null 2>&1; then\n  DEBIAN_FRONTEND=noninteractive apt-get update\n  DEBIAN_FRONTEND=noninteractive apt-get install -y docker.io\n  systemctl enable --now docker\nfi\n\necho \"=== current memory (Home Assistant is not light -- sanity-check this) ===\"\nfree -h\n\nCONFIG_DIR=/root/homeassistant\nif [ -d /volume ]; then\n  CONFIG_DIR=/volume/homeassistant\nfi\nmkdir -p \"$CONFIG_DIR\"\n\nif docker inspect homeassistant >/dev/null 2>&1; then\n  echo \"A 'homeassistant' container already exists -- leaving it alone. To reset: docker rm -f homeassistant\"\nelse\n  docker run -d --name homeassistant --restart=unless-stopped --network=host \\\n    -v \"$CONFIG_DIR:/config\" -e TZ=UTC \\\n    ghcr.io/home-assistant/home-assistant:stable\nfi\n\nsleep 3\ndocker ps --filter name=homeassistant\nIP=$(hostname -I | awk '{print $1}')\necho\necho \"Home Assistant is starting -- first boot can take a minute or two.\"\necho \"Once up, open: http://$IP:8123\"\n",
	},
	{
		ID:                     "nzbget",
		Title:                  "NZBGet (download client)",
		Description:            "Real upstream jnovack/cloudkey script (Phase 2). Installs NZBGet, points it at /volume/downloads, sets a random control-panel password shown once at the end -- write it down.",
		BundledScriptFile:      "phase2-01-nzbget.sh",
		PrerequisiteScriptFile: "phase2-00-provision-drive.sh",
	},
	{
		ID:                     "sonarr",
		Title:                  "Sonarr (TV shows)",
		Description:            "Real upstream jnovack/cloudkey script (Phase 2). Installs Sonarr via its official installer, patched to run non-interactively.",
		BundledScriptFile:      "phase2-02-sonarr.sh",
		PrerequisiteScriptFile: "phase2-00-provision-drive.sh",
	},
	{
		ID:                     "radarr",
		Title:                  "Radarr (movies)",
		Description:            "Real upstream jnovack/cloudkey script (Phase 2). Installs Radarr, then detects/repairs a known GLIBC/SQLite mismatch on this OS if present.",
		BundledScriptFile:      "phase2-03-radarr.sh",
		PrerequisiteScriptFile: "phase2-00-provision-drive.sh",
		SiblingScriptFiles:     []string{"phase2-05-servarr-sqlite-heal.sh"},
	},
	{
		ID:                 "prowlarr",
		Title:              "Prowlarr (indexer manager)",
		Description:        "Real upstream jnovack/cloudkey script (Phase 2). Talks to the other Servarr apps over HTTP, no drive provisioning needed.",
		BundledScriptFile:  "phase2-04-prowlarr.sh",
		SiblingScriptFiles: []string{"phase2-05-servarr-sqlite-heal.sh"},
	},
	{
		ID:                "tailscale",
		Title:             "Tailscale client",
		Description:       "Real upstream jnovack/cloudkey script (Phase 5). Installs tailscaled -- join a tailnet afterward via the terminal: tailscale up (or --login-server=... for a self-hosted Headscale server).",
		BundledScriptFile: "phase5-01-tailscale-client.sh",
	},
	{
		ID:                "autossh",
		Title:             "AutoSSH rescue tunnel",
		Description:       "Real upstream jnovack/cloudkey script (Phase 3). Sets up this side of a reverse SSH tunnel to a relay host you control; leaves the tunnel disabled and prints the relay-side setup block for you to run there by hand.",
		BundledScriptFile: "phase3-autossh-client.sh",
		Params: []InstallParam{
			{EnvVar: "RELAYS", Label: "Relay host(s)", Required: true, HelpText: "Space-separated -- bare hosts, or name=host pairs for more than one."},
			{EnvVar: "CLIENT_NAME", Label: "Client name (optional)", HelpText: "Identifies this box to the relay. Defaults to its hostname."},
			{EnvVar: "TIER1_PORT", Label: "Rescue SSH port on relay (optional)", HelpText: "Defaults to 2345."},
			{EnvVar: "WEBUI_PORTS", Label: "Extra web-UI ports to tunnel (optional)", HelpText: "Space-separated, e.g. \"8989 7878\". Leave blank to skip."},
		},
	},
	{
		ID:                 "wireguard-egress",
		Title:              "WireGuard VPN (fail-closed egress)",
		Description:        "Puts chosen services inside an isolated network namespace whose only route out is a WireGuard tunnel. App-authored orchestrator assembling the 7 real bundled Phase 4 scripts plus the systemd units documented on the upstream wiki -- same design CloudKey Wizard's own WireGuard Extra uses.",
		SiblingScriptFiles: []string{
			"phase4-netns-vpn-up.sh", "phase4-netns-vpn-down.sh", "phase4-vpn-peer-route.sh",
			"phase4-vpn-check.sh", "phase4-vpn-heal.sh", "phase4-vpn-verify-netns.sh", "phase4-vpn-verify-service.sh",
		},
		Params: []InstallParam{
			{EnvVar: "VPN_NS", Label: "Namespace/interface name", Default: "vpn", Required: true, HelpText: "\"vpn\" is fine unless you have a reason to change it."},
			{EnvVar: "WG_CONFIG", Label: "WireGuard config (from your VPN provider)", Required: true, HelpText: "Paste the full .conf file content -- private key included. Written to /etc/wireguard/<name>.conf, mode 600."},
			{EnvVar: "PROVIDER_DNS_IP", Label: "VPN provider's DNS server IP", Required: true},
			{EnvVar: "PORTS", Label: "Ports to keep reachable from outside", Required: true, HelpText: "Space-separated, e.g. \"8989 7878\"."},
			{EnvVar: "SERVICES", Label: "systemd service names to protect", Required: true, HelpText: "Space-separated, no \".service\" suffix, e.g. \"nzbget sonarr\"."},
			{EnvVar: "PROVIDER_CHECK_URL", Label: "VPN provider's connectivity-check URL", Required: true},
			{EnvVar: "PROVIDER_NAME", Label: "Text confirming a successful check", Required: true, HelpText: "A substring expected in that URL's response when genuinely connected."},
		},
		ScriptContent: wireguardOrchestratorScript,
	},
}

func findInstallDef(id string) *InstallDef {
	for i := range installCatalog {
		if installCatalog[i].ID == id {
			return &installCatalog[i]
		}
	}
	return nil
}

// wireguardOrchestratorScript is the identical design CloudKeyWizard's own ExtraCatalog.cs uses
// for its "wireguard" Extra (same reasoning documented there: an app-authored assembly of the real
// bundled Phase 4 scripts + the systemd units documented on the upstream wiki, since no single
// upstream file does this assembly). Adjusted only for running locally rather than over SSH: reads
// siblings from the local WORKDIR this binary writes them to (see runInstall in handlers_install.go)
// instead of an SFTP-uploaded directory -- the script content itself is otherwise the same.
const wireguardOrchestratorScript = `#!/bin/bash
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
`
