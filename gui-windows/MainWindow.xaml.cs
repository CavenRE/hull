using System.Diagnostics;
using System.IO;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Media;
using System.Windows.Threading;

namespace Hull.Gui;

public partial class MainWindow : Window
{
    public static MainWindow? Current { get; private set; }

    private HullClient? _client;
    private CancellationTokenSource? _events;
    private ConfigInfo? _config;
    private List<ProjectInfo> _projects = new();
    private string _current = "";
    private int _serviceCount;
    private readonly List<(Border border, Icon icon, TextBlock text, string route)> _navRows = new();

    public HullClient? Client => _client;
    public ConfigInfo? Config => _config;
    public IReadOnlyList<ProjectInfo> Projects => _projects;
    public string Tld => _config?.tld ?? "test";
    public bool Connected => _client is not null;

    public MainWindow()
    {
        InitializeComponent();
        Current = this;
        Loaded += async (_, _) => await ConnectAndLoad();
        Closed += (_, _) => { _events?.Cancel(); _jobCts?.Cancel(); };
        ThemeManager.Changed += OnThemeChanged;
    }

    private void OnThemeChanged()
    {
        // StaticResource-bound view content doesn't react to a palette swap;
        // re-mount the current screen so it re-resolves brushes.
        if (!string.IsNullOrEmpty(_current)) ContentHost.Content = MakeView(_current);
    }

    private async Task ConnectAndLoad()
    {
        _client = await HullClient.ConnectAsync();
        // Auto-start the daemon if it isn't running and the user opted in.
        if (_client is null && ThemeManager.Prefs.start_daemon_on_launch)
        {
            DaemonText.Text = "Starting daemon…";
            SpawnDaemon();
            for (int i = 0; i < 16 && _client is null; i++) { await Task.Delay(500); _client = await HullClient.ConnectAsync(); }
        }
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
        try { _serviceCount = (await _client.ServicesAsync()).Count; } catch { }
        BuildNav(_serviceCount);
        SelectNav("dashboard");
        // Optional deep-link for screenshots/tests: HULL_START_SCREEN=services|
        // mail|logs|settings|sites.
        var start = Environment.GetEnvironmentVariable("HULL_START_SCREEN");
        if (!string.IsNullOrEmpty(start))
        {
            if (start.Equals("sites", StringComparison.OrdinalIgnoreCase) && _config?.roots.Length > 0)
                SelectNav("root:" + _config.roots[0]);
            else
                SelectNav(start.ToLowerInvariant());
        }
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

    // ---- modal dialogs -------------------------------------------------

    public void ShowDialog(FrameworkElement content)
    {
        DialogHost.Content = content;
        DialogLayer.Visibility = Visibility.Visible;
    }

    public void CloseDialog()
    {
        DialogLayer.Visibility = Visibility.Collapsed;
        DialogHost.Content = null;
        _jobDetailOpen = false;
    }

    private void OnScrimClick(object sender, MouseButtonEventArgs e)
    {
        if (ReferenceEquals(e.OriginalSource, DialogLayer)) CloseDialog();
    }

    private void OnMinimize(object sender, RoutedEventArgs e) => WindowState = WindowState.Minimized;
    private void OnMaximize(object sender, RoutedEventArgs e) => ToggleMax();
    private void OnClose(object sender, RoutedEventArgs e) => Close();
    private void ToggleMax() => WindowState = WindowState == WindowState.Maximized ? WindowState.Normal : WindowState.Maximized;

    // ---- toast -------------------------------------------------------
    private DispatcherTimer? _toastTimer;
    public void Toast(string msg)
    {
        ToastText.Text = msg;
        ToastHost.Visibility = Visibility.Visible;
        _toastTimer?.Stop();
        _toastTimer = new DispatcherTimer { Interval = TimeSpan.FromMilliseconds(2100) };
        _toastTimer.Tick += (_, _) => { ToastHost.Visibility = Visibility.Collapsed; _toastTimer?.Stop(); };
        _toastTimer.Start();
    }

    // ---- reload + navigation ----------------------------------------
    public async Task Reload()
    {
        if (_client is null) return;
        try { _config = await _client.ConfigAsync(); } catch { }
        try { _projects = await _client.ProjectsAsync(); } catch { }
        try { _serviceCount = (await _client.ServicesAsync()).Count; } catch { }
        BuildNav(_serviceCount);
        // keep the current row highlighted after a rebuild
        foreach (var (border, icon, text, r) in _navRows)
        {
            var active = r == _current;
            border.Background = active ? (Brush)FindResource("AccentSoft") : Brushes.Transparent;
            icon.Brush = active ? (Brush)FindResource("Accent") : (Brush)FindResource("TextFaint");
            text.Foreground = active ? (Brush)FindResource("Accent") : (Brush)FindResource("TextDim");
        }
        RefreshCurrent();
    }

    public void RefreshCurrent()
    {
        if (ContentHost.Content is IRefreshable r) _ = r.RefreshAsync();
    }

    public void Navigate(string route)
    {
        // Re-mount even if it's the same route (used after roots change).
        if (_navRows.All(n => n.route != route)) route = "dashboard";
        SelectNav(route);
    }

    // ---- job-aware action helper + status bar ------------------------
    private CancellationTokenSource? _jobCts;
    private string _jobTitle = "";
    private readonly List<string> _jobLines = new();
    private bool _jobDone, _jobFailed, _jobDetailOpen;

    /// <summary>Runs an action; streams its job in the status bar, or toasts + reloads.</summary>
    public async Task RunJob(Func<Task<JobInfo?>> call, string okMsg)
    {
        JobInfo? job;
        try { job = await call(); }
        catch (Exception ex) { Toast(Friendly(ex)); return; }
        if (job is not null) { StreamJob(job, Pretty(okMsg, job.kind)); return; }
        if (!string.IsNullOrEmpty(okMsg)) Toast(okMsg);
        await Reload();
    }

    /// <summary>Runs a non-job action; toasts the result and reloads.</summary>
    public async Task Run(Func<Task> call, string okMsg)
    {
        try { await call(); }
        catch (Exception ex) { Toast(Friendly(ex)); return; }
        if (!string.IsNullOrEmpty(okMsg)) Toast(okMsg);
        await Reload();
    }

    private static string Friendly(Exception ex) => string.IsNullOrWhiteSpace(ex.Message) ? "Action failed" : ex.Message;
    private static string Pretty(string okMsg, string kind) => !string.IsNullOrEmpty(okMsg) ? okMsg : (kind ?? "Working").Replace('_', ' ').Replace('-', ' ');

    private void StreamJob(JobInfo job, string title)
    {
        _jobCts?.Cancel();
        _jobCts = new CancellationTokenSource();
        var ct = _jobCts.Token;
        _jobTitle = title; _jobLines.Clear(); _jobDone = false; _jobFailed = false;
        PaintStatusBar();
        if (_client is null) return;
        _ = Task.Run(async () =>
        {
            try
            {
                await _client.StreamJobAsync(job.id,
                    line => Dispatcher.InvokeAsync(() => { _jobLines.Add(line); PaintStatusBar(); }),
                    (err, failed) => Dispatcher.InvokeAsync(async () =>
                    {
                        _jobFailed = failed; _jobDone = true;
                        if (failed && !string.IsNullOrEmpty(err)) _jobLines.Add(err!);
                        PaintStatusBar();
                        await Reload();
                    }), ct);
            }
            catch { _ = Dispatcher.InvokeAsync(async () => { _jobDone = true; PaintStatusBar(); await Reload(); }); }
        }, ct);
    }

    private DispatcherTimer? _sbHide;
    private void PaintStatusBar()
    {
        StatusBar.Visibility = Visibility.Visible;
        StatusBarIcon.Glyph = _jobDone ? (_jobFailed ? "alert" : "check") : "restart";
        StatusBarIcon.Brush = (Brush)FindResource(_jobDone ? (_jobFailed ? "Red" : "Green") : "Accent");
        StatusBarTitle.Text = _jobTitle + (_jobDone ? (_jobFailed ? " — failed" : " — done") : "");
        StatusBarLine.Text = _jobLines.Count > 0 ? _jobLines[^1] : (_jobDone ? "" : "starting…");
        if (_jobDetailOpen) PaintJobDetail();
        if (_jobDone)
        {
            _sbHide?.Stop();
            _sbHide = new DispatcherTimer { Interval = TimeSpan.FromMilliseconds(_jobFailed ? 14000 : 4500) };
            _sbHide.Tick += (_, _) => { StatusBar.Visibility = Visibility.Collapsed; _sbHide?.Stop(); };
            _sbHide.Start();
        }
    }

    private TextBox? _jobLog;
    private void OnStatusBarClick(object sender, MouseButtonEventArgs e)
    {
        _jobDetailOpen = true;
        var log = new TextBox
        {
            IsReadOnly = true, BorderThickness = new Thickness(0), Background = Brushes.Transparent,
            Foreground = (Brush)FindResource("TextDim"), FontFamily = (FontFamily)FindResource("FontMono"),
            FontSize = 12, TextWrapping = TextWrapping.NoWrap, VerticalScrollBarVisibility = ScrollBarVisibility.Auto,
            Height = 320,
        };
        _jobLog = log;
        var panel = new StackPanel { Width = 560 };
        panel.Children.Add(new TextBlock { Text = _jobTitle, FontSize = 16, FontWeight = FontWeights.SemiBold, Margin = new Thickness(0, 0, 0, 12) });
        panel.Children.Add(log);
        var close = new Button { Style = (Style)FindResource("Btn"), Content = "Close", HorizontalAlignment = HorizontalAlignment.Right, Margin = new Thickness(0, 14, 0, 0) };
        close.Click += (_, _) => { _jobDetailOpen = false; CloseDialog(); };
        panel.Children.Add(close);
        var card = new Border { Style = (Style)FindResource("Card"), Padding = new Thickness(20), Child = panel };
        ShowDialog(card);
        PaintJobDetail();
    }
    private void PaintJobDetail()
    {
        if (_jobLog is null) return;
        _jobLog.Text = string.Join("\n", _jobLines);
        _jobLog.ScrollToEnd();
    }

    private async void OnDaemonPillClick(object sender, MouseButtonEventArgs e)
    {
        if (_client is null) await ConnectAndLoad();
    }

    // ---- daemon lifecycle (Settings → Hull service) ------------------
    private void SpawnDaemon()
    {
        try
        {
            var hulld = Path.Combine(AppContext.BaseDirectory, "hulld.exe");
            var psi = File.Exists(hulld)
                ? new ProcessStartInfo(hulld) { UseShellExecute = false, CreateNoWindow = true }
                : new ProcessStartInfo("hulld") { UseShellExecute = false, CreateNoWindow = true };
            Process.Start(psi);
        }
        catch { /* daemon binary not found; user can start Hull manually */ }
    }

    public async Task StopDaemonAsync()
    {
        if (_client is null) return;
        Toast("Stopping sites & services…");
        try { await _client.StopAllAsync(); } catch { }
        try { await _client.ShutdownAsync(); } catch { }
        _events?.Cancel();
        _client = null;
        DaemonDot.Fill = (Brush)FindResource("TextFaint");
        DaemonText.Text = "Daemon offline";
        Toast("Daemon stopped — all sites & services shut down");
    }

    public async Task RestartDaemonAsync()
    {
        if (_client is not null)
        {
            try { await _client.ShutdownAsync(); } catch { }
            _events?.Cancel();
            _client = null;
        }
        DaemonDot.Fill = (Brush)FindResource("TextFaint");
        DaemonText.Text = "Restarting…";
        await Task.Delay(700);
        SpawnDaemon();
        for (int i = 0; i < 25; i++)
        {
            await Task.Delay(600);
            var c = await HullClient.ConnectAsync();
            if (c is not null) { await ConnectAndLoad(); Toast("Hull restarted"); return; }
        }
        DaemonText.Text = "Daemon not running";
        Toast("Restart timed out — try again");
    }

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
