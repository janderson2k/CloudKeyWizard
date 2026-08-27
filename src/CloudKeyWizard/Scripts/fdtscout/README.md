# FDT.Scout

App-authored, not sourced from jnovack/cloudkey or any upstream project. Source lives at
`tools/fdtscout/` in this repo (a separate Go module -- it targets the Cloud Key's Linux/arm64
userland, not Windows, so it isn't part of the CloudKeyWizard.csproj build itself).

`fdtscout-arm64` here is a pre-built, stripped, statically-linked binary (`CGO_ENABLED=0`,
`GOOS=linux GOARCH=arm64`) checked in as a build artifact and embedded into the Windows app the
same way the jnovack/cloudkey scripts are, so installing it needs no compiler on the device and no
network access at install time beyond the SSH/SFTP connection this app already has open.

**Why arm64 and not the 32-bit `cloudkey-linux-arm` the LCD app ships**: confirmed via the
Phase-1-De-Ubiquitizing wiki page that this hardware's SoC (Qualcomm APQ8053) runs a genuine
64-bit ARM (aarch64) userland -- jnovack/cloudkey's own binary is published as 32-bit for its own
reasons (works fine via the SoC's AArch32 compatibility mode) but that doesn't mean the device
*requires* 32-bit. A native arm64 build is the better fit here, not a compatibility risk.

**To rebuild after changing the source**:
```
cd tools/fdtscout
go mod tidy
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -buildvcs=false -o fdtscout-arm64 -ldflags="-s -w" .
cp fdtscout-arm64 ../../src/CloudKeyWizard/Scripts/fdtscout/fdtscout-arm64
```
Then rebuild/republish CloudKeyWizard as usual -- the new binary is picked up automatically as an
embedded resource.

**What it is**: a small self-contained HTTPS admin console (see `Services/ExtraCatalog.cs`'s
`fdtscout` Extra for the install orchestration and full feature description) -- password-gated
login, a real web terminal (PTY over WebSocket), TLS cert management, hostname/front-panel-text
control, and 7-day health metrics. Installed to `/opt/fdtscout/fdtscout`, config/data under
`/opt/fdtscout/data` and `/etc/fdtscout/tls`, run as a systemd service (`fdtscout.service`, this
folder) as root.

**Not live-tested against real hardware as of the commit that added it** -- built and cross-
compiled cleanly, but no VPN/Cloud-Key session was available to actually browse to it and click
through every feature end-to-end. Treat the first real install as the actual first test.
