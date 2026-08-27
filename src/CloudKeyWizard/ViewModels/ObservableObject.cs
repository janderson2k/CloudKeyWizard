using System.ComponentModel;
using System.Runtime.CompilerServices;

namespace CloudKeyWizard.ViewModels;

/// <summary>Minimal INotifyPropertyChanged base. Deliberately not pulling in CommunityToolkit.Mvvm or
/// any other MVVM package -- this app ships as a self-contained single-file exe, so every extra
/// dependency is bytes in the portable exe for no functional gain over ~15 lines of boilerplate.</summary>
public abstract class ObservableObject : INotifyPropertyChanged
{
    public event PropertyChangedEventHandler? PropertyChanged;

    protected bool SetField<T>(ref T field, T value, [CallerMemberName] string? propertyName = null)
    {
        if (Equals(field, value)) return false;
        field = value;
        OnPropertyChanged(propertyName);
        return true;
    }

    protected void OnPropertyChanged([CallerMemberName] string? propertyName = null)
        => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(propertyName));
}
