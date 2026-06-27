using System.Windows;
using System.Windows.Controls;
using System.Windows.Documents;
using System.Windows.Input;
using System.Windows.Media;
using System.Windows.Shapes;

namespace Hull.Gui;

public partial class SitesView : UserControl, IRefreshable
{
    private readonly HullClient? _client;
    private readonly string? _root;
    private List<ProjectInfo> _all = new();
    private GroupsStore? _groups;
    private ProjectInfo? _selected;
    private string _tab = "Overview";
    private readonly Dictionary<string, Border> _rows = new();
    private LogPanel? _logPanel;
    private CancellationTokenSource? _logCts;

    public SitesView(HullClient? client, string? rootPath = null)
    {
        InitializeComponent();
        _client = client;
        _root = rootPath;
        Loaded += async (_, _) => { await RefreshAsync(); if (Environment.GetEnvironmentVariable("HULL_AUTO_DIALOG") == "new") OnNew(this, new RoutedEventArgs()); };
        Unloaded += (_, _) => _logCts?.Cancel();
    }

    public async Task RefreshAsync()
    {
        if (_client is null) return;
        try
        {
            var projects = await _client.ProjectsAsync();
            _all = _root is null ? projects : projects.Where(p => Under(p.dir, _root)).ToList();
            try { _groups = await _client.GroupsAsync(); } catch { }
            BuildList();
            // Deep-link a site/tab for screenshots/tests.
            var envSite = Environment.GetEnvironmentVariable("HULL_SITE");
            var envTab = Environment.GetEnvironmentVariable("HULL_SITE_TAB");
            if (!string.IsNullOrEmpty(envTab)) _tab = envTab;
            var keep = _selected is not null ? _all.FirstOrDefault(p => p.name == _selected.name)
                     : !string.IsNullOrEmpty(envSite) ? _all.FirstOrDefault(p => p.name == envSite) : null;
            if (keep is not null) SelectSite(keep);
            else { var first = _all.FirstOrDefault(p => !p.IsFolder); if (first is not null) SelectSite(first); else ShowEmpty(); }
        }
        catch (Exception ex) { Ui.Toast(ex.Message); }
    }

    // ---- list pane -----------------------------------------------------
    private void OnSearch(object sender, TextChangedEventArgs e) => BuildList();

    private string[] GroupOrder()
    {
        if (_groups?.roots is null || _root is null) return Array.Empty<string>();
        foreach (var (k, v) in _groups.roots)
            if (NormKey(k) == NormKey(_root)) return v.groups ?? Array.Empty<string>();
        return Array.Empty<string>();
    }

    private void BuildList()
    {
        var q = Search.Text?.Trim().ToLowerInvariant() ?? "";
        SearchHint.Visibility = string.IsNullOrEmpty(Search.Text) ? Visibility.Visible : Visibility.Collapsed;
        NavList.Children.Clear();
        _rows.Clear();

        var managed = _all.Where(p => !p.IsFolder && (q == "" || p.name.ToLowerInvariant().Contains(q))).ToList();
        var order = GroupOrder().ToList();
        // groups present on projects but not in the stored order get appended
        foreach (var g in managed.Where(p => !string.IsNullOrEmpty(p.group)).Select(p => p.group!).Distinct())
            if (!order.Contains(g)) order.Add(g);

        foreach (var g in order)
        {
            var inGroup = managed.Where(p => p.group == g).OrderBy(p => p.name).ToList();
            NavList.Children.Add(GroupHeader(g, inGroup.Count));
            foreach (var p in inGroup) NavList.Children.Add(SiteRow(p));
        }
        var ungrouped = managed.Where(p => string.IsNullOrEmpty(p.group)).OrderBy(p => p.name).ToList();
        if (ungrouped.Count > 0 || order.Count > 0)
        {
            NavList.Children.Add(GroupHeader("Ungrouped", ungrouped.Count));
            foreach (var p in ungrouped) NavList.Children.Add(SiteRow(p));
        }

        var folders = _all.Where(p => p.IsFolder && (q == "" || p.name.ToLowerInvariant().Contains(q))).OrderBy(p => p.name).ToList();
        if (folders.Count > 0)
        {
            NavList.Children.Add(GroupHeader("Folders to import", folders.Count));
            foreach (var p in folders) NavList.Children.Add(FolderRow(p));
        }
    }

    private FrameworkElement GroupHeader(string label, int count)
    {
        var g = new Grid { Margin = new Thickness(6, 12, 4, 4) };
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        var ic = Ui.Glyph("folder", 12, "TextFaint"); ic.Margin = new Thickness(0, 0, 7, 0);
        var t = new TextBlock { Text = label, FontSize = 11, FontWeight = FontWeights.SemiBold, Foreground = B("TextFaint"), VerticalAlignment = VerticalAlignment.Center };
        var c = new TextBlock { Text = count.ToString(), FontSize = 11, Foreground = B("TextFaint"), VerticalAlignment = VerticalAlignment.Center };
        Grid.SetColumn(ic, 0); Grid.SetColumn(t, 1); Grid.SetColumn(c, 2);
        g.Children.Add(ic); g.Children.Add(t); g.Children.Add(c);
        return g;
    }

    private Border SiteRow(ProjectInfo p)
    {
        var dot = new Ellipse { Width = 8, Height = 8, VerticalAlignment = VerticalAlignment.Center, Margin = new Thickness(2, 0, 10, 0), Fill = B(p.error != null ? "Red" : p.running ? "Green" : "TextFaint") };
        var name = new TextBlock { Text = p.name, VerticalAlignment = VerticalAlignment.Center, Foreground = B("Text") };
        var left = new StackPanel { Orientation = Orientation.Horizontal };
        left.Children.Add(dot); left.Children.Add(name);
        if (p.IsCluster) left.Children.Add(new Border { Style = (Style)FindResource("Chip"), Margin = new Thickness(8, 0, 0, 0), Padding = new Thickness(5, 1, 5, 1), Child = new TextBlock { Text = "cluster", FontSize = 10, Foreground = B("TextDim") } });

        var grid = new Grid();
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        Grid.SetColumn(left, 0); grid.Children.Add(left);
        if (p.served && !p.IsCluster)
        {
            var lk = Ui.Glyph("lock", 12, "TextFaint"); Grid.SetColumn(lk, 1); grid.Children.Add(lk);
        }

        var b = new Border { Padding = new Thickness(8, 7, 8, 7), CornerRadius = new CornerRadius(7), Margin = new Thickness(0, 1, 0, 1), Cursor = Cursors.Hand, Background = Brushes.Transparent, Child = grid };
        b.MouseLeftButtonUp += (_, _) => SelectSite(p);
        b.MouseEnter += (_, _) => { if (_selected?.name != p.name) b.Background = B("BgCard"); };
        b.MouseLeave += (_, _) => { if (_selected?.name != p.name) b.Background = Brushes.Transparent; };
        b.ContextMenu = RowMenu(p);
        _rows[p.name] = b;
        return b;
    }

    private ContextMenu RowMenu(ProjectInfo p)
    {
        var menu = new ContextMenu();
        MenuItem MI(string header, string glyph, RoutedEventHandler onClick)
        {
            var mi = new MenuItem { Header = header };
            if (!string.IsNullOrEmpty(glyph)) mi.Icon = Ui.Glyph(glyph, 14, "TextDim");
            mi.Click += onClick;
            return mi;
        }
        menu.Items.Add(MI("Start", "play", async (_, _) => await Action(p, "start")));
        menu.Items.Add(MI("Stop", "stop", async (_, _) => await Action(p, "stop")));
        menu.Items.Add(MI("Restart", "restart", async (_, _) => await Action(p, "restart")));
        menu.Items.Add(new Separator());
        var move = new MenuItem { Header = "Move to" };
        foreach (var g in GroupOrder()) { var gg = g; move.Items.Add(MI(g, "", async (_, _) => await MoveTo(p, gg))); }
        move.Items.Add(MI("Ungrouped", "", async (_, _) => await MoveTo(p, "")));
        move.Items.Add(MI("New group…", "plus", (_, _) => NewGroupDialog(p)));
        menu.Items.Add(move);
        return menu;
    }

    private FrameworkElement FolderRow(ProjectInfo p)
    {
        var rect = new Rectangle { RadiusX = 7, RadiusY = 7, Stroke = B("BorderStrong"), StrokeThickness = 1, StrokeDashArray = new DoubleCollection { 4, 3 }, Fill = Brushes.Transparent };
        var name = new TextBlock { Text = p.name, Foreground = B("TextDim"), FontStyle = FontStyles.Italic, FontSize = 12, VerticalAlignment = VerticalAlignment.Center };
        var import = new TextBlock { Text = "Import", Foreground = B("Accent"), FontSize = 11, HorizontalAlignment = HorizontalAlignment.Right, VerticalAlignment = VerticalAlignment.Center, Visibility = Visibility.Collapsed };
        var inner = new Grid { Margin = new Thickness(11, 7, 11, 7) };
        inner.Children.Add(name); inner.Children.Add(import);
        var host = new Grid { Margin = new Thickness(0, 2, 0, 0), Cursor = Cursors.Hand, Opacity = 0.72 };
        host.Children.Add(rect); host.Children.Add(inner);
        host.MouseEnter += (_, _) => { host.Opacity = 1.0; import.Visibility = Visibility.Visible; };
        host.MouseLeave += (_, _) => { host.Opacity = 0.72; import.Visibility = Visibility.Collapsed; };
        host.MouseLeftButtonUp += async (_, _) => { if (_client is not null) await Ui.RunJob(() => _client.PostForJobAsync("/v1/imports", new { name = p.name }), $"Importing {p.name}…"); };
        return host;
    }

    private void SelectSite(ProjectInfo p)
    {
        _selected = p;
        foreach (var kv in _rows) kv.Value.Background = kv.Key == p.name ? B("AccentSoft") : Brushes.Transparent;
        _logCts?.Cancel();
        DetailHost.Content = BuildDetail(p);
    }

    // ---- detail pane ---------------------------------------------------
    private void ShowEmpty()
    {
        var box = new StackPanel { HorizontalAlignment = HorizontalAlignment.Center, VerticalAlignment = VerticalAlignment.Center };
        box.Children.Add(new Border { Width = 48, Height = 48, CornerRadius = new CornerRadius(13), Background = B("BgCard"), HorizontalAlignment = HorizontalAlignment.Center, Child = Ui.Glyph("sites", 22, "TextFaint") });
        box.Children.Add(new TextBlock { Text = "No project selected", FontSize = 16, FontWeight = FontWeights.SemiBold, HorizontalAlignment = HorizontalAlignment.Center, Margin = new Thickness(0, 14, 0, 4) });
        box.Children.Add(new TextBlock { Text = "Choose a site, or press ＋ to point Hull at a new folder.", Style = (Style)FindResource("Faint"), HorizontalAlignment = HorizontalAlignment.Center });
        DetailHost.Content = box;
    }

    private FrameworkElement BuildDetail(ProjectInfo p)
    {
        var dock = new DockPanel { Margin = new Thickness(24, 16, 24, 20) };

        // header (col0 = title, col1 = actions kept clear of the window controls)
        var headerGrid = new Grid { Margin = new Thickness(0, 0, 0, 12) };
        headerGrid.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        headerGrid.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        var titleRow = new StackPanel { Orientation = Orientation.Horizontal, VerticalAlignment = VerticalAlignment.Center, ClipToBounds = true };
        Grid.SetColumn(titleRow, 0);
        titleRow.Children.Add(new Ellipse { Width = 9, Height = 9, VerticalAlignment = VerticalAlignment.Center, Margin = new Thickness(0, 0, 10, 0), Fill = B(p.error != null ? "Red" : p.running ? "Green" : "TextFaint") });
        titleRow.Children.Add(new TextBlock { Text = p.name, FontSize = 19, FontWeight = FontWeights.SemiBold, VerticalAlignment = VerticalAlignment.Center });
        titleRow.Children.Add(Chip(p.DisplayKind, false, new Thickness(10, 0, 0, 0)));
        if (!string.IsNullOrEmpty(p.php)) titleRow.Children.Add(Chip("php " + p.php, true, new Thickness(6, 0, 0, 0)));
        DockPanel.SetDock(headerGrid, Dock.Top);
        headerGrid.Children.Add(titleRow);

        var actions = new StackPanel { Orientation = Orientation.Horizontal, HorizontalAlignment = HorizontalAlignment.Right, Margin = new Thickness(12, 0, 132, 0), VerticalAlignment = VerticalAlignment.Center };
        Grid.SetColumn(actions, 1);
        if (p.running)
        {
            actions.Children.Add(IconButton("restart", "Restart", "BtnSm", () => _ = Action(p, "restart")));
            actions.Children.Add(IconButton("stop", "Stop", "BtnSm", () => _ = Action(p, "stop"), new Thickness(6, 0, 0, 0)));
        }
        else actions.Children.Add(IconButton("play", p.error != null ? "Retry" : "Start", "BtnPrimary", () => _ = Action(p, "start")));
        actions.Children.Add(IconButton("cube", "Rebuild", "BtnSm", () => _ = Action(p, "rebuild"), new Thickness(6, 0, 0, 0)));
        actions.Children.Add(IconButton("trash", "Reset", "BtnSmDanger", () => ResetDialog(p), new Thickness(6, 0, 0, 0)));
        headerGrid.Children.Add(actions);
        dock.Children.Add(headerGrid);

        // url row
        var urlRow = new StackPanel { Orientation = Orientation.Horizontal, Margin = new Thickness(0, 0, 0, 16) };
        DockPanel.SetDock(urlRow, Dock.Top);
        if (!string.IsNullOrEmpty(p.url) && !p.IsCluster)
        {
            var inner = new StackPanel { Orientation = Orientation.Horizontal };
            inner.Children.Add(Ui.Glyph("lock", 13, "Accent")); ((Icon)inner.Children[0]).Margin = new Thickness(0, 0, 8, 0);
            inner.Children.Add(new TextBlock { Text = p.url, Foreground = B("Accent"), FontFamily = Ui.Mono, FontSize = 13, VerticalAlignment = VerticalAlignment.Center });
            var pill = new Border { Background = B("AccentSoft"), CornerRadius = new CornerRadius(7), Padding = new Thickness(11, 7, 12, 7), Cursor = Cursors.Hand, Child = inner };
            pill.MouseLeftButtonUp += (_, _) => Ui.OpenExternal(p.url!);
            urlRow.Children.Add(pill);
            urlRow.Children.Add(IconButton("external", "Open", "Btn", () => Ui.OpenExternal(p.url!), new Thickness(10, 0, 0, 0)));
        }
        else if (!p.IsCluster)
            urlRow.Children.Add(new TextBlock { Text = "headless — no routed domain (enable in Settings)", Style = (Style)FindResource("Faint"), VerticalAlignment = VerticalAlignment.Center });
        dock.Children.Add(urlRow);

        // tabs
        var tabBar = new StackPanel { Orientation = Orientation.Horizontal };
        var tabHost = new ContentControl();
        foreach (var nm in new[] { "Overview", "Services", "Logs", "Settings" })
        {
            var tabName = nm;
            var tb = new TextBlock { Text = nm, Margin = new Thickness(0, 9, 0, 9), Cursor = Cursors.Hand, FontSize = 13, FontWeight = FontWeights.Medium, Foreground = B(_tab == tabName ? "Text" : "TextDim") };
            var underline = new Border { Height = 2, Background = B("Accent"), VerticalAlignment = VerticalAlignment.Bottom, Visibility = _tab == tabName ? Visibility.Visible : Visibility.Collapsed };
            var cell = new Grid { Margin = new Thickness(0, 0, 22, 0) };
            cell.Children.Add(tb); cell.Children.Add(underline);
            cell.MouseLeftButtonUp += (_, _) => { _tab = tabName; tabHost.Content = TabContent(p); RepaintTabs(tabBar); };
            tabBar.Children.Add(cell);
        }
        var tabsBorder = new Border { BorderBrush = B("Border"), BorderThickness = new Thickness(0, 0, 0, 1), Child = tabBar };
        DockPanel.SetDock(tabsBorder, Dock.Top);
        dock.Children.Add(tabsBorder);

        tabHost.Content = TabContent(p);
        dock.Children.Add(new ScrollViewer { VerticalScrollBarVisibility = ScrollBarVisibility.Auto, Content = tabHost, Margin = new Thickness(0, 16, 0, 0) });
        return dock;
    }

    private void RepaintTabs(StackPanel bar)
    {
        foreach (Grid cell in bar.Children.OfType<Grid>())
        {
            var tb = cell.Children.OfType<TextBlock>().First();
            var ul = cell.Children.OfType<Border>().First();
            var active = tb.Text == _tab;
            tb.Foreground = B(active ? "Text" : "TextDim");
            ul.Visibility = active ? Visibility.Visible : Visibility.Collapsed;
        }
    }

    private FrameworkElement TabContent(ProjectInfo p) => _tab switch
    {
        "Services" => ServicesTab(p),
        "Logs" => LogsTab(p),
        "Settings" => SettingsTab(p),
        _ => p.IsCluster ? ClusterOverview(p) : Overview(p),
    };

    private FrameworkElement Overview(ProjectInfo p)
    {
        var sp = new StackPanel();
        if (p.LinkedServices.Length > 0)
        {
            sp.Children.Add(Ui.SectionLabel("Linked services"));
            foreach (var s in p.LinkedServices) sp.Children.Add(LinkedCard(s, p.running));
        }
        sp.Children.Add(Ui.SectionLabel("Location"));
        var card = new Border { Style = (Style)FindResource("Card") };
        var g = new Grid();
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        var fic = Ui.Glyph("folder", 16, "TextDim"); fic.Margin = new Thickness(0, 0, 10, 0);
        var loc = new TextBlock { Text = p.dir, FontFamily = Ui.Mono, FontSize = 12, Foreground = B("TextDim"), VerticalAlignment = VerticalAlignment.Center, TextTrimming = TextTrimming.CharacterEllipsis };
        var locBtns = new StackPanel { Orientation = Orientation.Horizontal };
        locBtns.Children.Add(IconButton("folder", "Open folder", "BtnSm", () => _ = OpenTarget(p, "folder")));
        locBtns.Children.Add(IconButton("editor", "Open in editor", "BtnSm", () => _ = OpenTarget(p, "editor"), new Thickness(6, 0, 0, 0)));
        Grid.SetColumn(fic, 0); Grid.SetColumn(loc, 1); Grid.SetColumn(locBtns, 2);
        g.Children.Add(fic); g.Children.Add(loc); g.Children.Add(locBtns);
        card.Child = g;
        sp.Children.Add(card);
        return sp;
    }

    private FrameworkElement ClusterOverview(ProjectInfo p)
    {
        var sp = new StackPanel();
        sp.Children.Add(Ui.SectionLabel("Cluster routes"));
        if (p.RouteList.Length == 0)
        {
            sp.Children.Add(new Border { Style = (Style)FindResource("Card"), Child = new TextBlock { Text = "No routes parsed. Edit hull.yaml to map subdomains to services.", Style = (Style)FindResource("Muted"), TextWrapping = TextWrapping.Wrap } });
            return sp;
        }
        var tld = Ui.Main?.Tld ?? "test";
        var card = new StackPanel();
        for (int i = 0; i < p.RouteList.Length; i++)
        {
            if (i > 0) card.Children.Add(new Border { Style = (Style)FindResource("Hairline"), Margin = new Thickness(0, 4, 0, 4) });
            var r = p.RouteList[i];
            var g = new Grid { Margin = new Thickness(0, 4, 0, 4) };
            g.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
            g.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
            g.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
            var dot = Ui.Dot(p.running ? "running" : "stopped"); dot.Margin = new Thickness(0, 0, 10, 0);
            var mid = new StackPanel { VerticalAlignment = VerticalAlignment.Center };
            mid.Children.Add(new TextBlock { Text = r.served ? $"https://{r.subdomain}.{tld}" : r.subdomain + " (not served)", FontWeight = FontWeights.Medium, Foreground = B(r.served ? "Accent" : "Text"), FontFamily = Ui.Mono, FontSize = 12.5 });
            mid.Children.Add(new TextBlock { Text = $"→ {r.service}:{r.port}", Style = (Style)FindResource("Faint") });
            Grid.SetColumn(dot, 0); Grid.SetColumn(mid, 1);
            g.Children.Add(dot); g.Children.Add(mid);
            if (r.served) { var open = IconButton("external", "Open", "BtnSm", () => Ui.OpenExternal($"https://{r.subdomain}.{tld}")); Grid.SetColumn(open, 2); g.Children.Add(open); }
            card.Children.Add(g);
        }
        sp.Children.Add(new Border { Style = (Style)FindResource("Card"), Child = card });
        sp.Children.Add(new TextBlock { Text = "Lifecycle (Start / Stop / Rebuild / Reset) acts on the whole compose project.", Style = (Style)FindResource("Help") });
        return sp;
    }

    private Border LinkedCard(ProjectServiceInfo s, bool running)
    {
        var card = new Border { Style = (Style)FindResource("Card"), Padding = new Thickness(14, 11, 14, 11), Margin = new Thickness(0, 0, 0, 8) };
        var g = new Grid();
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        var ic = Ui.Glyph(s.Glyph, 18, "TextDim"); ic.Margin = new Thickness(0, 0, 12, 0);
        var texts = new StackPanel { VerticalAlignment = VerticalAlignment.Center };
        texts.Children.Add(new TextBlock { Text = s.Title, FontWeight = FontWeights.Medium });
        texts.Children.Add(new TextBlock { Text = s.key + " · " + s.ModeText, Style = (Style)FindResource("Faint"), Margin = new Thickness(0, 2, 0, 0) });
        var dot = Ui.Dot(running ? "running" : "stopped");
        Grid.SetColumn(ic, 0); Grid.SetColumn(texts, 1); Grid.SetColumn(dot, 2);
        g.Children.Add(ic); g.Children.Add(texts); g.Children.Add(dot);
        card.Child = g;
        return card;
    }

    private FrameworkElement ServicesTab(ProjectInfo p)
    {
        var sp = new StackPanel();
        if (p.LinkedServices.Length > 0)
        {
            sp.Children.Add(Ui.SectionLabel("Linked services"));
            foreach (var s in p.LinkedServices)
            {
                var card = new Border { Style = (Style)FindResource("Card"), Padding = new Thickness(14, 11, 14, 11), Margin = new Thickness(0, 0, 0, 8) };
                var g = new Grid();
                g.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
                g.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
                g.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
                var ic = Ui.Glyph(s.Glyph, 18, "TextDim"); ic.Margin = new Thickness(0, 0, 12, 0);
                var texts = new StackPanel { VerticalAlignment = VerticalAlignment.Center };
                texts.Children.Add(new TextBlock { Text = s.key + " → " + s.Title, FontWeight = FontWeights.Medium });
                texts.Children.Add(new TextBlock { Text = s.ModeText, Style = (Style)FindResource("Faint"), Margin = new Thickness(0, 2, 0, 0) });
                var unlink = IconButton("unlink", "Unlink", "BtnSm", async () => { if (_client is not null) await Ui.RunJob(() => _client.PostForJobAsync($"/v1/projects/{Uri.EscapeDataString(p.name)}/unlink", new { key = s.key }), $"Unlinked {s.key}"); });
                Grid.SetColumn(ic, 0); Grid.SetColumn(texts, 1); Grid.SetColumn(unlink, 2);
                g.Children.Add(ic); g.Children.Add(texts); g.Children.Add(unlink);
                card.Child = g;
                sp.Children.Add(card);
            }
        }
        else sp.Children.Add(new TextBlock { Text = $"No services linked to {p.name} yet.", Style = (Style)FindResource("Muted"), Margin = new Thickness(0, 0, 0, 4) });
        sp.Children.Add(IconButton("link", "Link a service…", "Btn", () => LinkServiceDialog(p), new Thickness(0, 12, 0, 0)));
        return sp;
    }

    private FrameworkElement LogsTab(ProjectInfo p)
    {
        var sp = new StackPanel();
        var head = new StackPanel { Orientation = Orientation.Horizontal, Margin = new Thickness(0, 0, 0, 10) };
        head.Children.Add(new TextBlock { Text = "LIVE LOG", Style = (Style)FindResource("SectionLabel"), Margin = new Thickness(0, 0, 10, 0) });
        head.Children.Add(new Border { Style = (Style)FindResource("Chip"), VerticalAlignment = VerticalAlignment.Center, Child = new TextBlock { Text = p.name, FontFamily = Ui.Mono, FontSize = 11, Foreground = B("TextDim") } });
        sp.Children.Add(head);
        _logPanel = new LogPanel { Height = 380 };
        sp.Children.Add(_logPanel);
        _logCts?.Cancel();
        if (!p.running) { _logPanel.ShowHint("Start the project to stream its logs."); return sp; }
        _logCts = new CancellationTokenSource();
        var ct = _logCts.Token;
        if (_client is not null)
            _ = Task.Run(async () =>
            {
                try { await _client.StreamLogsAsync($"project={Uri.EscapeDataString(p.name)}&tail=200", line => Dispatcher.InvokeAsync(() => _logPanel?.Append(line)), ct); }
                catch { }
            }, ct);
        return sp;
    }

    private FrameworkElement SettingsTab(ProjectInfo p)
    {
        var sp = new StackPanel();
        sp.Children.Add(Ui.SectionLabel("Configuration"));
        var card = new StackPanel();
        var grid = new Grid { MaxWidth = 520, HorizontalAlignment = HorizontalAlignment.Left };
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        var c0 = new StackPanel { Margin = new Thickness(0, 0, 8, 0) };
        bool isPhp = p.kind is "laravel" or "plain";
        if (isPhp)
        {
            c0.Children.Add(Ui.FieldLabel("PHP version"));
            var php = Ui.Select(new[] { "8.4", "8.3", "8.2", "8.1" }.Select(v => (v, v)), string.IsNullOrEmpty(p.php) ? "8.4" : p.php);
            php.SelectionChanged += async (_, _) => { if (_client is not null) await Ui.Run(() => _client.PatchProjectAsync(p.name, new { php = Ui.SelectedVal(php) }), $"PHP set — restart to apply"); };
            c0.Children.Add(php);
        }
        else { c0.Children.Add(Ui.FieldLabel("Type")); c0.Children.Add(new TextBox { Style = (Style)FindResource("MonoInput"), Text = p.DisplayKind, IsReadOnly = true }); }
        var c1 = new StackPanel { Margin = new Thickness(8, 0, 0, 0) };
        c1.Children.Add(Ui.FieldLabel("Local domain"));
        c1.Children.Add(new TextBox { Style = (Style)FindResource("MonoInput"), Text = p.IsCluster ? "routes — see Overview" : (p.served ? p.name + "." + (Ui.Main?.Tld ?? "test") : "— headless —"), IsReadOnly = true });
        Grid.SetColumn(c0, 0); Grid.SetColumn(c1, 1);
        grid.Children.Add(c0); grid.Children.Add(c1);
        card.Children.Add(grid);
        if (!p.IsCluster)
        {
            card.Children.Add(new Border { Style = (Style)FindResource("Hairline") });
            var sr = new Grid();
            sr.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
            sr.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
            var info = new StackPanel { VerticalAlignment = VerticalAlignment.Center };
            info.Children.Add(new TextBlock { Text = "Serve a domain", FontWeight = FontWeights.Medium });
            info.Children.Add(new TextBlock { Text = "Off = runs headless (no vhost, DNS, or certificate). Restart to apply.", Style = (Style)FindResource("Faint"), TextWrapping = TextWrapping.Wrap });
            var tog = Ui.Toggle(p.served, async v => { if (_client is not null) await Ui.Run(() => _client.PatchProjectAsync(p.name, new { serve = v }), v ? "Domain enabled — restart to apply" : "Domain removed — restart to apply"); });
            Grid.SetColumn(info, 0); Grid.SetColumn(tog, 1);
            sr.Children.Add(info); sr.Children.Add(tog);
            card.Children.Add(sr);
        }
        sp.Children.Add(new Border { Style = (Style)FindResource("Card"), Child = card, Margin = new Thickness(0, 0, 0, 16) });

        // Danger zone
        sp.Children.Add(Ui.SectionLabel("Danger zone"));
        var dz = new StackPanel();
        dz.Children.Add(new TextBlock { Text = p.IsCluster ? "Un-adopt cluster" : "Destroy project", FontWeight = FontWeights.SemiBold, Foreground = B("Red"), Margin = new Thickness(0, 0, 0, 6) });
        dz.Children.Add(new TextBlock { Text = p.IsCluster ? "Stops the stack and removes Hull's manifest — your compose files are left untouched." : "Removes Hull's configuration, certificate, and unlinks services. Your files are not deleted.", Style = (Style)FindResource("Muted"), TextWrapping = TextWrapping.Wrap, Margin = new Thickness(0, 0, 0, 10) });
        var destroy = IconButton("trash", p.IsCluster ? "Un-adopt" : "Destroy", "BtnDanger", () => DestroyDialog(p));
        destroy.HorizontalAlignment = HorizontalAlignment.Left;
        dz.Children.Add(destroy);
        sp.Children.Add(new Border { Style = (Style)FindResource("DangerCard"), Child = dz });
        return sp;
    }

    // ---- dialogs -------------------------------------------------------
    private void NewGroupDialog(ProjectInfo? assign)
    {
        var input = new TextBox { Style = (Style)FindResource("Input") };
        var body = new StackPanel { MinWidth = 380 };
        body.Children.Add(Ui.FieldLabel("Group name"));
        body.Children.Add(input);
        var create = Ui.TextButton("Create", "", "BtnPrimary", async (_, _) =>
        {
            var name = input.Text.Trim();
            if (name.Length == 0) { Ui.Toast("Name the group"); return; }
            Ui.CloseDialog();
            await AddGroup(name);
            if (assign is not null) await MoveTo(assign, name);
            else await RefreshAsync();
        });
        Ui.ShowDialog(Ui.Dialog("New group", body, Ui.CancelButton(), create));
    }

    private void OnGroup(object sender, RoutedEventArgs e) => NewGroupDialog(null);

    private void OnAdopt(object sender, RoutedEventArgs e)
    {
        var dir = new TextBox { Style = (Style)FindResource("MonoInput") };
        var body = new StackPanel { MinWidth = 440 };
        body.Children.Add(new TextBlock { Text = "Wrap an existing docker compose project so Hull manages it as one unit. Your compose files are never modified.", Style = (Style)FindResource("Muted"), TextWrapping = TextWrapping.Wrap, Margin = new Thickness(0, 0, 0, 12) });
        body.Children.Add(Ui.FieldLabel("Project folder"));
        var browseRow = new Grid();
        browseRow.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        browseRow.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        Grid.SetColumn(dir, 0);
        var browse = Ui.TextButton("Browse", "folder", "Btn", (_, _) => { var p = Ui.PickFolder("Choose a compose project to adopt"); if (p != null) dir.Text = p; });
        browse.Margin = new Thickness(8, 0, 0, 0); Grid.SetColumn(browse, 1);
        browseRow.Children.Add(dir); browseRow.Children.Add(browse);
        body.Children.Add(browseRow);
        var root = new TextBox { Style = (Style)FindResource("MonoInput"), Margin = new Thickness(0, 10, 0, 0) };
        body.Children.Add(Ui.FieldLabel("Compose root (blank = project root)"));
        body.Children.Add(root);
        var adopt = Ui.TextButton("Adopt cluster", "cube", "BtnPrimary", async (_, _) =>
        {
            if (dir.Text.Trim().Length == 0) { Ui.Toast("Pick the project folder"); return; }
            Ui.CloseDialog();
            object req = root.Text.Trim().Length > 0 ? new { dir = dir.Text.Trim(), compose_root = root.Text.Trim() } : new { dir = dir.Text.Trim() };
            if (_client is not null) await Ui.RunJob(() => _client.PostForJobAsync("/v1/clusters", req), "Adopting cluster…");
        });
        Ui.ShowDialog(Ui.Dialog("Adopt a cluster", body, Ui.CancelButton(), adopt));
    }

    private async void LinkServiceDialog(ProjectInfo p)
    {
        if (_client is null) return;
        List<ServiceInfo> services;
        try { services = await _client.ServicesAsync(); } catch (Exception ex) { Ui.Toast(ex.Message); return; }
        var linkedInstances = new HashSet<string>(p.LinkedServices.Select(l => l.instance ?? l.engine));
        var opts = services.Where(s => !linkedInstances.Contains(s.name)).ToList();
        var body = new StackPanel { MinWidth = 400 };
        if (opts.Count == 0) body.Children.Add(new TextBlock { Text = "No shared instances yet — add one on the Services page.", Style = (Style)FindResource("Muted") });
        foreach (var s in opts)
        {
            var row = new Grid();
            row.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
            row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
            var ic = Ui.Glyph(s.Glyph, 16, "TextDim"); ic.Margin = new Thickness(0, 0, 10, 0);
            var mid = new StackPanel { VerticalAlignment = VerticalAlignment.Center };
            mid.Children.Add(new TextBlock { Text = s.name, FontWeight = FontWeights.Medium });
            mid.Children.Add(new TextBlock { Text = s.Badge + (s.HasPort ? "  ·  " + s.Endpoint : ""), Style = (Style)FindResource("Faint") });
            Grid.SetColumn(ic, 0); Grid.SetColumn(mid, 1);
            row.Children.Add(ic); row.Children.Add(mid);
            var card = new Border { CornerRadius = new CornerRadius(8), Padding = new Thickness(12, 9, 12, 9), Margin = new Thickness(0, 0, 0, 6), Cursor = Cursors.Hand, Background = B("BgCard"), BorderBrush = B("Border"), BorderThickness = new Thickness(1), Child = row };
            card.MouseLeftButtonUp += async (_, _) => { Ui.CloseDialog(); await Ui.RunJob(() => _client.PostForJobAsync($"/v1/services/{Uri.EscapeDataString(s.name)}/link", new { project = p.name }), $"Linking {s.name} to {p.name}…"); };
            body.Children.Add(card);
        }
        Ui.ShowDialog(Ui.Dialog($"Link a service to {p.name}", body, Ui.CancelButton("Done")));
    }

    private void ResetDialog(ProjectInfo p)
    {
        var body = new StackPanel { MinWidth = 420 };
        body.Children.Add(new TextBlock { Text = "This deletes the project's named volumes (databases, caches) and starts it fresh. Host bind-mounted files are NOT touched.", Style = (Style)FindResource("Muted"), TextWrapping = TextWrapping.Wrap, Margin = new Thickness(0, 0, 0, 12) });
        var vols = new TextBlock { Text = "Loading volumes…", Style = (Style)FindResource("Faint"), FontFamily = Ui.Mono, Margin = new Thickness(0, 0, 0, 12), TextWrapping = TextWrapping.Wrap };
        body.Children.Add(vols);
        var hint = new TextBlock { Style = (Style)FindResource("Faint"), Margin = new Thickness(0, 0, 0, 6) };
        hint.Inlines.Add(new Run("Type "));
        hint.Inlines.Add(new Run(p.name) { Foreground = B("Accent"), FontWeight = FontWeights.SemiBold });
        hint.Inlines.Add(new Run(" to confirm."));
        body.Children.Add(hint);
        var input = new TextBox { Style = (Style)FindResource("MonoInput") };
        body.Children.Add(input);
        var reset = Ui.TextButton("Reset", "trash", "BtnDanger", async (_, _) =>
        {
            if (input.Text != p.name) { Ui.Toast("Name doesn't match"); return; }
            Ui.CloseDialog();
            if (_client is not null) await Ui.RunJob(() => _client.PostForJobAsync($"/v1/projects/{Uri.EscapeDataString(p.name)}/reset"), $"Resetting {p.name}…");
        });
        Ui.ShowDialog(Ui.Dialog($"Reset {p.name}?", body, Ui.CancelButton(), reset));
        if (_client is not null)
            _ = _client.VolumesAsync(p.name).ContinueWith(t =>
            {
                if (t.IsCompletedSuccessfully)
                    Dispatcher.InvokeAsync(() => vols.Text = t.Result.Count > 0 ? string.Join("\n", t.Result.Select(v => "• " + v)) : "No named volumes — nothing to delete.");
            });
    }

    private void DestroyDialog(ProjectInfo p)
    {
        var body = new StackPanel { MinWidth = 400 };
        body.Children.Add(new TextBlock { Text = p.IsCluster ? "Stops the stack and removes Hull's manifest. Your compose files and repo are untouched." : "Removes Hull's configuration, certificate, and unlinks services. Your files are not deleted.", Style = (Style)FindResource("Muted"), TextWrapping = TextWrapping.Wrap, Margin = new Thickness(0, 0, 0, 12) });
        var hint = new TextBlock { Style = (Style)FindResource("Faint"), Margin = new Thickness(0, 0, 0, 6) };
        hint.Inlines.Add(new Run("Type "));
        hint.Inlines.Add(new Run(p.name) { Foreground = B("Accent"), FontWeight = FontWeights.SemiBold });
        hint.Inlines.Add(new Run(" to confirm."));
        body.Children.Add(hint);
        var input = new TextBox { Style = (Style)FindResource("MonoInput") };
        body.Children.Add(input);
        var destroy = Ui.TextButton(p.IsCluster ? "Un-adopt" : "Destroy", "trash", "BtnDanger", async (_, _) =>
        {
            if (input.Text != p.name) { Ui.Toast("Name doesn't match"); return; }
            Ui.CloseDialog();
            _selected = null;
            if (_client is not null) await Ui.RunJob(() => _client.DeleteForJobAsync($"/v1/projects/{Uri.EscapeDataString(p.name)}"), $"Destroying {p.name}…");
        });
        Ui.ShowDialog(Ui.Dialog(p.IsCluster ? $"Un-adopt {p.name}?" : $"Destroy {p.name}?", body, Ui.CancelButton(), destroy));
    }

    // ---- group store mutations ----------------------------------------
    private async Task AddGroup(string name)
    {
        if (_client is null || _root is null) return;
        var store = _groups ?? new GroupsStore(new(), new());
        var roots = store.roots ?? new();
        var key = roots.Keys.FirstOrDefault(k => NormKey(k) == NormKey(_root)) ?? _root;
        var list = (roots.TryGetValue(key, out var rg) ? rg.groups?.ToList() : null) ?? new List<string>();
        if (!list.Contains(name)) list.Add(name);
        roots[key] = new RootGroups(list.ToArray());
        var updated = new GroupsStore(roots, store.members ?? new());
        try { await _client.PutGroupsAsync(updated); _groups = updated; await RefreshAsync(); }
        catch (Exception ex) { Ui.Toast(ex.Message); }
    }

    private async Task MoveTo(ProjectInfo p, string group)
    {
        if (_client is null) return;
        try { await _client.PostForJobAsync($"/v1/projects/{Uri.EscapeDataString(p.name)}/group", new { group }); Ui.Toast(group.Length == 0 ? "Ungrouped" : $"Moved to {group}"); await RefreshAsync(); }
        catch (Exception ex) { Ui.Toast(ex.Message); }
    }

    // ---- actions -------------------------------------------------------
    private async Task Action(ProjectInfo p, string action)
    {
        if (_client is null) return;
        await Ui.RunJob(() => _client.PostForJobAsync($"/v1/projects/{Uri.EscapeDataString(p.name)}/{action}"),
            $"{char.ToUpper(action[0])}{action[1..]}ing {p.name}…");
    }

    private async Task OpenTarget(ProjectInfo p, string target)
    {
        if (_client is null) return;
        await Ui.Run(() => _client.OpenAsync(p.name, target), target == "folder" ? "Revealing folder…" : "Opening in editor…");
    }

    private void OnNew(object sender, RoutedEventArgs e)
    {
        if (Window.GetWindow(this) is MainWindow mw)
            mw.ShowDialog(new NewProjectDialog(_client, async () => await RefreshAsync()));
    }

    // ---- helpers -------------------------------------------------------
    private Brush B(string key) => (Brush)FindResource(key);

    private Border Chip(string text, bool accent, Thickness margin)
    {
        var b = new Border { Style = (Style)FindResource(accent ? "ChipAccent" : "Chip"), Margin = margin };
        b.Child = new TextBlock { Text = text, Foreground = B(accent ? "Accent" : "TextDim"), FontSize = 11 };
        return b;
    }

    private Button IconButton(string glyph, string text, string style, Action onClick, Thickness margin = default)
    {
        var b = Ui.TextButton(text, glyph, style, (_, _) => onClick(), style == "BtnSm" ? 13 : 15);
        b.Margin = margin;
        return b;
    }

    private static string NormKey(string p) => p.Replace('\\', '/').TrimEnd('/').ToLowerInvariant();

    private static bool Under(string dir, string root)
    {
        var d = NormKey(dir); var r = NormKey(root);
        return d == r || d.StartsWith(r + "/");
    }
}
