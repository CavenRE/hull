using System.Windows;
using System.Windows.Controls;

namespace Hull.Gui;

public partial class LogsView : UserControl, IRefreshable
{
    private readonly HullClient? _client;
    private readonly LogPanel _panel = new();
    private CancellationTokenSource? _streams;
    private string _source = "all";

    public LogsView(HullClient? client)
    {
        InitializeComponent();
        _client = client;
        PanelHost.Child = _panel;
        Loaded += async (_, _) => await RefreshAsync();
        Unloaded += (_, _) => _streams?.Cancel();
    }

    private List<ProjectInfo> _projects = new();
    private List<ServiceInfo> _services = new();

    public async Task RefreshAsync()
    {
        _projects = Ui.Main?.Projects?.Where(p => p.kind != "folder").ToList() ?? new();
        try { _services = _client is null ? new() : await _client.ServicesAsync(); } catch { _services = new(); }
        BuildToolbar();
        StartStreams();
    }

    private void BuildToolbar()
    {
        var bar = new StackPanel { Orientation = Orientation.Horizontal };

        // Source select
        var opts = new List<(string, string)> { ("all", "All running") };
        foreach (var p in _projects) opts.Add(("project:" + p.name, p.name));
        foreach (var s in _services) opts.Add(("service:" + s.name, s.name));
        var src = Ui.Select(opts, _source, 220);
        src.SelectionChanged += (_, _) => { _source = Ui.SelectedVal(src); _panel.Clear(); StartStreams(); };
        bar.Children.Add(Labeled("Source", src));

        // Level segmented
        var level = Ui.Segmented(new[] { "All", "Info", "Warn", "Error" }, 0, i => _panel.SetLevel(i switch { 1 => "info", 2 => "warn", 3 => "error", _ => "all" }));
        level.Margin = new Thickness(14, 0, 0, 0);
        bar.Children.Add(Labeled("Level", level));

        // Search
        var search = new TextBox { Style = Ui.S("Input"), Width = 200 };
        search.TextChanged += (_, _) => _panel.SetSearch(search.Text);
        var searchWrap = Labeled("Search", search); searchWrap.Margin = new Thickness(14, 0, 0, 0);
        bar.Children.Add(searchWrap);

        // Follow toggle
        var follow = Ui.Toggle(true, v => _panel.Follow = v);
        var followWrap = Labeled("Follow", follow); followWrap.Margin = new Thickness(14, 0, 0, 0);
        bar.Children.Add(followWrap);

        // Clear
        var clear = Ui.TextButton("Clear", "", "BtnSm", (_, _) => _panel.Clear());
        var clearWrap = new StackPanel { VerticalAlignment = VerticalAlignment.Bottom, Margin = new Thickness(14, 0, 0, 0) };
        clearWrap.Children.Add(new TextBlock { Text = " ", FontSize = 11, Margin = new Thickness(0, 0, 0, 5) });
        clearWrap.Children.Add(clear);
        bar.Children.Add(clearWrap);

        Toolbar.Child = bar;
    }

    private StackPanel Labeled(string label, FrameworkElement control)
    {
        var sp = new StackPanel { VerticalAlignment = VerticalAlignment.Bottom };
        sp.Children.Add(Ui.FieldLabel(label));
        sp.Children.Add(control);
        return sp;
    }

    private void StartStreams()
    {
        _streams?.Cancel();
        _streams = new CancellationTokenSource();
        var ct = _streams.Token;
        if (_client is null) { _panel.ShowHint("Daemon not running."); return; }

        var sources = new List<(string kind, string name)>();
        if (_source == "all")
        {
            foreach (var p in _projects.Where(p => p.running)) sources.Add(("project", p.name));
            foreach (var s in _services.Where(s => s.running)) sources.Add(("service", s.name));
        }
        else if (_source.StartsWith("project:")) sources.Add(("project", _source["project:".Length..]));
        else if (_source.StartsWith("service:")) sources.Add(("service", _source["service:".Length..]));

        if (sources.Count == 0)
        {
            _panel.ShowHint(_source == "all" ? "No running sites or services — start one to stream its logs." : "Start this source to stream its logs.");
            return;
        }
        _panel.Clear();
        bool prefix = sources.Count > 1;
        foreach (var (kind, name) in sources)
        {
            var query = $"{kind}={Uri.EscapeDataString(name)}&tail=200";
            _ = Task.Run(async () =>
            {
                try
                {
                    await _client.StreamLogsAsync(query,
                        line => Dispatcher.InvokeAsync(() => _panel.Append(prefix ? $"{name}  {line}" : line)),
                        ct);
                }
                catch { /* stream ended / source stopped */ }
            }, ct);
        }
    }
}
