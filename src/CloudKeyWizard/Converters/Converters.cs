using System.Globalization;
using System.Windows;
using System.Windows.Data;
using System.Windows.Media;
using CloudKeyWizard.Models;

namespace CloudKeyWizard.Converters;

public sealed class StatusToGlyphConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture) => value switch
    {
        StepStatus.Done => "✓",      // check
        StepStatus.Current => "●",   // filled circle
        StepStatus.Pending => "○",   // open circle
        StepStatus.Skipped => "⊖",   // circled minus
        StepStatus.Failed => "✕",    // cross
        StepStatus.Locked => "–", // en dash -- a padlock glyph renders inconsistently across fonts
        _ => "-",
    };

    public object ConvertBack(object value, Type targetType, object? parameter, CultureInfo culture) => throw new NotSupportedException();
}

public sealed class StatusToBrushConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture) => value switch
    {
        StepStatus.Done => new SolidColorBrush(Color.FromRgb(0x2E, 0xA0, 0x5A)),
        StepStatus.Current => new SolidColorBrush(Color.FromRgb(0x3A, 0x8E, 0xE6)),
        StepStatus.Failed => new SolidColorBrush(Color.FromRgb(0xD6, 0x3B, 0x3B)),
        StepStatus.Skipped => new SolidColorBrush(Color.FromRgb(0x9A, 0x9A, 0x9A)),
        StepStatus.Locked => new SolidColorBrush(Color.FromRgb(0x6E, 0x6E, 0x6E)),
        _ => new SolidColorBrush(Color.FromRgb(0xB0, 0xB0, 0xB0)),
    };

    public object ConvertBack(object value, Type targetType, object? parameter, CultureInfo culture) => throw new NotSupportedException();
}

public sealed class SeverityToBrushConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture) => value switch
    {
        StepSeverity.Danger => new SolidColorBrush(Color.FromRgb(0x5A, 0x1E, 0x1E)),
        StepSeverity.Caution => new SolidColorBrush(Color.FromRgb(0x5A, 0x4A, 0x12)),
        _ => new SolidColorBrush(Color.FromRgb(0x1E, 0x3A, 0x5A)),
    };

    public object ConvertBack(object value, Type targetType, object? parameter, CultureInfo culture) => throw new NotSupportedException();
}

public sealed class SeverityToLabelConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture) => value switch
    {
        StepSeverity.Danger => "DANGER — IRREVERSIBLE",
        StepSeverity.Caution => "CAUTION",
        _ => "INFO",
    };

    public object ConvertBack(object value, Type targetType, object? parameter, CultureInfo culture) => throw new NotSupportedException();
}

public sealed class TerminalKindToBrushConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture) => value switch
    {
        TerminalLineKind.CommandEcho => new SolidColorBrush(Color.FromRgb(0x6C, 0xC7, 0xFF)),
        TerminalLineKind.StdErr => new SolidColorBrush(Color.FromRgb(0xFF, 0x8A, 0x8A)),
        TerminalLineKind.AppInfo => new SolidColorBrush(Color.FromRgb(0xE0, 0xB0, 0x50)),
        _ => new SolidColorBrush(Color.FromRgb(0xD8, 0xD8, 0xD8)),
    };

    public object ConvertBack(object value, Type targetType, object? parameter, CultureInfo culture) => throw new NotSupportedException();
}

/// <summary>True when the bound value's ToString() equals the converter parameter -- used to show/hide
/// per-step panels in the middle pane keyed off PhaseStep.Id without a template per step.</summary>
public sealed class StringEqualsToVisibilityConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture)
        => string.Equals(value?.ToString(), parameter?.ToString(), StringComparison.Ordinal) ? Visibility.Visible : Visibility.Collapsed;

    public object ConvertBack(object value, Type targetType, object? parameter, CultureInfo culture) => throw new NotSupportedException();
}

public sealed class InverseBoolConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture) => !(value is true);
    public object ConvertBack(object value, Type targetType, object? parameter, CultureInfo culture) => !(value is true);
}

public sealed class BoolToVisibilityConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture)
        => value is true ? Visibility.Visible : Visibility.Collapsed;

    public object ConvertBack(object value, Type targetType, object? parameter, CultureInfo culture)
        => value is Visibility.Visible;
}

public sealed class InverseBoolToVisibilityConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture)
        => value is true ? Visibility.Collapsed : Visibility.Visible;

    public object ConvertBack(object value, Type targetType, object? parameter, CultureInfo culture)
        => value is Visibility.Collapsed;
}

/// <summary>Visible only for Caution/Danger steps -- Info steps don't need a colored warning banner.</summary>
public sealed class SeverityIsWarningToVisibilityConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture)
        => value is StepSeverity.Caution or StepSeverity.Danger ? Visibility.Visible : Visibility.Collapsed;

    public object ConvertBack(object value, Type targetType, object? parameter, CultureInfo culture) => throw new NotSupportedException();
}

public sealed class OutcomeVerdictToBrushConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture) => value switch
    {
        OutcomeVerdict.Go => new SolidColorBrush(Color.FromRgb(0x1B, 0x4A, 0x28)),
        OutcomeVerdict.NoGo => new SolidColorBrush(Color.FromRgb(0x5A, 0x1E, 0x1E)),
        OutcomeVerdict.Inconclusive => new SolidColorBrush(Color.FromRgb(0x5A, 0x4A, 0x12)),
        _ => Brushes.Transparent,
    };

    public object ConvertBack(object value, Type targetType, object? parameter, CultureInfo culture) => throw new NotSupportedException();
}

public sealed class OutcomeVerdictToLabelConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture) => value switch
    {
        OutcomeVerdict.Go => "✓ GO",
        OutcomeVerdict.NoGo => "✕ NO-GO",
        OutcomeVerdict.Inconclusive => "⚠ REVIEW BEFORE PROCEEDING",
        _ => "",
    };

    public object ConvertBack(object value, Type targetType, object? parameter, CultureInfo culture) => throw new NotSupportedException();
}

public sealed class BoolToStatusDotBrushConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture)
        => value is true ? new SolidColorBrush(Color.FromRgb(0x2E, 0xA0, 0x5A)) : new SolidColorBrush(Color.FromRgb(0x6E, 0x6E, 0x6E));

    public object ConvertBack(object value, Type targetType, object? parameter, CultureInfo culture) => throw new NotSupportedException();
}

public sealed class DeviceRiskToBrushConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture) => value switch
    {
        DeviceRisk.Recommended => new SolidColorBrush(Color.FromRgb(0x5E, 0xD6, 0x85)),
        DeviceRisk.DoNotSelect => new SolidColorBrush(Color.FromRgb(0xFF, 0x8A, 0x8A)),
        _ => new SolidColorBrush(Color.FromRgb(0xED, 0xED, 0xEF)),
    };

    public object ConvertBack(object value, Type targetType, object? parameter, CultureInfo culture) => throw new NotSupportedException();
}

/// <summary>Binds a RadioButton's IsChecked to a string property against a fixed candidate value
/// (the ConverterParameter) -- the standard WPF pattern for a radio group backed by one string.</summary>
public sealed class StringEqualsToBoolConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture)
        => string.Equals(value?.ToString(), parameter?.ToString(), StringComparison.Ordinal);

    public object ConvertBack(object value, Type targetType, object? parameter, CultureInfo culture)
        => value is true ? parameter?.ToString() ?? string.Empty : Binding.DoNothing;
}

public sealed class StringHasValueToVisibilityConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture)
        => !string.IsNullOrEmpty(value as string) ? Visibility.Visible : Visibility.Collapsed;

    public object ConvertBack(object value, Type targetType, object? parameter, CultureInfo culture) => throw new NotSupportedException();
}

public sealed class StringEmptyToVisibilityConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture)
        => string.IsNullOrEmpty(value as string) ? Visibility.Visible : Visibility.Collapsed;

    public object ConvertBack(object value, Type targetType, object? parameter, CultureInfo culture) => throw new NotSupportedException();
}

public sealed class IntGreaterThanZeroToVisibilityConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture)
        => value is int i && i > 0 ? Visibility.Visible : Visibility.Collapsed;

    public object ConvertBack(object value, Type targetType, object? parameter, CultureInfo culture) => throw new NotSupportedException();
}

public sealed class NullToVisibilityConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture)
        => value is null ? Visibility.Collapsed : Visibility.Visible;

    public object ConvertBack(object value, Type targetType, object? parameter, CultureInfo culture) => throw new NotSupportedException();
}
