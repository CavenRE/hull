using System.Windows;
using System.Windows.Controls;
using System.Windows.Documents;
using System.Windows.Media;
using System.Windows.Shapes;

namespace Hull.Gui;

public partial class DashboardView : UserControl, IRefreshable
{
    private readonly HullClient? _client;
    private List<ProjectInfo> _projects = new();
    private List<ServiceInfo> _services = new();

    public DashboardView(HullClient? client)
    {
        InitializeComponent();
        _client = client;
        Loaded += async (_, _) => await RefreshAsync();
    }

    public async Task RefreshAsync()
    {
        if (_client is null) return;
        try
        {
            _projects = await _client.ProjectsAsync();
            _services = await _client.ServicesAsync();
            var checks = await _client.DoctorAsync();

            var running = _projects.Count(p => p.running);
            var issues = _projects.Count(p => !string.IsNullOrEmpty(p.error));
            var svcOn = _services.Count(s => s.running);

            Sub.Text = $"{running} of {_projects.Count} sites running";
            Ratio(TileSites, running, _projects.Count);
            Ratio(TileServices, svcOn, _services.Count);
            TileIssues.Text = issues.ToString();
            TileIssues.Foreground = (Brush)FindResource(issues > 0 ? "Red" : "Text");
            TileShared.Text = _services.Count.ToString();

            RecentList.ItemsSource = _projects.OrderByDescending(p => p.running).Take(5).ToList();
            ActiveList.ItemsSource = _services;
            NoServices.Visibility = _services.Count == 0 ? Visibility.Visible : Visibility.Collapsed;

            BuildHealth(checks);
        }
        catch { /* ignore */ }
    }

    // "running / total" with the total greyed and smaller, like the design tile.
    private void Ratio(TextBlock tb, int n, int total)
    {
        tb.Inlines.Clear();
        tb.Inlines.Add(new Run(n.ToString()));
        tb.Inlines.Add(new Run($" / {total}") { Foreground = (Brush)FindResource("TextFaint"), FontSize = 18 });
    }

    private void BuildHealth(List<Check> checks)
    {
        HealthGrid.Children.Clear();
        AddHealth("Engine", "server", Find(checks, "container engine") ?? Find(checks, "docker"));
        AddHealth("Router", "route", Find(checks, "router"));
        AddHealth("DNS", "globe", Find(checks, "name resolution") ?? Find(checks, "dns"));
        AddHealth("Certificate", "cert", Find(checks, "certificate"));
    }

    private static Check? Find(List<Check> checks, string keyword) =>
        checks.FirstOrDefault(c => c.name.ToLowerInvariant().Contains(keyword));

    private void AddHealth(string title, string glyph, Check? c)
    {
        var status = c?.status ?? "warn";
        var dotBrush = (Brush)FindResource(status == "ok" ? "Green" : status == "fail" ? "Red" : "Amber");

        var icon = new Icon { Glyph = glyph, Width = 20, Height = 20, Brush = (Brush)FindResource("TextDim"), VerticalAlignment = VerticalAlignment.Center, Margin = new Thickness(0, 0, 12, 0) };
        var dot = new Ellipse { Width = 8, Height = 8, Fill = dotBrush, VerticalAlignment = VerticalAlignment.Center, Margin = new Thickness(0, 0, 7, 0) };
        var name = new TextBlock { Text = title, Foreground = (Brush)FindResource("Text"), FontWeight = FontWeights.Medium, VerticalAlignment = VerticalAlignment.Center };
        var nameRow = new StackPanel { Orientation = Orientation.Horizontal };
        nameRow.Children.Add(dot);
        nameRow.Children.Add(name);
        var detail = new TextBlock { Text = c?.detail ?? "unavailable", Foreground = (Brush)FindResource("TextFaint"), FontSize = 12, Margin = new Thickness(0, 3, 0, 0), TextTrimming = TextTrimming.CharacterEllipsis };
        var texts = new StackPanel();
        texts.Children.Add(nameRow);
        texts.Children.Add(detail);

        var inner = new StackPanel { Orientation = Orientation.Horizontal };
        inner.Children.Add(icon);
        inner.Children.Add(texts);

        HealthGrid.Children.Add(new Border
        {
            Background = (Brush)FindResource("BgCard"),
            BorderBrush = (Brush)FindResource("Border"),
            BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(10),
            Padding = new Thickness(14, 12, 14, 12),
            Margin = new Thickness(0, 0, 8, 8),
            Child = inner,
        });
    }

    private async void OnStartAll(object sender, RoutedEventArgs e) => await ForEachSite("start", p => !p.running);
    private async void OnStopAll(object sender, RoutedEventArgs e) => await ForEachSite("stop", p => p.running);

    private async Task ForEachSite(string action, Func<ProjectInfo, bool> when)
    {
        if (_client is null) return;
        foreach (var p in _projects.Where(when))
        {
            try { await _client.ProjectActionAsync(p.name, action); } catch { /* continue */ }
        }
        await RefreshAsync();
    }

    private async void OnStartSite(object sender, RoutedEventArgs e)
    {
        if (_client is null || (sender as FrameworkElement)?.DataContext is not ProjectInfo p) return;
        try { await _client.ProjectActionAsync(p.name, "start"); await RefreshAsync(); } catch { /* ignore */ }
    }

    private async void OnStartService(object sender, RoutedEventArgs e)
    {
        if (_client is null || (sender as FrameworkElement)?.DataContext is not ServiceInfo svc) return;
        try { await _client.ServiceActionAsync(svc.name, "start"); await RefreshAsync(); } catch { /* ignore */ }
    }
}
