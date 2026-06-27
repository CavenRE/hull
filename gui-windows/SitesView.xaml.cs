using System.Windows;
using System.Windows.Controls;

namespace Hull.Gui;

public partial class SitesView : UserControl, IRefreshable
{
    private readonly HullClient? _client;
    private readonly string? _root;
    private List<ProjectInfo> _all = new();

    public SitesView(HullClient? client, string? rootPath = null)
    {
        InitializeComponent();
        _client = client;
        _root = rootPath;
        if (rootPath is not null)
        {
            var t = rootPath.TrimEnd('/', '\\');
            var i = t.LastIndexOfAny(new[] { '/', '\\' });
            Title.Text = i >= 0 ? t[(i + 1)..] : t;
        }
        Loaded += async (_, _) => await RefreshAsync();
    }

    public async Task RefreshAsync()
    {
        if (_client is null) return;
        try
        {
            var projects = await _client.ProjectsAsync();
            _all = _root is null
                ? projects
                : projects.Where(p => Under(p.dir, _root)).ToList();
            ApplyFilter();
        }
        catch { /* surfaced elsewhere */ }
    }

    private static bool Under(string dir, string root)
    {
        static string N(string p) => p.Replace('\\', '/').TrimEnd('/').ToLowerInvariant();
        var d = N(dir);
        var r = N(root);
        return d == r || d.StartsWith(r + "/");
    }

    private void ApplyFilter()
    {
        var q = Search.Text?.Trim().ToLowerInvariant() ?? "";
        List.ItemsSource = string.IsNullOrEmpty(q)
            ? _all
            : _all.Where(p => p.name.ToLowerInvariant().Contains(q)).ToList();
    }

    private void OnSearch(object sender, TextChangedEventArgs e) => ApplyFilter();
    private async void OnRefresh(object sender, RoutedEventArgs e) => await RefreshAsync();
    private async void OnStart(object sender, RoutedEventArgs e) => await Act(sender, "start");
    private async void OnStop(object sender, RoutedEventArgs e) => await Act(sender, "stop");
    private async void OnRestart(object sender, RoutedEventArgs e) => await Act(sender, "restart");

    private async Task Act(object sender, string action)
    {
        if (_client is null) return;
        if ((sender as FrameworkElement)?.DataContext is not ProjectInfo p) return;
        try
        {
            await _client.ProjectActionAsync(p.name, action);
            await RefreshAsync();
        }
        catch { /* ignore; daemon surfaces detail */ }
    }
}
