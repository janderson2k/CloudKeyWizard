using System.Collections.ObjectModel;
using System.Text;
using System.Text.RegularExpressions;
using System.Windows;
using CloudKeyWizard.Models;
using CloudKeyWizard.Services;
using Renci.SshNet;

namespace CloudKeyWizard.ViewModels;

/// <summary>
/// The whole wizard's state and behavior. One view model backing the three-pane MainWindow: left
/// nav (Steps), middle working surface (SelectedStep + its inputs), right terminal (TerminalLines).
/// </summary>
public sealed class MainViewModel : ObservableObject, IDisposable
{
    private readonly SshSessionService _ssh = new();
    private readonly BundledScriptProvider _scripts = new();
    private readonly PreflightService _preflight;
    private ConnectionProfile? _lastProfile;
    private ShellStream? _shellStream;
    private CancellationTokenSource? _runCts;

    private static readonly Regex AnsiEscape = new(@"\x1B\[[0-9;?]*[a-zA-Z]", RegexOptions.Compiled);

    public MainViewModel()
    {
        _preflight = new PreflightService(_ssh);
        _ssh.OutputReceived += line => RunOnUi(() => AddTerminalLine(line));

        Steps = new ObservableCollection<PhaseStep>(ScriptCatalog.BuildSteps());
        Steps[0].Status = StepStatus.Current; // "connect"
        for (var i = 1; i < Steps.Count; i++) Steps[i].Status = StepStatus.Locked;

        foreach (var extra in ExtraCatalog.BuildExtras())
        {
            (extra.Category switch
            {
                ExtraCategory.General => GeneralExtras,
                ExtraCategory.MediaServer => MediaExtras,
                ExtraCategory.HomeAutomation => HomeAutomationExtras,
                ExtraCategory.Backup => BackupExtras,
                _ => RemoteExtras,
            }).Add(extra);
        }

        foreach (var host in KnownHostsStore.Load().OrderByDescending(h => h.LastConnected))
            KnownHosts.Add(host);

        // Setter applies the merged theme dictionary too -- App.xaml's static default is Dark, so
        // this only actually does anything at startup when the saved preference is Light.
        IsDarkTheme = AppSettingsStore.Load().DarkTheme;

        ConnectCommand = new AsyncRelayCommand(ConnectAsync, () => !IsBusy && !IsConnected && !string.IsNullOrWhiteSpace(Host));
        RunPreflightCommand = new AsyncRelayCommand(RunPreflightAsync, () => !IsBusy && IsConnected);
        SimulateCommand = new AsyncRelayCommand(() => RunSelectedStepAsync(simulate: true), () => CanSimulate);
        ApplyCommand = new AsyncRelayCommand(() => RunSelectedStepAsync(simulate: false), () => CanApply);
        SkipCommand = new RelayCommand(SkipSelectedStep, () => SelectedStep?.IsOptional == true && !IsBusy);
        RebootCommand = new AsyncRelayCommand(RebootAndWaitAsync, () => !IsBusy && IsConnected && SelectedStep?.Id == "reboot");
        RefreshDevicesCommand = new AsyncRelayCommand(LoadBlockDevicesAsync, () => !IsBusy && IsConnected && SelectedStep?.Id == "format-volume");
        ToggleShellCommand = new RelayCommand(ToggleShell, () => IsConnected);
        SendShellCommand = new RelayCommand(SendShellInput, () => IsShellMode && !string.IsNullOrEmpty(ShellInputText));
        AcceptDisclaimerCommand = new RelayCommand(() => ShowDisclaimer = false);
        SelectStepCommand = new RelayCommand<PhaseStep>(step => { if (step is not null && step.Status != StepStatus.Locked) SelectedStep = step; });
        BackCommand = new RelayCommand(() => MoveSelection(-1), () => !IsBusy && SelectedStep is not null && Steps.IndexOf(SelectedStep) > 0);
        NextCommand = new RelayCommand(() => MoveSelection(+1), () => !IsBusy && SelectedStep is not null && Steps.IndexOf(SelectedStep) < Steps.Count - 1 &&
                                                                        Steps[Steps.IndexOf(SelectedStep) + 1].Status != StepStatus.Locked);
        DisconnectCommand = new RelayCommand(() =>
        {
            _ssh.Disconnect();
            IsConnected = false;
        }, () => IsConnected && !IsBusy);

        CancelCommand = new RelayCommand(() =>
        {
            AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = "--- cancel requested -- waiting for the current step to unwind ---" });
            _runCts?.Cancel();
        }, () => IsBusy);

        RunExtraCommand = new RelayCommand<ExtraItem>(item => { if (item is not null) _ = RunExtraAsync(item); },
            item => item is { IsAvailable: true } && !IsBusy && IsConnected);
        ToggleSecretRevealedCommand = new RelayCommand<ExtraParam>(p => { if (p is not null) p.IsRevealed = !p.IsRevealed; });
        SetPasswordCommand = new AsyncRelayCommand(SetPasswordAsync, () => !IsBusy && IsConnected);
        RelockCommand = new AsyncRelayCommand(RelockAsync, () => !IsBusy && IsConnected && PasswordLoginEnabled);
        CopySummaryCommand = new RelayCommand(() => Clipboard.SetText(SummaryText), () => !string.IsNullOrEmpty(SummaryText));
        ShowDisclaimerCommand = new RelayCommand(() => ShowDisclaimer = true);
        ForgetCurrentHostCommand = new RelayCommand(ForgetCurrentHost, () => !string.IsNullOrWhiteSpace(Host) && KnownHosts.Any(h => h.Host == Host));
        ClearKnownHostsCommand = new RelayCommand(() =>
        {
            KnownHosts.Clear();
            KnownHostsStore.Save(KnownHosts);
        }, () => KnownHosts.Count > 0);

        SelectedStep = Steps[0];
    }

    // ---------------------------------------------------------------- state

    public ObservableCollection<PhaseStep> Steps { get; }
    public ObservableCollection<TerminalLine> TerminalLines { get; } = new();
    public ObservableCollection<PreflightCheckResult> PreflightChecks { get; } = new();
    public ObservableCollection<BlockDeviceInfo> BlockDevices { get; } = new();
    public ObservableCollection<ExtraItem> GeneralExtras { get; } = new();
    public ObservableCollection<ExtraItem> MediaExtras { get; } = new();
    public ObservableCollection<ExtraItem> RemoteExtras { get; } = new();
    public ObservableCollection<ExtraItem> HomeAutomationExtras { get; } = new();
    public ObservableCollection<ExtraItem> BackupExtras { get; } = new();
    public ObservableCollection<KnownHost> KnownHosts { get; } = new();

    private bool _showDisclaimer = true;
    public bool ShowDisclaimer { get => _showDisclaimer; set => SetField(ref _showDisclaimer, value); }

    private string _host = string.Empty;
    public string Host { get => _host; set { if (SetField(ref _host, value)) Requery(); } }

    private string _username = "root";
    public string Username { get => _username; set => SetField(ref _username, value); }

    private string _password = string.Empty;
    public string Password { get => _password; set => SetField(ref _password, value); }

    private bool _useKeyAuth;
    public bool UseKeyAuth { get => _useKeyAuth; set => SetField(ref _useKeyAuth, value); }

    private string _privateKeyPath = string.Empty;
    public string PrivateKeyPath { get => _privateKeyPath; set => SetField(ref _privateKeyPath, value); }

    private KnownHost? _selectedKnownHost;
    /// <summary>Picking an entry here prefills the connect fields -- it never auto-connects and
    /// never touches Password (never stored). Purely a quick-fill convenience.</summary>
    public KnownHost? SelectedKnownHost
    {
        get => _selectedKnownHost;
        set
        {
            if (SetField(ref _selectedKnownHost, value) && value is not null)
            {
                Host = value.Host;
                Username = value.Username;
                UseKeyAuth = value.UseKeyAuth;
                if (value.UseKeyAuth && !string.IsNullOrEmpty(value.PrivateKeyPath))
                    PrivateKeyPath = value.PrivateKeyPath;
            }
        }
    }

    private bool _isConnected;
    public bool IsConnected
    {
        get => _isConnected;
        set { if (SetField(ref _isConnected, value)) { Requery(); OnPropertyChanged(nameof(ConnectionStatusText)); } }
    }

    public string ConnectionStatusText => IsConnected ? $"Connected — {Username}@{Host}" : "Not connected";

    private bool _isBusy;
    public bool IsBusy { get => _isBusy; set { if (SetField(ref _isBusy, value)) Requery(); } }

    private string _busyMessage = string.Empty;
    public string BusyMessage { get => _busyMessage; set => SetField(ref _busyMessage, value); }

    private DeviceModel _detectedModel = DeviceModel.Unknown;
    public DeviceModel DetectedModel { get => _detectedModel; set => SetField(ref _detectedModel, value); }

    private bool _preflightPassed;
    public bool PreflightPassed { get => _preflightPassed; set => SetField(ref _preflightPassed, value); }

    private PhaseStep? _selectedStep;
    public PhaseStep? SelectedStep
    {
        get => _selectedStep;
        set
        {
            if (SetField(ref _selectedStep, value))
            {
                ConfirmationInput = string.Empty;
                OutcomeVerdict = null; // stale GO/NO-GO banner shouldn't follow you to a different step
                OutcomeReason = string.Empty;
                OnPropertyChanged(nameof(IsDangerStep));
                OnPropertyChanged(nameof(RequiredConfirmationText));
                Requery();
                if (value?.Id == "format-volume" && IsConnected && BlockDevices.Count == 0)
                    _ = LoadBlockDevicesAsync();
                if (value?.Id == "purge" && IsConnected)
                    _ = LoadPurgePreviewAsync();
                if (value?.Id == "finish")
                {
                    OnPropertyChanged(nameof(CloudkeyUserExists));
                    SummaryText = BuildSummaryText();
                }
            }
        }
    }

    private string _confirmationInput = string.Empty;
    public string ConfirmationInput { get => _confirmationInput; set { if (SetField(ref _confirmationInput, value)) Requery(); } }

    private BlockDeviceInfo? _selectedDevice;
    public BlockDeviceInfo? SelectedDevice
    {
        get => _selectedDevice;
        set
        {
            if (SetField(ref _selectedDevice, value))
            {
                // RequiredConfirmationText's value depends on SelectedDevice for the format-volume
                // step, but WPF only refreshes a computed property's binding when told to -- without
                // this, the "Type X to enable Apply" hint stayed blank after picking a device even
                // though CanApply itself was evaluating correctly underneath.
                OnPropertyChanged(nameof(RequiredConfirmationText));
                Requery();
            }
        }
    }

    private string _pubKeyInput = string.Empty;
    public string PubKeyInput { get => _pubKeyInput; set => SetField(ref _pubKeyInput, value); }

    private string _lastScriptInfo = string.Empty;
    public string LastScriptInfo { get => _lastScriptInfo; set => SetField(ref _lastScriptInfo, value); }

    private string _purgeLivePreview = string.Empty;
    /// <summary>What Apply would actually remove on THIS device, shown on the confirmation screen
    /// itself rather than requiring a separate manual Simulate click first -- informed consent
    /// based on real data, not generic prose. Loaded automatically when landing on the purge step
    /// (see SelectedStep's setter); read-only, never mutates anything.</summary>
    public string PurgeLivePreview { get => _purgeLivePreview; set => SetField(ref _purgeLivePreview, value); }

    private string _purgeBatchProgress = string.Empty;
    /// <summary>"Batch N of 5" while a real purge Apply is streaming, derived purely by watching
    /// phase1-purge.sh's own existing output markers (`--- purging: ... ---` / `--- skipping
    /// batch...`) as they scroll past in AddTerminalLine -- not a script change, since the purge
    /// script's own logic is deliberately kept untouched (see QUE.MD Step 3). Empty outside an
    /// active purge run.</summary>
    public string PurgeBatchProgress { get => _purgeBatchProgress; set => SetField(ref _purgeBatchProgress, value); }
    private int _purgeBatchesSeen;
    private const int PurgeTotalBatches = 5;

    private bool _passwordLoginEnabled = true;
    /// <summary>Tracks whether the box currently accepts SSH password login -- flipped false when
    /// Harden Access succeeds, true after an emergency unlock, false again after re-lock. Purely a
    /// UI-side belief about device state (not re-queried from the box), so it can drift if password
    /// state is changed some other way -- good enough for gating the Re-lock button sensibly.</summary>
    public bool PasswordLoginEnabled { get => _passwordLoginEnabled; set { if (SetField(ref _passwordLoginEnabled, value)) Requery(); } }

    private string _passwordChangeAccount = "root";
    public string PasswordChangeAccount { get => _passwordChangeAccount; set => SetField(ref _passwordChangeAccount, value); }

    private string _newPasswordValue = string.Empty;
    public string NewPasswordValue { get => _newPasswordValue; set => SetField(ref _newPasswordValue, value); }

    private string _confirmPasswordValue = string.Empty;
    public string ConfirmPasswordValue { get => _confirmPasswordValue; set => SetField(ref _confirmPasswordValue, value); }

    private string _passwordChangeStatus = string.Empty;
    public string PasswordChangeStatus { get => _passwordChangeStatus; set => SetField(ref _passwordChangeStatus, value); }

    private string _summaryText = string.Empty;
    public string SummaryText { get => _summaryText; set { if (SetField(ref _summaryText, value)) CopySummaryCommand?.RaiseCanExecuteChanged(); } }

    public bool CloudkeyUserExists => Steps.First(s => s.Id == "security").Status == StepStatus.Done;

    private bool _isDarkTheme = true;
    public bool IsDarkTheme
    {
        get => _isDarkTheme;
        set
        {
            if (SetField(ref _isDarkTheme, value))
            {
                ApplyTheme(value);
                AppSettingsStore.Save(new AppSettings { DarkTheme = value });
            }
        }
    }

    /// <summary>Swaps the merged theme dictionary at index 0. Every color reference in this app's
    /// XAML uses DynamicResource specifically so this propagates everywhere on its own -- no manual
    /// binding refresh needed, unlike a naive StaticResource setup.</summary>
    private static void ApplyTheme(bool dark)
    {
        var uri = new Uri(dark ? "Themes/DarkPalette.xaml" : "Themes/LightPalette.xaml", UriKind.Relative);
        Application.Current.Resources.MergedDictionaries[0] = new ResourceDictionary { Source = uri };
    }

    private OutcomeVerdict? _outcomeVerdict;
    /// <summary>Null = no run has completed for the current step yet (or it was just switched away
    /// from); set after every Simulate/Apply run by <see cref="OutcomeInterpreter"/>.</summary>
    public OutcomeVerdict? OutcomeVerdict { get => _outcomeVerdict; set => SetField(ref _outcomeVerdict, value); }

    private string _outcomeReason = string.Empty;
    public string OutcomeReason { get => _outcomeReason; set => SetField(ref _outcomeReason, value); }

    private bool _isShellMode;
    public bool IsShellMode { get => _isShellMode; set => SetField(ref _isShellMode, value); }

    private string _shellInputText = string.Empty;
    public string ShellInputText { get => _shellInputText; set { if (SetField(ref _shellInputText, value)) Requery(); } }

    public bool IsDangerStep => SelectedStep?.Severity == StepSeverity.Danger;

    public string RequiredConfirmationText => SelectedStep?.Id switch
    {
        "format-volume" => SelectedDevice?.Path ?? "",
        _ => SelectedStep?.ConfirmationPhrase ?? "",
    };

    private bool CanSimulate => !IsBusy && IsConnected && SelectedStep is { SupportsSimulate: true } &&
                                 StepSpecificReady(requireDevice: SelectedStep.Id == "format-volume");

    private bool CanApply => !IsBusy && IsConnected && SelectedStep is { ScriptFileName: not null } &&
                              StepSpecificReady(requireDevice: SelectedStep.Id == "format-volume") &&
                              (!IsDangerStep || (ConfirmationInput.Length > 0 && ConfirmationInput == RequiredConfirmationText));

    private bool StepSpecificReady(bool requireDevice)
    {
        if (requireDevice && SelectedDevice is null) return false;
        if (SelectedStep?.Id == "security" && string.IsNullOrWhiteSpace(PubKeyInput)) return false;
        return true;
    }

    // -------------------------------------------------------------- commands

    public AsyncRelayCommand ConnectCommand { get; }
    public AsyncRelayCommand RunPreflightCommand { get; }
    public AsyncRelayCommand SimulateCommand { get; }
    public AsyncRelayCommand ApplyCommand { get; }
    public RelayCommand SkipCommand { get; }
    public AsyncRelayCommand RebootCommand { get; }
    public AsyncRelayCommand RefreshDevicesCommand { get; }
    public RelayCommand ToggleShellCommand { get; }
    public RelayCommand SendShellCommand { get; }
    public RelayCommand AcceptDisclaimerCommand { get; }
    public RelayCommand<PhaseStep> SelectStepCommand { get; }
    public RelayCommand BackCommand { get; }
    public RelayCommand NextCommand { get; }
    public RelayCommand DisconnectCommand { get; }
    public RelayCommand CancelCommand { get; }
    public RelayCommand<ExtraItem> RunExtraCommand { get; }
    public RelayCommand<ExtraParam> ToggleSecretRevealedCommand { get; }
    public AsyncRelayCommand SetPasswordCommand { get; }
    public AsyncRelayCommand RelockCommand { get; }
    public RelayCommand CopySummaryCommand { get; }
    public RelayCommand ShowDisclaimerCommand { get; }
    public RelayCommand ForgetCurrentHostCommand { get; }
    public RelayCommand ClearKnownHostsCommand { get; }

    // ---------------------------------------------------------------- flow

    private async Task ConnectAsync()
    {
        IsBusy = true;
        BusyMessage = "Connecting...";
        try
        {
            var profile = new ConnectionProfile
            {
                Host = Host.Trim(),
                Username = Username.Trim(),
                Password = UseKeyAuth ? null : Password,
                PrivateKeyPath = UseKeyAuth ? PrivateKeyPath.Trim() : null,
            };
            await _ssh.ConnectAsync(profile);
            _lastProfile = profile;
            IsConnected = true;
            MarkDone("connect", "Connected.");
            RememberCurrentHost();
            var preflightStep = Steps.First(s => s.Id == "preflight");
            preflightStep.Status = StepStatus.Pending;
            SelectedStep = preflightStep;
            await RunPreflightAsync();
            RememberCurrentHost(); // now that DetectedModel is known
            RestoreExtrasFromKnownHost();
            await RestoreExtrasFromDeviceStateAsync(); // device-authoritative -- overrides the above if present
        }
        catch (Exception ex)
        {
            AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = $"Connect failed: {ex.Message}" });
        }
        finally
        {
            IsBusy = false;
        }
    }

    private async Task RunPreflightAsync()
    {
        IsBusy = true;
        BusyMessage = "Running health checks...";
        try
        {
            var result = await _preflight.RunAsync();
            PreflightChecks.Clear();
            foreach (var c in result.Checks) PreflightChecks.Add(c);
            DetectedModel = result.Model;
            PreflightPassed = result.AllBlockingPassed;

            // Only resume-detect and unlock downstream steps once the required checks actually
            // pass -- a failed blocking check must keep the rest of the wizard locked, not just
            // display a warning while quietly leaving Apply reachable.
            if (PreflightPassed)
            {
                await _preflight.DetectStepStatusAsync(Steps, DetectedModel);
                UnlockRemainingSteps();
            }

            var preflightStep = Steps.First(s => s.Id == "preflight");
            preflightStep.Status = PreflightPassed ? StepStatus.Done : StepStatus.Failed;
            preflightStep.Detail = PreflightPassed ? "All required checks passed." : "One or more required checks failed -- see list.";
        }
        finally
        {
            IsBusy = false;
        }
    }

    private void UnlockRemainingSteps()
    {
        if (!PreflightPassed) return;
        foreach (var step in Steps)
        {
            if (step.Status == StepStatus.Locked) step.Status = StepStatus.Pending;
        }
    }

    private const int MaxAutoRetriesAfterReconnect = 2;

    // Separate, smaller cap from the reconnect-retry one above -- this covers a specific detected
    // condition (OutcomeInterpreter.StepOutcome.SafeAutoRetry), not a general transport failure, and
    // a single retry is expected to either fully resolve it or reveal a genuinely different problem
    // worth surfacing to the operator rather than looping on.
    private const int MaxSafeAutoRetries = 1;

    private async Task RunSelectedStepAsync(bool simulate, int retryAttempt = 0, int safeAutoRetryAttempt = 0)
    {
        var step = SelectedStep;
        if (step?.ScriptFileName is null) return;

        IsBusy = true;
        BusyMessage = simulate ? $"Simulating: {step.Title}" : $"Applying: {step.Title}";
        OutcomeVerdict = null;
        OutcomeReason = string.Empty;
        _runCts = new CancellationTokenSource();
        try
        {
            var loaded = await _scripts.GetScriptAsync(step.ScriptFileName, _runCts.Token);
            LastScriptInfo = step.IsAppAuthored
                ? $"{step.ScriptFileName}  sha256:{loaded.Sha256Hex[..16]}...  (app-authored, not from jnovack/cloudkey)"
                : $"{step.ScriptFileName}  sha256:{loaded.Sha256Hex[..16]}...  (bundled in this build -- no network fetch)  upstream source: {BundledScriptProvider.SourceUrlFor(step.ScriptFileName)}";
            AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = $"--- {(simulate ? "SIMULATE" : "APPLY")} {step.Title} ({step.ScriptFileName}) ---" });

            var args = BuildArgs(step, simulate);
            if (args is null)
            {
                AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = "Missing required input for this step (device selection or public key) -- aborted before running anything." });
                return;
            }

            // phase1-security-setup.sh execs a sibling security-lock.sh via $(dirname "$0") as its
            // final step -- place that alongside it in the remote work dir first so the lookup
            // resolves, without needing to special-case the whole run flow for one step.
            if (step.Id == "security")
            {
                var lockScript = await _scripts.GetScriptAsync("security-lock.sh", _runCts.Token);
                await _ssh.UploadSupportFileAsync("security-lock.sh", lockScript.Content, _runCts.Token);
            }

            // Reset the batch progress indicator for this run. Simulate never reaches Step 2's
            // batches (it exits at the confirmation prompt with stdin closed), so this only ever
            // advances during a real Apply -- staying blank through Simulate is correct, not a bug.
            if (step.Id == "purge")
            {
                _purgeBatchesSeen = 0;
                PurgeBatchProgress = string.Empty;
            }

            // Pre-purge forensic snapshot -- read-only, doesn't touch the purge script or logic at
            // all. If something goes wrong mid-purge, this is an actual record of exactly what
            // packages were installed beforehand to compare against, not just the terminal log.
            if (step.Id == "purge" && !simulate)
            {
                AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = "--- capturing pre-purge package snapshot (forensic record, read-only) ---" });
                var snapshotPath = $"{SshSessionService.RemoteWorkDir}/pre-purge-snapshot-{DateTime.Now:yyyyMMdd-HHmmss}.txt";
                await _ssh.RunSimpleAsync($"mkdir -p {SshSessionService.RemoteWorkDir} && dpkg --get-selections > {snapshotPath} && echo saved: {snapshotPath}", _runCts.Token);
            }

            var (exitCode, output) = await _ssh.RunScriptStreamingAsync(step.Id, loaded.Content, args, _runCts.Token);

            var outcome = OutcomeInterpreter.Interpret(step.Id, simulate, exitCode, output);
            OutcomeVerdict = outcome.Verdict;
            OutcomeReason = outcome.Reason;

            // Account cleanup used to be its own separate wizard step; folded in here as an
            // automatic follow-on since orphaned accounts only exist because of what purge itself
            // just did -- it's really the second half of "remove the UniFi app layer," not an
            // independent action needing its own confirmation. Runs in the same mode (dry-run
            // alongside Simulate, real alongside a successful Apply) so Simulate shows the complete
            // picture and a failed Apply doesn't touch accounts at all.
            if (step.Id == "purge" && (simulate || exitCode == 0))
            {
                var cleanup = await _scripts.GetScriptAsync("phase1-account-cleanup.sh", _runCts.Token);
                AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = "--- removing orphaned UniFi accounts left by the purge (app-authored, automatic follow-on) ---" });
                await _ssh.RunScriptStreamingAsync("account-cleanup", cleanup.Content, simulate ? "--dry-run" : "-y", _runCts.Token);
            }

            if (simulate)
            {
                AddTerminalLine(new TerminalLine
                {
                    Kind = TerminalLineKind.AppInfo,
                    Text = $"--- simulate run finished, exit code {exitCode}. Nothing on the device was changed by a simulate run for this step. ---",
                });
            }
            else if (exitCode == 0)
            {
                step.Status = StepStatus.Done;
                step.Detail = "Applied successfully.";
                if (step.Id == "security")
                {
                    PasswordLoginEnabled = false; // this step's whole job is disabling it
                    OnPropertyChanged(nameof(CloudkeyUserExists));
                }
                await _preflight.DetectStepStatusAsync(Steps, DetectedModel);
                AdvanceToNextPendingStep();
            }
            else if (outcome.SafeAutoRetry && safeAutoRetryAttempt < MaxSafeAutoRetries)
            {
                // A recognized, known-safe-to-retry condition (see OutcomeInterpreter) -- e.g. purge's
                // batch-vs-dependency-cascade race, where the script itself recomputes what's actually
                // left to do on every invocation, including re-running Step 0's simulate/exact-match
                // safety gate fresh. Retry immediately rather than leaving the step Failed and waiting
                // for the operator to notice the explanation and click Apply again by hand.
                AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = $"--- {outcome.Reason} Auto-retrying '{step.Title}' (attempt {safeAutoRetryAttempt + 1}/{MaxSafeAutoRetries})... ---" });
                await RunSelectedStepAsync(simulate, retryAttempt, safeAutoRetryAttempt + 1);
                return; // the recursive call owns its own finally/IsBusy handling
            }
            else
            {
                step.Status = StepStatus.Failed;
                step.Detail = $"Exited with code {exitCode} -- see terminal log.";
                AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = $"--- step failed, exit code {exitCode} ---" });
            }
        }
        catch (OperationCanceledException)
        {
            // Not a failure -- the operator hit Cancel. Leave the step re-runnable rather than
            // marking it Failed, since nothing here implies the device itself is in a bad state.
            step.Status = StepStatus.Pending;
            step.Detail = "Cancelled by user.";
            AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = "--- cancelled ---" });
        }
        catch (Exception ex) when (!_ssh.IsConnected && retryAttempt < MaxAutoRetriesAfterReconnect)
        {
            // Transport-level drop (confirmed by the underlying SSH client itself reporting
            // disconnected), not a script failure -- don't just leave the UI stuck on a stale
            // "Connected" indicator with a confusing "not connected" error on the next click.
            IsConnected = false;
            AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = $"--- connection lost ({ex.Message}) -- attempting to reconnect ---" });

            var reconnected = await TryReconnectLoopAsync(_runCts?.Token ?? default);
            if (reconnected)
            {
                IsConnected = true;
                AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = $"--- reconnected -- retrying '{step.Title}' (attempt {retryAttempt + 1}/{MaxAutoRetriesAfterReconnect}) ---" });
                await RunSelectedStepAsync(simulate, retryAttempt + 1);
                return; // the recursive call owns its own finally/IsBusy handling
            }

            step.Status = StepStatus.Failed;
            step.Detail = "Connection lost and could not reconnect -- check the device and network, then reconnect manually.";
            AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = "--- gave up reconnecting -- reconnect manually from the Connect page when ready ---" });
        }
        catch (Exception ex)
        {
            step.Status = StepStatus.Failed;
            step.Detail = ex.Message;
            AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = $"Error: {ex.Message}" });
        }
        finally
        {
            IsBusy = false;
            ConfirmationInput = string.Empty;
        }
    }

    /// <summary>Builds the shell args for a script run. Returns null if a required input (device
    /// selection, public key) is missing -- callers must treat null as "cannot run", not "run with
    /// no args".</summary>
    private string? BuildArgs(PhaseStep step, bool simulate) => step.Id switch
    {
        "purge" => simulate ? "" : "-y",
        "format-volume" => SelectedDevice is null ? null : simulate ? SelectedDevice.Path : $"{SelectedDevice.Path} -y",
        "security" => string.IsNullOrWhiteSpace(PubKeyInput) ? null : ShellQuote(PubKeyInput.Trim()),
        _ => "",
    };

    /// <summary>Single-quotes a value for safe interpolation into the one-line `bash script.sh <args>`
    /// command string (an SSH public key line routinely contains spaces). Not a general-purpose shell
    /// escaper -- sufficient for the values this app ever passes as script args.</summary>
    private static string ShellQuote(string value) => "'" + value.Replace("'", "'\\''") + "'";

    private void SkipSelectedStep()
    {
        if (SelectedStep is not { IsOptional: true } step) return;
        step.Status = StepStatus.Skipped;
        step.Detail = "Skipped by user.";
        AdvanceToNextPendingStep();
    }

    private void AdvanceToNextPendingStep()
    {
        var idx = Steps.IndexOf(SelectedStep!);
        for (var i = idx + 1; i < Steps.Count; i++)
        {
            if (Steps[i].Status is StepStatus.Pending or StepStatus.Failed)
            {
                SelectedStep = Steps[i];
                return;
            }
        }
        if (idx + 1 < Steps.Count) SelectedStep = Steps[idx + 1];
    }

    /// <summary>Polls to reconnect using the last successful connection profile, roughly every 10
    /// seconds, for a couple of minutes -- what a dropped connection mid-step should do instead of
    /// just failing once and leaving the UI on a stale "Connected" indicator.</summary>
    private async Task<bool> TryReconnectLoopAsync(CancellationToken ct)
    {
        if (_lastProfile is null) return false;

        const int maxAttempts = 12; // ~2 minutes at 10s apart
        for (var attempt = 1; attempt <= maxAttempts; attempt++)
        {
            ct.ThrowIfCancellationRequested();
            AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = $"--- reconnect attempt {attempt}/{maxAttempts} ---" });
            try
            {
                _ssh.Disconnect(); // clean up the dead client/sftp handles before making new ones
                await _ssh.ConnectAsync(_lastProfile, ct);
                return true;
            }
            catch
            {
                // expected while the box/network is still down -- keep trying
            }
            await Task.Delay(TimeSpan.FromSeconds(10), ct);
        }
        return false;
    }

    private async Task RebootAndWaitAsync()
    {
        var step = Steps.First(s => s.Id == "reboot");
        var host = _lastProfile?.Host;
        if (host is null) return;

        IsBusy = true;
        BusyMessage = "Rebooting...";
        try
        {
            AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = "--- Reboot ---" });
            try
            {
                // Backgrounded + detached so the reboot doesn't try to kill the very channel reporting it,
                // which tends to look like a failure even when the reboot itself is proceeding fine.
                await _ssh.RunSimpleAsync("nohup sh -c 'sleep 1; reboot' > /dev/null 2>&1 & disown");
            }
            catch
            {
                // A dropped connection here is the expected/normal outcome of a reboot -- ignore.
            }

            _ssh.Disconnect();
            IsConnected = false;

            // Two-phase wait, deliberately not just "poll for it to come back": the backgrounded
            // `sleep 1; reboot` hasn't actually started shutting anything down yet at this exact
            // moment, so sshd is often still fully up for a beat after we disconnect. Confirm the
            // port actually goes DOWN first -- otherwise the very first "is it back" probe can just
            // reconnect to the still-running, not-yet-rebooted box and report a false "it's back"
            // almost instantly, which is exactly what was observed live: the step appeared to skip
            // the wait entirely.
            AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = "Waiting for the box to actually go down (up to 90 seconds)..." });
            var wentDown = await SshSessionService.WaitForPortDownAsync(host, _lastProfile!.Port, TimeSpan.FromSeconds(90));
            if (!wentDown)
            {
                step.Status = StepStatus.Failed;
                step.Detail = "The box's SSH port never went down -- the reboot command may not have actually run. Check the box directly before continuing.";
                AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = "--- SSH port never went down -- reboot may not have taken effect. ---" });
                return;
            }

            AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = "Box went down. Waiting for SSH to come back (up to 5 minutes)..." });
            var back = await SshSessionService.WaitForPortAsync(host, _lastProfile!.Port, TimeSpan.FromMinutes(5));
            if (!back)
            {
                step.Status = StepStatus.Failed;
                step.Detail = "Timed out waiting for SSH to come back -- check the box manually.";
                AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = "--- Timed out waiting for reboot. ---" });
                return;
            }

            AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = "Port 22 is back. Reconnecting..." });
            await _ssh.ConnectAsync(_lastProfile);
            IsConnected = true;
            step.Status = StepStatus.Done;
            step.Detail = "Rebooted and reconnected.";

            var verifyStep = Steps.First(s => s.Id == "verify");
            SelectedStep = verifyStep;
            await RunSelectedStepAsync(simulate: false);
        }
        finally
        {
            IsBusy = false;
        }
    }

    private async Task LoadBlockDevicesAsync()
    {
        IsBusy = true;
        BusyMessage = "Reading block devices...";
        try
        {
            var devices = await BlockDeviceService.GetDisksAsync(_ssh);
            BlockDevices.Clear();
            foreach (var d in devices) BlockDevices.Add(d);

            // Auto-select ONLY when exactly one device looks safe -- this saves a click in the
            // common case (one obvious data drive) without ever silently picking between multiple
            // candidates or overriding an operator's existing choice. Still just a dropdown
            // selection, not a confirmation: Apply still requires typing the device path.
            var recommended = devices.Where(d => d.Risk == DeviceRisk.Recommended).ToList();
            if (recommended.Count == 1 && SelectedDevice is null)
            {
                SelectedDevice = recommended[0];
                AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = $"--- suggested drive: {recommended[0].Path} ({recommended[0].RiskReason}) -- verify before typing the confirmation ---" });
            }
            else if (recommended.Count == 0)
            {
                AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = "--- no drive looked like an obvious safe candidate -- review the list carefully, none pre-selected ---" });
            }
            else if (recommended.Count > 1)
            {
                AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = $"--- {recommended.Count} drives look like plausible candidates -- review carefully before selecting, none pre-selected ---" });
            }
        }
        catch (Exception ex)
        {
            AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = $"Could not list block devices: {ex.Message}" });
        }
        finally
        {
            IsBusy = false;
        }
    }

    // The exact PACKAGES list phase1-purge.sh itself targets (see that script's own PACKAGES
    // variable) -- kept in sync by hand since this is a read-only preview query, not something
    // that drives the actual purge logic. If the bundled script's list ever changes, update here
    // too so the preview stays accurate.
    private static readonly string[] PurgeTargetPackages =
    {
        "unifi-assets-uckp", "unifi-assets-uckg2", "unifi-email-templates-all", "python3-unifi-console-protos",
        "mongodb-server", "mongodb-clients", "mongodb-server-core",
        "unifi", "unifi-core",
        "unifi-directory", "unifi-identity-update", "uid-agent", "ucs-agent", "uos-agent", "uos-discovery-client", "uos", "ulp-go",
        "ustd", "ubnt-systemhub", "ubnt-unifi-setup", "ucore-setup-listener",
    };

    /// <summary>Populates PurgeLivePreview with the actual packages on THIS device that Apply would
    /// remove -- read-only (`dpkg -l`, nothing else), doesn't touch the purge script or its logic.
    /// Fired automatically when landing on the purge step (see SelectedStep's setter).</summary>
    private async Task LoadPurgePreviewAsync()
    {
        PurgeLivePreview = "Loading live preview...";
        try
        {
            var (_, stdOut, _) = await _ssh.RunSimpleAsync("dpkg -l", default, "(live preview: checking which target packages are currently installed)");
            var installedNames = stdOut
                .Split('\n')
                .Where(line => line.StartsWith("ii ") || line.StartsWith("rc "))
                .Select(line => line.Split((char[]?)null, StringSplitOptions.RemoveEmptyEntries).Skip(1).FirstOrDefault())
                .Where(name => name is not null)
                .ToHashSet();

            var installed = PurgeTargetPackages.Where(p => installedNames.Contains(p)).ToList();
            PurgeLivePreview = installed.Count == 0
                ? "None of the target packages are currently installed on this device -- Apply would have nothing to remove."
                : $"{installed.Count} package(s) currently installed on THIS device that Apply would remove:\n" + string.Join("\n", installed.Select(p => "  • " + p));
        }
        catch (Exception ex)
        {
            PurgeLivePreview = $"(couldn't load live preview: {ex.Message})";
        }
    }

    private async Task RunExtraAsync(ExtraItem item)
    {
        if (!item.IsAvailable) return;
        if (item.ScriptContent is null && item.BundledScriptFileName is null) return;

        var missing = item.Params.Where(p => p.IsRequired && string.IsNullOrWhiteSpace(p.Value)).ToList();
        if (missing.Count > 0)
        {
            item.Detail = $"Enter a value for '{missing[0].Label}' first.";
            return;
        }

        IsBusy = true;
        BusyMessage = $"Running: {item.Title}";
        item.Status = StepStatus.Current;
        _runCts = new CancellationTokenSource();
        try
        {
            if (item.PrerequisiteBundledScriptFileName is not null)
            {
                var prereq = await _scripts.GetScriptAsync(item.PrerequisiteBundledScriptFileName, _runCts.Token);
                AddTerminalLine(new TerminalLine
                {
                    Kind = TerminalLineKind.AppInfo,
                    Text = $"--- prerequisite: {item.PrerequisiteBundledScriptFileName} (bundled from jnovack/cloudkey, sha256:{prereq.Sha256Hex[..16]}...) ---",
                });
                var (prereqExit, _) = await _ssh.RunScriptStreamingAsync($"extra-{item.Id}-prereq", prereq.Content, "", _runCts.Token);
                if (prereqExit != 0)
                {
                    item.Status = StepStatus.Failed;
                    item.Detail = $"Prerequisite {item.PrerequisiteBundledScriptFileName} failed (exit {prereqExit}) -- stopped before running the main script.";
                    return;
                }
            }

            foreach (var siblingName in item.SiblingBundledScriptFileNames)
            {
                var sibling = await _scripts.GetScriptAsync(siblingName, _runCts.Token);
                await _ssh.UploadSupportFileAsync(siblingName, sibling.Content, _runCts.Token);
            }

            foreach (var upload in item.BinaryUploads)
            {
                var binary = await _scripts.GetBinaryAsync(upload.ResourceFileName, _runCts.Token);
                var remoteDir = upload.RemotePath[..upload.RemotePath.LastIndexOf('/')];
                await _ssh.RunSimpleAsync($"mkdir -p {remoteDir}", _runCts.Token);
                await _ssh.UploadBinaryFileAsync(upload.RemotePath, binary.Content, upload.ChmodMode, _runCts.Token);
                AddTerminalLine(new TerminalLine
                {
                    Kind = TerminalLineKind.AppInfo,
                    Text = $"--- uploaded {upload.ResourceFileName} -> {upload.RemotePath} (sha256:{binary.Sha256Hex[..16]}...) ---",
                });
            }

            string scriptContent;
            if (item.BundledScriptFileName is not null)
            {
                var loaded = await _scripts.GetScriptAsync(item.BundledScriptFileName, _runCts.Token);
                scriptContent = loaded.Content;
                AddTerminalLine(new TerminalLine
                {
                    Kind = TerminalLineKind.AppInfo,
                    Text = $"--- {item.Title} ({item.BundledScriptFileName}, bundled from jnovack/cloudkey, sha256:{loaded.Sha256Hex[..16]}...) ---",
                });
            }
            else
            {
                scriptContent = item.ScriptContent!;
                AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = $"--- {item.Title} (app-authored, not from jnovack/cloudkey) ---" });
            }

            var envPrefix = string.Join(" ", item.Params.Select(p => $"{p.EnvVarName}={ShellQuote(p.Value.Trim())}"));
            // Real, live-confirmed bug: the terminal echo previously showed this exact envPrefix
            // verbatim, meaning a secret Param's actual plaintext value (e.g. FDT.Scout's
            // ADMIN_PASSWORD) landed straight in the terminal pane -- caught from a real user's own
            // pasted log after the masked-password propagation bug was finally fixed and a real
            // password made it all the way through for the first time. Build a redacted display
            // version alongside the real one; the real values still go over SSH exactly as before,
            // only what's ECHOED into the terminal changes.
            var envPrefixDisplay = string.Join(" ", item.Params.Select(p => $"{p.EnvVarName}={(p.IsSecret ? "'***'" : ShellQuote(p.Value.Trim()))}"));
            var (exitCode, _) = await _ssh.RunScriptStreamingAsync($"extra-{item.Id}", scriptContent, "", _runCts.Token, envPrefix, envPrefixDisplay);

            if (exitCode == 0)
            {
                item.Status = StepStatus.Done;
                item.Detail = "Done.";
            }
            else
            {
                item.Status = StepStatus.Failed;
                item.Detail = $"Exited with code {exitCode} -- see terminal log.";
            }
        }
        catch (OperationCanceledException)
        {
            item.Status = StepStatus.Pending;
            item.Detail = "Cancelled.";
        }
        catch (Exception ex)
        {
            item.Status = StepStatus.Failed;
            item.Detail = ex.Message;
            AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = $"Error: {ex.Message}" });
        }
        finally
        {
            PersistExtraStatus(item); // unlike Phase 1 steps, Extras have no live re-detection on reconnect
            IsBusy = false;
        }
    }

    private async Task SetPasswordAsync()
    {
        if (string.IsNullOrEmpty(NewPasswordValue))
        {
            PasswordChangeStatus = "Enter a new password.";
            return;
        }
        if (NewPasswordValue != ConfirmPasswordValue)
        {
            PasswordChangeStatus = "Passwords don't match.";
            return;
        }

        IsBusy = true;
        BusyMessage = "Setting password...";
        try
        {
            if (!PasswordLoginEnabled)
            {
                AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = "--- password login is currently disabled -- temporarily re-enabling to set a new password ---" });
                var unlockScript = await _scripts.GetScriptAsync("security-unlock.sh");
                await _ssh.RunScriptStreamingAsync("security-unlock", unlockScript.Content, "");
                PasswordLoginEnabled = true;
            }

            var account = PasswordChangeAccount;
            var payload = $"{account}:{NewPasswordValue}";
            var command = $"echo {ShellQuote(payload)} | chpasswd";
            var (exitCode, _, stdErr) = await _ssh.RunSimpleAsync(command, default, displayOverride: $"echo '{account}:••••••••' | chpasswd");

            PasswordChangeStatus = exitCode == 0
                ? $"Password set for '{account}'. Password login is currently ENABLED on the box -- test it, then click Re-lock when done."
                : $"chpasswd failed (exit {exitCode}): {stdErr.Trim()}";
        }
        catch (Exception ex)
        {
            PasswordChangeStatus = $"Error: {ex.Message}";
        }
        finally
        {
            NewPasswordValue = string.Empty;
            ConfirmPasswordValue = string.Empty;
            IsBusy = false;
        }
    }

    private async Task RelockAsync()
    {
        IsBusy = true;
        BusyMessage = "Re-locking...";
        try
        {
            var lockScript = await _scripts.GetScriptAsync("security-lock.sh");
            await _ssh.RunScriptStreamingAsync("security-lock", lockScript.Content, "");
            PasswordLoginEnabled = false;
            PasswordChangeStatus = "Re-locked -- password login is disabled again. Key-based login still works.";
        }
        catch (Exception ex)
        {
            PasswordChangeStatus = $"Error re-locking: {ex.Message}";
        }
        finally
        {
            IsBusy = false;
        }
    }

    private string BuildSummaryText()
    {
        var sb = new StringBuilder();
        sb.AppendLine("CloudKey Wizard -- Summary");
        sb.AppendLine($"Generated: {DateTime.Now:u}");
        sb.AppendLine();
        sb.AppendLine($"Device:    {Host}");
        sb.AppendLine($"Model:     {DetectedModel}");
        sb.AppendLine($"Connected as: {Username} ({(UseKeyAuth ? "private key" : "password")})");
        sb.AppendLine();
        sb.AppendLine("Access after this session:");
        if (CloudkeyUserExists)
        {
            sb.AppendLine("  - 'cloudkey' admin user created, passwordless sudo, key-only SSH");
            sb.AppendLine("  - root/ubnt/cloudkey passwords are LOCKED (no password login)");
            sb.AppendLine("  - Reconnect using the private key matching what you supplied on the Harden Access step");
        }
        else
        {
            sb.AppendLine("  - Harden Access was skipped or not yet run -- original credentials still work");
        }
        sb.AppendLine();
        sb.AppendLine("Steps:");
        foreach (var s in Steps.Where(s => s.Id is not ("connect" or "preflight" or "extras" or "finish")))
            sb.AppendLine($"  [{s.Status,-7}] {s.Title}{(string.IsNullOrWhiteSpace(s.Detail) ? "" : $" -- {s.Detail}")}");

        return sb.ToString();
    }

    private void ToggleShell()
    {
        if (IsShellMode)
        {
            _shellStream?.Dispose();
            _shellStream = null;
            IsShellMode = false;
            AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = "--- left interactive shell; re-checking device state ---" });
            if (IsConnected) _ = RunPreflightAsync();
        }
        else
        {
            _shellStream = _ssh.OpenInteractiveShell();
            _shellStream.DataReceived += (_, e) => RunOnUi(() =>
            {
                var text = AnsiEscape.Replace(Encoding.UTF8.GetString(e.Data), "").Replace("\r", "");
                if (text.Length > 0) AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.StdOut, Text = text });
            });
            IsShellMode = true;
            AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = "--- interactive shell open -- type commands below; this is a real shell on the device ---" });
        }
    }

    private void SendShellInput()
    {
        if (_shellStream is null || string.IsNullOrEmpty(ShellInputText)) return;
        AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.CommandEcho, Text = ShellInputText });
        _shellStream.WriteLine(ShellInputText);
        ShellInputText = string.Empty;
    }

    /// <summary>Writes a single raw control byte straight to the interactive shell (Ctrl+C/D/Z --
    /// see MainWindow's ShellInputBox_PreviewKeyDown). A whole-line WriteLine can't express these;
    /// the remote process is genuinely still running and waiting on stdin, same as a real terminal.</summary>
    public void SendRawShellByte(byte value, string label)
    {
        if (_shellStream is null) return;
        _shellStream.Write(new[] { value }, 0, 1);
        _shellStream.Flush();
        AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.CommandEcho, Text = label });
    }

    // -------------------------------------------------------- known hosts

    private void RememberCurrentHost()
    {
        if (_lastProfile is null) return;

        var existing = KnownHosts.FirstOrDefault(h => h.Host == _lastProfile.Host);
        if (existing is null)
        {
            existing = new KnownHost { Host = _lastProfile.Host };
            KnownHosts.Insert(0, existing);
        }
        existing.Username = _lastProfile.Username;
        existing.UseKeyAuth = UseKeyAuth;
        existing.PrivateKeyPath = UseKeyAuth ? PrivateKeyPath : null; // never Password
        existing.LastConnected = DateTime.Now;
        existing.DetectedModel = DetectedModel.ToString();

        KnownHostsStore.Save(KnownHosts);
        ForgetCurrentHostCommand.RaiseCanExecuteChanged();
        ClearKnownHostsCommand.RaiseCanExecuteChanged();
    }

    /// <summary>Extras have no live re-detection from the device the way Phase 1 steps do (see
    /// PreflightService.DetectStepStatusAsync) -- this is the one thing a known-host entry actually
    /// needs to restore rather than just being a connection-details shortcut.</summary>
    private void RestoreExtrasFromKnownHost()
    {
        var existing = KnownHosts.FirstOrDefault(h => h.Host == Host);
        if (existing is null) return;

        foreach (var extra in GeneralExtras.Concat(MediaExtras).Concat(RemoteExtras).Concat(HomeAutomationExtras).Concat(BackupExtras))
        {
            if (existing.ExtraStatuses.TryGetValue(extra.Id, out var statusText) &&
                Enum.TryParse<StepStatus>(statusText, out var status))
            {
                extra.Status = status;
                if (existing.ExtraDetails.TryGetValue(extra.Id, out var detail)) extra.Detail = detail;
            }
        }
    }

    /// <summary>Device-authoritative continuity: reads Scripts/fdtscout-state.json off the device
    /// itself (written by either this app or FDT.Scout's own Apps tab) and, if present, overrides
    /// whatever the local KnownHosts entry remembers -- so a *different* copy of this app connecting
    /// to an already-converted Cloud Key picks up real state, not a blank slate. Silent no-op if the
    /// file doesn't exist yet (first time either app has touched this device) or fails to read --
    /// this is a nice-to-have continuity aid, not something that should block Connect on failure.</summary>
    private async Task RestoreExtrasFromDeviceStateAsync()
    {
        DeviceStateStore.DeviceState? state;
        try { state = await DeviceStateStore.ReadAsync(_ssh); }
        catch { return; }
        if (state is null) return;

        foreach (var extra in GeneralExtras.Concat(MediaExtras).Concat(RemoteExtras).Concat(HomeAutomationExtras).Concat(BackupExtras))
        {
            if (state.ExtraStatuses.TryGetValue(extra.Id, out var statusText) &&
                Enum.TryParse<StepStatus>(statusText, out var status))
            {
                extra.Status = status;
                if (state.ExtraDetails.TryGetValue(extra.Id, out var detail)) extra.Detail = detail;
            }
        }
        AddTerminalLine(new TerminalLine { Kind = TerminalLineKind.AppInfo, Text = $"Loaded prior state from the device itself (last updated by {state.LastUpdatedBy})." });
        PersistExtraStatus(null); // re-sync local KnownHosts from the now-authoritative in-memory state
    }

    private void PersistExtraStatus(ExtraItem? item)
    {
        var existing = KnownHosts.FirstOrDefault(h => h.Host == Host);
        if (existing is null) return; // shouldn't happen -- RememberCurrentHost runs on every connect

        if (item is not null)
        {
            existing.ExtraStatuses[item.Id] = item.Status.ToString();
            existing.ExtraDetails[item.Id] = item.Detail;
        }
        else
        {
            // Called from RestoreExtrasFromDeviceStateAsync with no single item -- re-sync every
            // Extra's current in-memory status into the local KnownHosts entry wholesale.
            foreach (var extra in GeneralExtras.Concat(MediaExtras).Concat(RemoteExtras).Concat(HomeAutomationExtras).Concat(BackupExtras))
            {
                existing.ExtraStatuses[extra.Id] = extra.Status.ToString();
                existing.ExtraDetails[extra.Id] = extra.Detail;
            }
        }
        KnownHostsStore.Save(KnownHosts);

        if (item is not null) _ = WriteDeviceStateAsync();
    }

    /// <summary>Mirrors the current Extras status onto the device itself (see DeviceStateStore) so
    /// FDT.Scout's Apps tab and any other copy of this app connecting later both see the same
    /// truth. Best-effort -- a failed write here shouldn't surface as an error to the user; the
    /// local KnownHosts copy (already saved by the caller) remains the fallback.</summary>
    private async Task WriteDeviceStateAsync()
    {
        if (!_ssh.IsConnected) return;
        try
        {
            var statuses = new Dictionary<string, string>();
            var details = new Dictionary<string, string>();
            foreach (var extra in GeneralExtras.Concat(MediaExtras).Concat(RemoteExtras).Concat(HomeAutomationExtras).Concat(BackupExtras))
            {
                statuses[extra.Id] = extra.Status.ToString();
                details[extra.Id] = extra.Detail;
            }
            await DeviceStateStore.WriteAsync(_ssh, statuses, details, DetectedModel.ToString(), $"CloudKeyWizard {AppVersion.Version}");
        }
        catch { /* best effort -- local KnownHosts copy already saved */ }
    }

    public void ExportKnownHostsTo(string path) => KnownHostsStore.SaveTo(path, KnownHosts);

    /// <summary>Merges rather than replaces -- importing a hosts file from another machine should
    /// add what you don't already have, not silently wipe your current list. On a host that exists
    /// in both, the more recently-connected entry wins.</summary>
    public void ImportKnownHostsFrom(string path)
    {
        var imported = KnownHostsStore.LoadFrom(path);
        foreach (var incoming in imported)
        {
            var existing = KnownHosts.FirstOrDefault(h => h.Host == incoming.Host);
            if (existing is null)
            {
                KnownHosts.Add(incoming);
            }
            else if (incoming.LastConnected > existing.LastConnected)
            {
                KnownHosts[KnownHosts.IndexOf(existing)] = incoming;
            }
        }
        KnownHostsStore.Save(KnownHosts);
        ForgetCurrentHostCommand.RaiseCanExecuteChanged();
        ClearKnownHostsCommand.RaiseCanExecuteChanged();
    }

    private void ForgetCurrentHost()
    {
        var existing = KnownHosts.FirstOrDefault(h => h.Host == Host);
        if (existing is null) return;
        KnownHosts.Remove(existing);
        KnownHostsStore.Save(KnownHosts);
    }

    // ------------------------------------------------------------- helpers

    private void MarkDone(string id, string detail)
    {
        var step = Steps.FirstOrDefault(s => s.Id == id);
        if (step is null) return;
        step.Status = StepStatus.Done;
        step.Detail = detail;
    }

    private void AddTerminalLine(TerminalLine line)
    {
        TerminalLines.Add(line);
        while (TerminalLines.Count > 4000) TerminalLines.RemoveAt(0);

        // Batch progress indicator: purely reactive to phase1-purge.sh's own existing markers, one
        // per run_batch() call -- see PurgeBatchProgress's doc comment for why this doesn't touch
        // the script itself.
        if (line.Text.StartsWith("--- purging: ", StringComparison.Ordinal) ||
            line.Text.StartsWith("--- skipping batch; none of these packages remain:", StringComparison.Ordinal))
        {
            _purgeBatchesSeen = Math.Min(_purgeBatchesSeen + 1, PurgeTotalBatches);
            PurgeBatchProgress = $"Batch {_purgeBatchesSeen} of {PurgeTotalBatches}";
        }
    }

    private static void RunOnUi(Action action)
    {
        if (Application.Current?.Dispatcher.CheckAccess() == true) action();
        else Application.Current?.Dispatcher.Invoke(action);
    }

    private void Requery()
    {
        ConnectCommand?.RaiseCanExecuteChanged();
        SkipCommand?.RaiseCanExecuteChanged();
        DisconnectCommand?.RaiseCanExecuteChanged();
        CancelCommand?.RaiseCanExecuteChanged();
        RunExtraCommand?.RaiseCanExecuteChanged();
        SetPasswordCommand?.RaiseCanExecuteChanged();
        RelockCommand?.RaiseCanExecuteChanged();
        ForgetCurrentHostCommand?.RaiseCanExecuteChanged();
    }

    private void MoveSelection(int delta)
    {
        if (SelectedStep is null) return;
        var idx = Steps.IndexOf(SelectedStep) + delta;
        if (idx >= 0 && idx < Steps.Count && Steps[idx].Status != StepStatus.Locked)
            SelectedStep = Steps[idx];
    }

    public void Dispose()
    {
        _runCts?.Cancel();
        _shellStream?.Dispose();
        _ssh.Dispose();
    }
}
