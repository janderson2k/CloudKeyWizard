# Publishes CloudKey Wizard as a single portable, self-contained win-x64 .exe.
# No installer, no registry writes, no admin rights, no separate .NET runtime install required --
# copy the one .exe anywhere (including a USB stick) and run it.
#
# Usage (from repo root or anywhere):
#   pwsh -File scripts/Publish.ps1
#
# WPF apps can't be safely trimmed (heavy XAML reflection), so this lands around 150-200MB rather
# than a tiny trimmed console tool -- still exactly one file.

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$proj = Join-Path $root "src/CloudKeyWizard/CloudKeyWizard.csproj"
$out  = Join-Path $root "publish"

dotnet publish $proj `
    -c Release `
    -r win-x64 `
    --self-contained true `
    -p:PublishSingleFile=true `
    -p:IncludeNativeLibrariesForSelfExtract=true `
    -p:EnableCompressionInSingleFile=true `
    -p:DebugType=None `
    -o $out
# Deliberately NOT setting InvariantGlobalization=true: WPF's data-binding engine (XmlLanguage /
# BindingExpression culture resolution) throws at startup without real ICU culture data -- confirmed
# by hitting exactly that crash during testing. The size savings aren't worth a broken app.

Write-Host ""
Write-Host "Published: $out\CloudKeyWizard.exe" -ForegroundColor Green
