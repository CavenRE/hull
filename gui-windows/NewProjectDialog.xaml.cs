using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Media;

namespace Hull.Gui;

public partial class NewProjectDialog : UserControl
{
    private readonly HullClient? _client;
    private readonly Action? _onDone;
    private readonly TextBox _name;
    private readonly TextBox _php;
    private readonly CheckBox _redis;
    private Func<string> _template = () => "laravel";
    private Func<string> _db = () => "";

    public NewProjectDialog(HullClient? client, Action? onDone)
    {
        InitializeComponent();
        _client = client;
        _onDone = onDone;

        _name = Input();
        Body.Children.Add(Field("Name", _name));
        Body.Children.Add(Field("Template", Segmented(
            new[] { "Laravel", "WordPress", "Plain PHP" }, new[] { "laravel", "wordpress", "plain" }, 0, out _template)));
        Body.Children.Add(Field("Database", Segmented(
            new[] { "None", "Postgres", "MySQL", "MariaDB" }, new[] { "", "postgres", "mysql", "mariadb" }, 0, out _db)));
        _redis = new CheckBox { Content = "Add Redis", Foreground = (Brush)FindResource("TextDim"), Margin = new Thickness(0, 0, 0, 14) };
        Body.Children.Add(_redis);
        _php = Input("8.4");
        Body.Children.Add(Field("PHP version", _php));

        Loaded += (_, _) => _name.Focus();
    }

    private TextBox Input(string text = "") => new()
    {
        Style = (Style)FindResource("Input"),
        Text = text,
    };

    private FrameworkElement Field(string label, FrameworkElement element)
    {
        var sp = new StackPanel { Margin = new Thickness(0, 0, 0, 14) };
        sp.Children.Add(new TextBlock { Text = label, Style = (Style)FindResource("Muted"), Margin = new Thickness(0, 0, 0, 6) });
        sp.Children.Add(element);
        return sp;
    }

    private FrameworkElement Segmented(string[] labels, string[] values, int def, out Func<string> getter)
    {
        var sel = def;
        var panel = new StackPanel { Orientation = Orientation.Horizontal };
        var cells = new List<Border>();
        void Paint()
        {
            for (var i = 0; i < cells.Count; i++)
            {
                var active = i == sel;
                cells[i].Background = active ? (Brush)FindResource("AccentSoft") : (Brush)FindResource("Ctrl");
                ((TextBlock)cells[i].Child).Foreground = active ? (Brush)FindResource("Accent") : (Brush)FindResource("TextDim");
            }
        }
        for (var i = 0; i < labels.Length; i++)
        {
            var idx = i;
            var tb = new TextBlock { Text = labels[i], FontSize = 12 };
            var cell = new Border { Child = tb, Padding = new Thickness(11, 6, 11, 6), CornerRadius = new CornerRadius(7), Margin = new Thickness(0, 0, 6, 0), Cursor = Cursors.Hand };
            cell.MouseLeftButtonUp += (_, _) => { sel = idx; Paint(); };
            cells.Add(cell);
            panel.Children.Add(cell);
        }
        Paint();
        getter = () => values[sel];
        return panel;
    }

    private void OnCancel(object sender, RoutedEventArgs e) => Close();

    private async void OnCreate(object sender, RoutedEventArgs e)
    {
        if (_client is null) return;
        var name = _name.Text.Trim();
        if (name.Length == 0) { Status.Text = "Name is required."; return; }

        var req = new Dictionary<string, object> { ["name"] = name, ["template"] = _template() };
        var db = _db();
        if (db.Length > 0) req["db"] = db;
        if (_redis.IsChecked == true) req["redis"] = true;
        var php = _php.Text.Trim();
        if (php.Length > 0) req["php"] = php;

        Status.Text = "Creating…";
        try
        {
            await _client.CreateProjectAsync(req);
            Close();
            _onDone?.Invoke();
        }
        catch (Exception ex)
        {
            Status.Text = "Error: " + ex.Message;
        }
    }

    private void Close() => (Window.GetWindow(this) as MainWindow)?.CloseDialog();
}
