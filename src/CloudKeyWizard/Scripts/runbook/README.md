# Bundled runbook scripts

Most `.sh` files here are verbatim copies of
[jnovack/cloudkey](https://github.com/jnovack/cloudkey)'s `scripts/runbook/` directory at commit
[`47fa33bf412deaadec36676b9abbee841bbdfa43`](https://github.com/jnovack/cloudkey/tree/47fa33bf412deaadec36676b9abbee841bbdfa43/scripts/runbook)
(main branch, 2026-07-30). A few are app-authored instead (see below) even though they're embedded
and loaded through the exact same mechanism. All of them are embedded directly into the exe at
build time (`CloudKeyWizard.csproj` → `<EmbeddedResource Include="Scripts\runbook\*.sh" />`) and
read at runtime via `Services/BundledScriptProvider.cs`. Nothing in this app fetches script content
over the network at runtime — the SSH connection to the Cloud Key itself is the only network
activity this app performs.

**License**: jnovack/cloudkey (and therefore the verbatim files here) is licensed under
[Creative Commons Attribution-NonCommercial-ShareAlike 4.0 International (CC BY-NC-SA 4.0)](https://creativecommons.org/licenses/by-nc-sa/4.0/)
— see `LICENSE-jnovack-cloudkey.md` in this directory for the exact license text. CloudKey Wizard
(this app) is given away for free, built and distributed on a personal, non-commercial basis, and
doesn't modify these specific files at all (verbatim + pinned commit, hash-checked at read time) —
consistent with CC BY-NC-SA's own terms rather than a separate legal opinion. The three app-authored
files below are original work by this app's author, not adaptations of jnovack/cloudkey content, and
aren't covered by that license.

**App-authored, not jnovack/cloudkey content** (each `PhaseStep.IsAppAuthored = true` in
`ScriptCatalog.cs`, so the terminal log labels these correctly when they run):
- `phase1-base-tools.sh` — the package list this app installs on top of the stock image.
- `phase1-format-mount-volume.sh` — whole-disk ext4 + a systemd `.mount` unit for `/volume`.
- `phase1-account-cleanup.sh` — removes the UniFi appliance accounts a purge leaves behind; runs
  automatically as part of the purge step now, not as its own separate wizard step.

`security-lock.sh` is included alongside `phase1-security-setup.sh` because the latter execs it as
a final step (`$(dirname "$0")/security-lock.sh`) — `BundledScriptProvider` uploads both to the same
remote directory before running the security step so that sibling lookup resolves.

## Updating

To pull in an upstream change to one of the still-jnovack-sourced files: review the diff at
`https://github.com/jnovack/cloudkey/compare/47fa33bf412deaadec36676b9abbee841bbdfa43...<new-sha>`
under `scripts/runbook/`, replace the affected file(s) here with the new verbatim content, and
update the commit reference in this file and in `BundledScriptProvider.PinnedCommitSha`. The three
app-authored files above aren't tracked against that commit at all -- edit them directly.
