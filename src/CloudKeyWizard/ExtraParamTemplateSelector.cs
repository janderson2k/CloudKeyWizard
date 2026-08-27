using System.Windows;
using System.Windows.Controls;
using CloudKeyWizard.Models;

namespace CloudKeyWizard;

/// <summary>
/// Picks which of 4 mutually-exclusive per-Param edit templates to instantiate, based on
/// ExtraParam's fixed (init-only) IsMultiline/IsDropdown/IsSecret flags -- see the doc comment on
/// the 4 named templates in MainWindow.xaml (PlainParamTemplate/DropdownParamTemplate/
/// SecretParamTemplate/MultilineParamTemplate) for the real live bug this replaced: a single shared
/// DataTemplate that instantiated all 4 alternate edit controls together (only Visibility toggling
/// between them) meant every one of them stayed TwoWay-bound to the same ExtraParam.Value at once,
/// even while Collapsed. The ComboBox specifically -- bound to an empty Options list for every
/// non-dropdown param -- would resolve SelectedItem to null the instant Value changed to anything
/// not present in that empty list, and because that binding is TwoWay, WPF pushed the null straight
/// back into Value, silently erasing whatever had just been typed into the PasswordBox a moment
/// before. Confirmed live, twice, after two earlier (real, but insufficient on their own) fixes to
/// PasswordBoxHelper itself didn't resolve it. Only materializing the one relevant control per
/// param removes every other unrelated binding from existing at all for that param.
/// </summary>
public sealed class ExtraParamTemplateSelector : DataTemplateSelector
{
    public DataTemplate? PlainTemplate { get; set; }
    public DataTemplate? DropdownTemplate { get; set; }
    public DataTemplate? SecretTemplate { get; set; }
    public DataTemplate? MultilineTemplate { get; set; }

    public override DataTemplate? SelectTemplate(object? item, DependencyObject container)
    {
        if (item is not ExtraParam p) return base.SelectTemplate(item, container);
        if (p.IsMultiline) return MultilineTemplate;
        if (p.IsDropdown) return DropdownTemplate;
        if (p.IsSecret) return SecretTemplate;
        return PlainTemplate;
    }
}
