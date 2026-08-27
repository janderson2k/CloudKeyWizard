using System.Windows;
using System.Windows.Controls;
using System.Windows.Data;

namespace CloudKeyWizard;

/// <summary>
/// WPF's PasswordBox deliberately doesn't support binding its Password property directly (so a
/// plaintext password never sits in a bindable dependency property where a debugger/binding trace
/// could see it) -- this app's three existing PasswordBox usages (Connect, New/Confirm password on
/// Finish) work around that with a fixed x:Name and a PasswordChanged event handler in code-behind,
/// which only works for a single, statically-named instance.
///
/// The Extras' per-Param secret fields (ADMIN_PASSWORD, WG_CONFIG) are different: they come from a
/// dynamic ItemsControl DataTemplate, so there's no fixed x:Name to hang a code-behind handler off
/// of. This attached property is the standard WPF answer to that specific gap.
///
/// Registers ONE static class handler for PasswordBox.PasswordChanged, app-wide, rather than
/// attaching a per-instance handler lazily from inside BoundPassword's own change callback -- an
/// earlier design did the latter and had a real, live-confirmed bug: WPF doesn't reliably invoke a
/// property-changed callback when a freshly-applied binding's initial value equals the property's
/// declared default (both were "" here), so the callback -- and the per-instance attachment inside
/// it -- silently never ran at all. A class handler sidesteps that question entirely by firing
/// unconditionally on the very first real event, for every PasswordBox in the app; the
/// GetBindingExpression check below is what tells this one apart from the app's other, unrelated
/// PasswordBox usages.
/// </summary>
public static class PasswordBoxHelper
{
    public static readonly DependencyProperty BoundPassword = DependencyProperty.RegisterAttached(
        "BoundPassword", typeof(string), typeof(PasswordBoxHelper),
        new FrameworkPropertyMetadata(string.Empty, FrameworkPropertyMetadataOptions.BindsTwoWayByDefault, OnBoundPasswordChanged));

    // Guards against a feedback loop: setting PasswordBox.Password (to sync FROM the bound value)
    // fires PasswordChanged, which would otherwise immediately push right back into the bound
    // value again.
    private static readonly DependencyProperty IsUpdating = DependencyProperty.RegisterAttached(
        "IsUpdating", typeof(bool), typeof(PasswordBoxHelper), new PropertyMetadata(false));

    public static string GetBoundPassword(DependencyObject d) => (string)d.GetValue(BoundPassword);
    public static void SetBoundPassword(DependencyObject d, string value) => d.SetValue(BoundPassword, value);

    static PasswordBoxHelper()
    {
        EventManager.RegisterClassHandler(typeof(PasswordBox), PasswordBox.PasswordChangedEvent,
            new RoutedEventHandler(OnPasswordChanged));
    }

    private static void OnPasswordChanged(object sender, RoutedEventArgs e)
    {
        if (sender is not PasswordBox box) return;

        // This class handler fires for EVERY PasswordBox in the whole app, including the 3
        // pre-existing fixed-instance ones (Connect, New/Confirm password) that use their own
        // code-behind handler instead and never touch BoundPassword at all. GetBindingExpression is
        // the direct, purpose-built way to tell "does this box actually use BoundPassword" apart
        // from "some other PasswordBox happened to fire" -- ReadLocalValue was tried first and
        // turned out not to reliably report this specific attached-property-on-a-templated-child
        // case (confirmed live: switching to this fixed it).
        if (BindingOperations.GetBindingExpression(box, BoundPassword) is null) return;

        box.SetValue(IsUpdating, true);
        box.SetCurrentValue(BoundPassword, box.Password);
        box.SetValue(IsUpdating, false);
    }

    private static void OnBoundPasswordChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
    {
        if (d is not PasswordBox box) return;
        if (!(bool)box.GetValue(IsUpdating))
        {
            box.Password = e.NewValue as string ?? string.Empty;
        }
    }
}
