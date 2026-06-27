using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Media;

namespace Hull.Gui;

public partial class MainWindow : Window
{
    private HullClient? _client;
    private CancellationTokenSource? _events;
    private ConfigInfo? _config;
    private List<ProjectInfo> _projects = new();
    private string _current = "";
    private readonly List<(Border border, Icon icon, TextBlock text, string route)> _navRows = new();

    public MainWindow()
    {
        InitializeComponent();
        Loaded += async (_, _) => await ConnectAndLoad();
        Closed += (_, _) => _events?.Cancel();
    }

    private async Task ConnectAndLoad()
    {
        _client = await HullClient.ConnectAsync();
        if (_client is null)
        {
            DaemonDot.Fill = (Brush)FindResource("TextFaint");
            DaemonText.Text = "Daemon not running";
            BuildNav(0);
            SelectNav("dashboard");
            return;
        }
        DaemonDot.Fill = (Brush)FindResource("Green");
        DaemonText.Text = "Daemon running";
        try { _config = await _client.ConfigAsync(); } catch { }
        try { _projects = await _client.ProjectsAsync(); } catch { }
        var svc = 0;
        try { svc = (await _client.ServicesAsync()).Count; } catch { }
        BuildNav(svc);
        SelectNav("dashboard");
        StartEvents();
    }

    // ---- navigation ----------------------------------------------------

    private void BuildNav(int serviceCount)
    {
        NavPanel.Children.Clear();
        _navRows.Clear();
        AddNav("dashboard", "dashboard", "Dashboard", null);
        if (_config is not null)
        {
            foreach (var root in _config.roots)
            {
                var label = LastSegment(root);
                var glyph = label.ToLowerInvariant().Contains("app") ? "cube" : "sites";
                var count = _projects.Count(p => p.kind != "folder" && Under(p.dir, root));
                AddNav("root:" + root, glyph, label, count);
            }
        }
        AddSeparator();
        AddNav("services", "services", "Services", serviceCount > 0 ? serviceCount : null);
        AddNav("mail", "mail", "Mail", null);
        AddNav("logs", "logs", "Logs", null);
        AddNav("settings", "settings", "Settings", null);
    }

    private void AddSeparator() =>
        NavPanel.Children.Add(new Border { Height = 1, Background = (Brush)FindResource("Border"), Margin = new Thickness(10, 8, 10, 8) });

    private void AddNav(string route, string glyph, string label, int? count)
    {
        var icon = new Icon { Glyph = glyph, Width = 18, Height = 18, Brush = (Brush)FindResource("TextFaint"), VerticalAlignment = VerticalAlignment.Center };
        var text = new TextBlock { Text = label, Foreground = (Brush)FindResource("TextDim"), FontSize = 13, FontWeight = FontWeights.Medium, VerticalAlignment = VerticalAlignment.Center, Margin = new Thickness(11, 0, 0, 0) };

        var left = new StackPanel { Orientation = Orientation.Horizontal };
        left.Children.Add(icon);
        left.Children.Add(text);

        var grid = new Grid();
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        Grid.SetColumn(left, 0);
        grid.Children.Add(left);
        if (count is int c)
        {
            var cnt = new TextBlock { Text = c.ToString(), Foreground = (Brush)FindResource("TextFaint"), FontSize = 11, VerticalAlignment = VerticalAlignment.Center };
            Grid.SetColumn(cnt, 1);
            grid.Children.Add(cnt);
        }

        var border = new Border
        {
            Background = Brushes.Transparent,
            CornerRadius = new CornerRadius(7),
            Padding = new Thickness(10, 8, 10, 8),
            Margin = new Thickness(0, 1, 0, 1),
            Cursor = Cursors.Hand,
            Child = grid,
        };
        border.MouseLeftButtonUp += (_, _) => SelectNav(route);
        border.MouseEnter += (_, _) => { if (_current != route) border.Background = (Brush)FindResource("BgCard"); };
        border.MouseLeave += (_, _) => { if (_current != route) border.Background = Brushes.Transparent; };
        NavPanel.Children.Add(border);
        _navRows.Add((border, icon, text, route));
    }

    private void SelectNav(string route)
    {
        _current = route;
        foreach (var (border, icon, text, r) in _navRows)
        {
            var active = r == route;
            border.Background = active ? (Brush)FindResource("AccentSoft") : Brushes.Transparent;
            icon.Brush = active ? (Brush)FindResource("Accent") : (Brush)FindResource("TextFaint");
            text.Foreground = active ? (Brush)FindResource("Accent") : (Brush)FindResource("TextDim");
        }
        ContentHost.Content = MakeView(route);
    }

    private object MakeView(string route)
    {
        if (route.StartsWith("root:"))
            return new SitesView(_client, route.Substring(5));
        return route switch
        {
            "dashboard" => new DashboardView(_client),
            "services" => new ServicesView(_client),
            "settings" => new SettingsView(_client),
            "mail" => new PlaceholderView("Mail", "Mailpit inbox , coming next."),
            "logs" => new PlaceholderView("Logs", "Live container logs , coming next."),
            _ => new DashboardView(_client),
        };
    }

    private void StartEvents()
    {
        _events?.Cancel();
        _events = new CancellationTokenSource();
        var ct = _events.Token;
        _ = Task.Run(async () =>
        {
            try
            {
                await _client!.StreamEventsAsync(
                    running => Dispatcher.InvokeAsync(() =>
                    {
                        if (ContentHost.Content is IRefreshable r) _ = r.RefreshAsync();
                    }),
                    ct);
            }
            catch { /* stream ended */ }
        }, ct);
    }

    // ---- window chrome -------------------------------------------------

    private void TitleBar_MouseDown(object sender, MouseButtonEventArgs e)
    {
        if (e.ChangedButton != MouseButton.Left) return;
        if (e.ClickCount == 2) { ToggleMax(); return; }
        try { DragMove(); } catch { /* not in a drag-able state */ }
    }

    private void OnMinimize(object sender, RoutedEventArgs e) => WindowState = WindowState.Minimized;
    private void OnMaximize(object sender, RoutedEventArgs e) => ToggleMax();
    private void OnClose(object sender, RoutedEventArgs e) => Close();
    private void ToggleMax() => WindowState = WindowState == WindowState.Maximized ? WindowState.Normal : WindowState.Maximized;

    // ---- path helpers --------------------------------------------------

    private static string LastSegment(string path)
    {
        var t = path.TrimEnd('/', '\\');
        var i = t.LastIndexOfAny(new[] { '/', '\\' });
        return i >= 0 ? t[(i + 1)..] : t;
    }

    private static string Norm(string p) => p.Replace('\\', '/').TrimEnd('/').ToLowerInvariant();

    private static bool Under(string dir, string root)
    {
        var d = Norm(dir);
        var r = Norm(root);
        return d == r || d.StartsWith(r + "/");
    }
}
