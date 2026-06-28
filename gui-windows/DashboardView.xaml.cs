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
            var jobs = await _client.JobsAsync();

            var sites = _projects.Where(p => !p.IsFolder).ToList();
            var running = sites.Count(p => p.running);
            var issues = sites.Count(p => !string.IsNullOrEmpty(p.error));
            var svcOn = _services.Count(s => s.running);

            Sub.Text = $"{running} of {sites.Count} sites running";
            Ratio(TileSites, running, sites.Count);
            Ratio(TileServices, svcOn, _services.Count);
            TileIssues.Text = issues.ToString();
            TileIssues.Foreground = (Brush)FindResource(issues > 0 ? "Red" : "Text");
            TileShared.Text = _services.Count.ToString();

            RecentList.ItemsSource = sites.OrderByDescending(p => p.running).Take(5).ToList();
            ActiveList.ItemsSource = _services;
            NoServices.Visibility = _services.Count == 0 ? Visibility.Visible : Visibility.Collapsed;
            ActivityList.ItemsSource = jobs.Take(8).ToList();
            NoActivity.Visibility = jobs.Count == 0 ? Visibility.Visible : Visibility.Collapsed;

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
        var targets = _projects.Where(p => !p.IsFolder).Where(when).ToList();
        if (targets.Count == 0) { Ui.Toast(action == "start" ? "Nothing to start" : "Nothing to stop"); return; }
        Ui.Toast($"{(action == "start" ? "Starting" : "Stopping")} {targets.Count} site(s)…");
        foreach (var p in targets) { try { await _client.ProjectActionAsync(p.name, action); } catch { } }
        await RefreshAsync();
    }

    private async void OnStartAllServices(object sender, RoutedEventArgs e) => await ForEachService("start", s => !s.running);
    private async void OnStopAllServices(object sender, RoutedEventArgs e) => await ForEachService("stop", s => s.running);

    private async Task ForEachService(string action, Func<ServiceInfo, bool> when)
    {
        if (_client is null) return;
        var targets = _services.Where(when).ToList();
        if (targets.Count == 0) { Ui.Toast(action == "start" ? "Nothing to start" : "Nothing to stop"); return; }
        Ui.Toast($"{(action == "start" ? "Starting" : "Stopping")} {targets.Count} service(s)…");
        foreach (var s in targets) { try { await _client.ServiceActionAsync(s.name, action); } catch { } }
        await RefreshAsync();
    }

    private async void OnStartSite(object sender, RoutedEventArgs e)
    {
        if (_client is null || (sender as FrameworkElement)?.DataContext is not ProjectInfo p) return;
        await Ui.Run(() => _client.ProjectActionAsync(p.name, "start"), $"Starting {p.name}…");
        await RefreshAsync();
    }

    private void OnOpenSite(object sender, RoutedEventArgs e)
    {
        if ((sender as FrameworkElement)?.DataContext is ProjectInfo p && !string.IsNullOrEmpty(p.url)) Ui.OpenExternal(p.url!);
    }

    private void OnRecentRowClick(object sender, System.Windows.Input.MouseButtonEventArgs e)
    {
        if (e.OriginalSource is System.Windows.Controls.Primitives.ButtonBase) return;
        if ((sender as FrameworkElement)?.DataContext is not ProjectInfo p) return;
        var roots = Ui.Main?.Config?.roots ?? Array.Empty<string>();
        var root = roots.FirstOrDefault(r => Under(p.dir, r));
        Ui.Navigate(root is not null ? "root:" + root : "dashboard");
    }

    private async void OnStartService(object sender, RoutedEventArgs e)
    {
        if (_client is null || (sender as FrameworkElement)?.DataContext is not ServiceInfo svc) return;
        await Ui.Run(() => _client.ServiceActionAsync(svc.name, "start"), $"Starting {svc.name}…");
        await RefreshAsync();
    }

    private async void OnStopService(object sender, RoutedEventArgs e)
    {
        if (_client is null || (sender as FrameworkElement)?.DataContext is not ServiceInfo svc) return;
        await Ui.Run(() => _client.ServiceActionAsync(svc.name, "stop"), $"Stopping {svc.name}…");
        await RefreshAsync();
    }

    private static bool Under(string dir, string root)
    {
        static string N(string p) => p.Replace('\\', '/').TrimEnd('/').ToLowerInvariant();
        var d = N(dir); var r = N(root);
        return d == r || d.StartsWith(r + "/");
    }
}
