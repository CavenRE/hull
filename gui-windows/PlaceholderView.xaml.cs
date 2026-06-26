using System.Windows.Controls;

namespace Hull.Gui;

public partial class PlaceholderView : UserControl
{
    public PlaceholderView(string title, string body)
    {
        InitializeComponent();
        TitleText.Text = title;
        BodyText.Text = body;
    }
}
