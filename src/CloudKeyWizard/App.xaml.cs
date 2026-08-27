using System.IO;
using System.Windows;
using System.Windows.Threading;

namespace CloudKeyWizard;

/// <summary>
/// Interaction logic for App.xaml
/// </summary>
public partial class App : Application
{
    private static readonly string LogPath = Path.Combine(
        Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
        "CloudKeyWizard", "crash.log");

    protected override void OnStartup(StartupEventArgs e)
    {
        base.OnStartup(e);

        // This app runs destructive commands against real hardware over a live SSH session; an
        // unhandled exception should never just vanish -- log it and tell the operator plainly,
        // since the last thing on screen might have been mid-way through a step.
        DispatcherUnhandledException += OnDispatcherUnhandledException;
        AppDomain.CurrentDomain.UnhandledException += (_, args) =>
            LogCrash(args.ExceptionObject as Exception ?? new Exception("Unknown fatal error"));
    }

    private void OnDispatcherUnhandledException(object sender, DispatcherUnhandledExceptionEventArgs e)
    {
        LogCrash(e.Exception);
        MessageBox.Show(
            $"An unexpected error occurred:\n\n{e.Exception.Message}\n\nDetails were written to:\n{LogPath}\n\n" +
            "If a device operation was in progress, check the terminal pane and the device itself before continuing.",
            "CloudKey Wizard - Unexpected Error", MessageBoxButton.OK, MessageBoxImage.Error);
        e.Handled = true;
    }

    private static void LogCrash(Exception ex)
    {
        try
        {
            Directory.CreateDirectory(Path.GetDirectoryName(LogPath)!);
            File.AppendAllText(LogPath, $"[{DateTime.Now:u}] {ex}\n\n");
        }
        catch
        {
            // If we can't even log the crash, there's nothing further to do here.
        }
    }
}
