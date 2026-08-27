using System.Windows;

namespace CloudKeyWizard;

/// <summary>Small reusable scrollable read-only text window -- used for About and View Changelog,
/// where content is too long for a MessageBox to show comfortably (a MessageBox doesn't scroll).</summary>
public partial class TextWindow : Window
{
    public TextWindow(string title, string body)
    {
        InitializeComponent();
        Title = title;
        TitleText.Text = title;
        BodyText.Text = body;
    }

    private void CloseButton_Click(object sender, RoutedEventArgs e) => Close();
}
