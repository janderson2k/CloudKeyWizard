using System.Collections.Specialized;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Documents;
using System.Windows.Input;
using System.Windows.Media;
using CloudKeyWizard.Models;
using CloudKeyWizard.ViewModels;

namespace CloudKeyWizard;

public partial class MainWindow : Window
{
    private readonly MainViewModel _vm;

    public MainWindow()
    {
        InitializeComponent();
        _vm = new MainViewModel();
        DataContext = _vm;

        _vm.TerminalLines.CollectionChanged += TerminalLines_CollectionChanged;
        Closing += (_, _) => _vm.Dispose();

        // SetPasswordCommand clears the VM's copy of what was typed once it's done with it (so the
        // secret doesn't linger in memory) -- mirror that in the boxes themselves, since PasswordBox
        // isn't bound and wouldn't otherwise notice the VM-side clear.
        _vm.PropertyChanged += (_, e) =>
        {
            if (e.PropertyName == nameof(MainViewModel.NewPasswordValue) && string.IsNullOrEmpty(_vm.NewPasswordValue))
                NewPasswordBox.Clear();
            if (e.PropertyName == nameof(MainViewModel.ConfirmPasswordValue) && string.IsNullOrEmpty(_vm.ConfirmPasswordValue))
                ConfirmPasswordBox.Clear();
        };
    }

    private static readonly FontFamily TerminalFont = new("Cascadia Mono, Consolas, Courier New");

    private void TerminalLines_CollectionChanged(object? sender, NotifyCollectionChangedEventArgs e)
    {
        // FlowDocument.Blocks isn't bindable, so this mirrors the VM's ObservableCollection<TerminalLine>
        // (Add / RemoveAt(0) rolling cap -- see MainViewModel.AddTerminalLine) into RichTextBox
        // paragraphs directly, instead of rebuilding the whole document on every line.
        switch (e.Action)
        {
            case NotifyCollectionChangedAction.Add when e.NewItems is not null:
                foreach (var item in e.NewItems)
                    TerminalDocument.Blocks.Add(BuildTerminalParagraph((TerminalLine)item));
                break;
            case NotifyCollectionChangedAction.Remove when e.OldItems is not null:
                for (var i = 0; i < e.OldItems.Count && TerminalDocument.Blocks.FirstBlock is not null; i++)
                    TerminalDocument.Blocks.Remove(TerminalDocument.Blocks.FirstBlock);
                break;
            case NotifyCollectionChangedAction.Reset:
                TerminalDocument.Blocks.Clear();
                break;
        }

        // Keep the terminal pane pinned to the newest output, same expectation as any real terminal.
        Dispatcher.BeginInvoke(() => TerminalRichTextBox.ScrollToEnd());
    }

    private static Paragraph BuildTerminalParagraph(TerminalLine line) => new(new Run(line.Text) { Foreground = TerminalKindBrush(line.Kind) })
    {
        Margin = new Thickness(0, 1, 0, 1),
        FontFamily = TerminalFont,
        FontSize = 12,
    };

    // Same Kind -> color mapping as the old TerminalKindToBrushConverter (kept for any other binding
    // still using it) -- duplicated here rather than shared since a converter can't be called directly
    // from code-behind without an IValueConverter.Convert(...) ceremony call for no real benefit.
    private static SolidColorBrush TerminalKindBrush(TerminalLineKind kind) => kind switch
    {
        TerminalLineKind.CommandEcho => new SolidColorBrush(Color.FromRgb(0x6C, 0xC7, 0xFF)),
        TerminalLineKind.StdErr => new SolidColorBrush(Color.FromRgb(0xFF, 0x8A, 0x8A)),
        TerminalLineKind.AppInfo => new SolidColorBrush(Color.FromRgb(0xE0, 0xB0, 0x50)),
        _ => new SolidColorBrush(Color.FromRgb(0xD8, 0xD8, 0xD8)),
    };

    // PasswordBox deliberately doesn't support data binding to its Password property (that's by
    // design, to avoid the cleartext password sitting in the visual tree / bindings) -- wiring it
    // by hand through this event is the standard WPF pattern.
    private void PasswordInput_PasswordChanged(object sender, RoutedEventArgs e)
    {
        if (sender is PasswordBox pb) _vm.Password = pb.Password;
    }

    private void NewPasswordBox_PasswordChanged(object sender, RoutedEventArgs e)
    {
        if (sender is PasswordBox pb) _vm.NewPasswordValue = pb.Password;
    }

    private void ConfirmPasswordBox_PasswordChanged(object sender, RoutedEventArgs e)
    {
        if (sender is PasswordBox pb) _vm.ConfirmPasswordValue = pb.Password;
    }

    // A Locked step (a prerequisite hasn't completed) shouldn't be selectable via the nav list --
    // revert the selection rather than letting the middle pane show a step that isn't reachable yet.
    private void StepsList_SelectionChanged(object sender, SelectionChangedEventArgs e)
    {
        if (sender is not ListBox listBox) return;
        if (listBox.SelectedItem is PhaseStep { Status: StepStatus.Locked })
        {
            listBox.SelectedItem = e.RemovedItems.Count > 0 ? e.RemovedItems[0] : _vm.Steps[0];
        }
    }

    private void ExportHostsMenuItem_Click(object sender, RoutedEventArgs e)
    {
        var dlg = new Microsoft.Win32.SaveFileDialog
        {
            Filter = "JSON files (*.json)|*.json|All files (*.*)|*.*",
            FileName = "cloudkey-wizard-hosts.json",
        };
        if (dlg.ShowDialog() != true) return;
        try
        {
            _vm.ExportKnownHostsTo(dlg.FileName);
            MessageBox.Show($"Exported {_vm.KnownHosts.Count} host(s) to:\n{dlg.FileName}",
                "Export Known Hosts", MessageBoxButton.OK, MessageBoxImage.Information);
        }
        catch (Exception ex)
        {
            MessageBox.Show($"Export failed: {ex.Message}", "Export Known Hosts", MessageBoxButton.OK, MessageBoxImage.Error);
        }
    }

    private void ImportHostsMenuItem_Click(object sender, RoutedEventArgs e)
    {
        var dlg = new Microsoft.Win32.OpenFileDialog { Filter = "JSON files (*.json)|*.json|All files (*.*)|*.*" };
        if (dlg.ShowDialog() != true) return;
        try
        {
            _vm.ImportKnownHostsFrom(dlg.FileName);
            MessageBox.Show("Import complete -- entries were merged into your existing list (newer wins on conflict).",
                "Import Known Hosts", MessageBoxButton.OK, MessageBoxImage.Information);
        }
        catch (Exception ex)
        {
            MessageBox.Show($"Import failed: {ex.Message}", "Import Known Hosts", MessageBoxButton.OK, MessageBoxImage.Error);
        }
    }

    private void ExitMenuItem_Click(object sender, RoutedEventArgs e) => Close();

    private void AboutMenuItem_Click(object sender, RoutedEventArgs e)
    {
        var body = $"CloudKey Wizard {AppVersion.Version}  (built {AppVersion.BuildDate})\n\n" +
                   AppVersion.AboutText +
                   "\n\nEvery Phase 1 script is bundled into this exe at build time, pinned to a specific " +
                   "upstream commit -- no runtime GitHub dependency except the optional 'Replace " +
                   "Front-Panel LCD App' step, which reaches GitHub from the Cloud Key itself, not from " +
                   "this app.";
        new TextWindow("About CloudKey Wizard", body) { Owner = this }.ShowDialog();
    }

    private void ChangelogMenuItem_Click(object sender, RoutedEventArgs e)
    {
        var body = string.Join("\n\n", AppVersion.Changelog.Select(entry =>
            $"{entry.Version}  ({entry.Date})\n" + string.Join("\n", entry.Notes.Select(n => "  - " + n))));
        new TextWindow("CloudKey Wizard -- Changelog", body) { Owner = this }.ShowDialog();
    }

    private void TroubleshootingMenuItem_Click(object sender, RoutedEventArgs e)
    {
        MessageBox.Show(
            "A step seems stuck?\n" +
            "  Click Cancel (appears in the top bar while something's running). Phase 1 steps are " +
            "designed to be safely re-run afterward.\n\n" +
            "Can't connect?\n" +
            "  Confirm SSH is enabled on the device first: UniFi Console -> System Settings -> " +
            "Advanced -> SSH. That step is manual and happens on the device, not in this app.\n\n" +
            "Connection dropped partway through a step?\n" +
            "  The app tells a genuine dropped connection apart from a script failure, reconnects " +
            "automatically roughly every 10 seconds for about 2 minutes, and retries the step once " +
            "back (up to 2 automatic retries).\n\n" +
            "A Danger-tier step's Apply button won't turn on?\n" +
            "  Type the exact phrase shown above the box (or, for the storage-wipe step, the exact " +
            "selected device path) -- it has to match exactly before Apply enables.",
            "Troubleshooting", MessageBoxButton.OK, MessageBoxImage.Information);
    }

    // PreviewKeyDown, not KeyDown: TextBox has a built-in Ctrl+C "copy selection" command binding
    // that would otherwise consume the chord first, so Ctrl+C never reached a KeyDown handler here.
    private void ShellInputBox_PreviewKeyDown(object sender, KeyEventArgs e)
    {
        if (Keyboard.Modifiers == ModifierKeys.Control)
        {
            var controlByte = e.Key switch
            {
                Key.C => (byte?)0x03, // ETX -- SIGINT, interrupt the running command
                Key.D => (byte?)0x04, // EOT -- end of input
                Key.Z => (byte?)0x1A, // SUB -- SIGTSTP, suspend
                _ => null,
            };
            if (controlByte is not null)
            {
                _vm.SendRawShellByte(controlByte.Value, e.Key == Key.C ? "^C" : e.Key == Key.D ? "^D" : "^Z");
                e.Handled = true;
                return;
            }
        }

        if (e.Key == Key.Enter && _vm.SendShellCommand.CanExecute(null))
        {
            _vm.SendShellCommand.Execute(null);
            e.Handled = true;
        }
    }
}
