using System.Diagnostics;
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
    private ProjectInfo? _selected;
    private string _tab = "Overview";
    private readonly Dictionary<string, Border> _rows = new();

    public SitesView(HullClient? client, string? rootPath = null)
    {
        InitializeComponent();
        _client = client;
        _root = rootPath;
        Loaded += async (_, _) => await RefreshAsync();
    }

    public async Task RefreshAsync()
    {
        if (_client is null) return;
        try
        {
            var projects = await _client.ProjectsAsync();
            _all = _root is null ? projects : projects.Where(p => Under(p.dir, _root)).ToList();
            BuildList();
            var keep = _selected is null ? null : _all.FirstOrDefault(p => p.name == _selected.name);
            if (keep is not null)
            {
                SelectSite(keep);
            }
            else
            {
                var first = _all.FirstOrDefault(p => !p.IsFolder);
                if (first is not null) SelectSite(first);
                else ShowEmpty();
            }
        }
        catch { /* ignore */ }
    }

    // ---- list pane -----------------------------------------------------

    private void OnSearch(object sender, TextChangedEventArgs e) => BuildList();

    private void BuildList()
    {
        var q = Search.Text?.Trim().ToLowerInvariant() ?? "";
        SearchHint.Visibility = string.IsNullOrEmpty(Search.Text) ? Visibility.Visible : Visibility.Collapsed;
        NavList.Children.Clear();
        _rows.Clear();

        var managed = _all.Where(p => !p.IsFolder && (q == "" || p.name.ToLowerInvariant().Contains(q))).ToList();
        foreach (var grp in managed.GroupBy(p => p.GroupName).OrderBy(g => g.Key))
        {
            NavList.Children.Add(GroupHeader(grp.Key, grp.Count()));
            foreach (var p in grp.OrderBy(p => p.name)) NavList.Children.Add(SiteRow(p));
        }

        var folders = _all.Where(p => p.IsFolder && (q == "" || p.name.ToLowerInvariant().Contains(q))).ToList();
        if (folders.Count > 0)
        {
            NavList.Children.Add(GroupHeader("Folders to import", folders.Count));
            foreach (var p in folders.OrderBy(p => p.name)) NavList.Children.Add(FolderRow(p));
        }
    }

    private FrameworkElement GroupHeader(string label, int count)
    {
        var t = new TextBlock { FontSize = 11, FontWeight = FontWeights.SemiBold, Margin = new Thickness(6, 12, 0, 4), Foreground = (Brush)FindResource("TextFaint") };
        t.Inlines.Add(new Run(label));
        t.Inlines.Add(new Run("   " + count));
        return t;
    }

    private Border SiteRow(ProjectInfo p)
    {
        var dot = new Ellipse { Width = 8, Height = 8, VerticalAlignment = VerticalAlignment.Center, Margin = new Thickness(2, 0, 10, 0), Fill = (Brush)FindResource(p.running ? "Green" : "TextFaint") };
        var name = new TextBlock { Text = p.name, VerticalAlignment = VerticalAlignment.Center, Foreground = (Brush)FindResource("Text") };
        var left = new StackPanel { Orientation = Orientation.Horizontal };
        left.Children.Add(dot);
        left.Children.Add(name);

        var grid = new Grid();
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        Grid.SetColumn(left, 0);
        grid.Children.Add(left);
        if (p.served)
        {
            var lk = new Icon { Glyph = "lock", Width = 12, Height = 12, Brush = (Brush)FindResource("TextFaint"), VerticalAlignment = VerticalAlignment.Center };
            Grid.SetColumn(lk, 1);
            grid.Children.Add(lk);
        }

        var b = new Border { Padding = new Thickness(8, 7, 8, 7), CornerRadius = new CornerRadius(7), Margin = new Thickness(0, 1, 0, 1), Cursor = Cursors.Hand, Background = Brushes.Transparent, Child = grid };
        b.MouseLeftButtonUp += (_, _) => SelectSite(p);
        b.MouseEnter += (_, _) => { if (_selected?.name != p.name) b.Background = (Brush)FindResource("BgCard"); };
        b.MouseLeave += (_, _) => { if (_selected?.name != p.name) b.Background = Brushes.Transparent; };
        _rows[p.name] = b;
        return b;
    }

    private Border FolderRow(ProjectInfo p)
    {
        var t = new TextBlock { Text = p.name, Foreground = (Brush)FindResource("TextFaint"), FontStyle = FontStyles.Italic, FontSize = 12 };
        return new Border { Padding = new Thickness(9, 6, 9, 6), CornerRadius = new CornerRadius(6), BorderBrush = (Brush)FindResource("Border"), BorderThickness = new Thickness(1), Margin = new Thickness(0, 2, 0, 0), Cursor = Cursors.Hand, Child = t };
    }

    private void SelectSite(ProjectInfo p)
    {
        _selected = p;
        foreach (var kv in _rows) kv.Value.Background = kv.Key == p.name ? (Brush)FindResource("AccentSoft") : Brushes.Transparent;
        DetailHost.Content = BuildDetail(p);
    }

    // ---- detail pane ---------------------------------------------------

    private void ShowEmpty()
    {
        DetailHost.Content = new TextBlock
        {
            Text = "Select a site",
            Foreground = (Brush)FindResource("TextFaint"),
            HorizontalAlignment = HorizontalAlignment.Center,
            VerticalAlignment = VerticalAlignment.Center,
        };
    }

    private FrameworkElement BuildDetail(ProjectInfo p)
    {
        var dock = new DockPanel { Margin = new Thickness(24, 16, 24, 20) };

        // header
        var headerGrid = new Grid { Height = 34, Margin = new Thickness(0, 0, 0, 12) };
        var titleRow = new StackPanel { Orientation = Orientation.Horizontal, VerticalAlignment = VerticalAlignment.Center };
        var dot = new Ellipse { Width = 9, Height = 9, VerticalAlignment = VerticalAlignment.Center, Margin = new Thickness(0, 0, 10, 0), Fill = (Brush)FindResource(p.running ? "Green" : "TextFaint") };
        titleRow.Children.Add(dot);
        titleRow.Children.Add(new TextBlock { Text = p.name, FontSize = 20, FontWeight = FontWeights.SemiBold, VerticalAlignment = VerticalAlignment.Center });
        titleRow.Children.Add(Chip(p.kind, false, new Thickness(10, 0, 0, 0)));
        DockPanel.SetDock(headerGrid, Dock.Top);
        headerGrid.Children.Add(titleRow);

        var actions = new StackPanel { Orientation = Orientation.Horizontal, HorizontalAlignment = HorizontalAlignment.Right, Margin = new Thickness(0, 0, 112, 0), VerticalAlignment = VerticalAlignment.Center };
        actions.Children.Add(TextButton(p.running ? "Stop" : "Start", p.running ? "Btn" : "BtnPrimary", async () => await Action(p, p.running ? "stop" : "start")));
        actions.Children.Add(TextButton("Rebuild", "Btn", async () => await Action(p, "rebuild"), new Thickness(8, 0, 0, 0)));
        actions.Children.Add(TextButton("Reset", "BtnDanger", async () => await Action(p, "reset"), new Thickness(8, 0, 0, 0)));
        headerGrid.Children.Add(actions);
        dock.Children.Add(headerGrid);

        // url row
        if (!string.IsNullOrEmpty(p.url))
        {
            var urlRow = new StackPanel { Orientation = Orientation.Horizontal, Margin = new Thickness(0, 0, 0, 14) };
            var pill = new Border { Style = (Style)FindResource("Pill"), Background = (Brush)FindResource("BgInset") };
            var link = new TextBlock { Foreground = (Brush)FindResource("Accent"), FontFamily = new FontFamily("Consolas"), FontSize = 12 };
            link.Text = p.url;
            pill.Child = link;
            urlRow.Children.Add(pill);
            urlRow.Children.Add(TextButton("Open", "Btn", () => OpenExternal(p.url!), new Thickness(8, 0, 0, 0)));
            DockPanel.SetDock(urlRow, Dock.Top);
            dock.Children.Add(urlRow);
        }

        // tabs
        var tabBar = new StackPanel { Orientation = Orientation.Horizontal, Margin = new Thickness(0, 0, 0, 2) };
        var tabHost = new ContentControl();
        foreach (var name in new[] { "Overview", "Services", "Logs", "Settings" })
        {
            var tabName = name;
            var tb = new TextBlock { Text = name, Margin = new Thickness(0, 9, 16, 9), Cursor = Cursors.Hand, FontSize = 13, FontWeight = FontWeights.Medium };
            void Paint() => tb.Foreground = (Brush)FindResource(_tab == tabName ? "Text" : "TextDim");
            Paint();
            var underline = new Border { Height = 2, Background = (Brush)FindResource("Accent"), VerticalAlignment = VerticalAlignment.Bottom, Visibility = _tab == tabName ? Visibility.Visible : Visibility.Collapsed };
            var cell = new Grid();
            cell.Children.Add(tb);
            cell.Children.Add(underline);
            cell.MouseLeftButtonUp += (_, _) => { _tab = tabName; tabHost.Content = TabContent(p); RepaintTabs(tabBar); };
            tabBar.Children.Add(cell);
        }
        var tabsBorder = new Border { BorderBrush = (Brush)FindResource("Border"), BorderThickness = new Thickness(0, 0, 0, 1), Child = tabBar };
        DockPanel.SetDock(tabsBorder, Dock.Top);
        dock.Children.Add(tabsBorder);

        tabHost.Content = TabContent(p);
        dock.Children.Add(new ScrollViewer { VerticalScrollBarVisibility = ScrollBarVisibility.Auto, Content = tabHost, Margin = new Thickness(0, 14, 0, 0) });
        return dock;
    }

    private void RepaintTabs(StackPanel bar)
    {
        foreach (Grid cell in bar.Children.OfType<Grid>())
        {
            var tb = cell.Children.OfType<TextBlock>().First();
            var ul = cell.Children.OfType<Border>().First();
            var active = tb.Text == _tab;
            tb.Foreground = (Brush)FindResource(active ? "Text" : "TextDim");
            ul.Visibility = active ? Visibility.Visible : Visibility.Collapsed;
        }
    }

    private FrameworkElement TabContent(ProjectInfo p) => _tab switch
    {
        "Services" => OverviewServices(p, true),
        "Logs" => Hint("Live logs , coming to this tab."),
        "Settings" => Hint($"PHP {p.php ?? "n/a"} · domain {p.url} · destroy , dialog pass."),
        _ => Overview(p),
    };

    private FrameworkElement Overview(ProjectInfo p)
    {
        var sp = new StackPanel();
        sp.Children.Add(new TextBlock { Text = "LINKED SERVICES", Style = (Style)FindResource("SectionLabel") });
        sp.Children.Add(OverviewServices(p, false));
        sp.Children.Add(new TextBlock { Text = "LOCATION", Style = (Style)FindResource("SectionLabel") });
        var card = new Border { Style = (Style)FindResource("Card") };
        var g = new Grid();
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        var loc = new TextBlock { Text = p.dir, FontFamily = new FontFamily("Consolas"), FontSize = 12, Foreground = (Brush)FindResource("TextDim"), VerticalAlignment = VerticalAlignment.Center, TextTrimming = TextTrimming.CharacterEllipsis };
        Grid.SetColumn(loc, 0);
        var locBtns = new StackPanel { Orientation = Orientation.Horizontal };
        locBtns.Children.Add(TextButton("Open folder", "Btn", () => OpenExternal(p.dir), default, 26, 12));
        locBtns.Children.Add(TextButton("Open in editor", "Btn", () => { }, new Thickness(6, 0, 0, 0), 26, 12));
        Grid.SetColumn(locBtns, 1);
        g.Children.Add(loc);
        g.Children.Add(locBtns);
        card.Child = g;
        sp.Children.Add(card);
        return sp;
    }

    private FrameworkElement OverviewServices(ProjectInfo p, bool sectionLabel)
    {
        var sp = new StackPanel();
        if (sectionLabel) sp.Children.Add(new TextBlock { Text = "LINKED SERVICES", Style = (Style)FindResource("SectionLabel") });
        if (p.LinkedServices.Length == 0)
        {
            sp.Children.Add(new TextBlock { Text = "No linked services.", Style = (Style)FindResource("Faint"), Margin = new Thickness(2, 2, 0, 0) });
            return sp;
        }
        foreach (var s in p.LinkedServices)
        {
            var card = new Border { Style = (Style)FindResource("Card"), Padding = new Thickness(14, 11, 14, 11), Margin = new Thickness(0, 0, 0, 8) };
            var row = new StackPanel { Orientation = Orientation.Horizontal };
            row.Children.Add(new Icon { Glyph = s.Glyph, Width = 18, Height = 18, Brush = (Brush)FindResource("TextDim"), VerticalAlignment = VerticalAlignment.Center, Margin = new Thickness(0, 0, 12, 0) });
            var texts = new StackPanel { VerticalAlignment = VerticalAlignment.Center };
            texts.Children.Add(new TextBlock { Text = s.Title, FontWeight = FontWeights.Medium });
            texts.Children.Add(new TextBlock { Text = s.key + " · " + s.ModeText, Style = (Style)FindResource("Faint"), Margin = new Thickness(0, 2, 0, 0) });
            row.Children.Add(texts);
            card.Child = row;
            sp.Children.Add(card);
        }
        return sp;
    }

    // ---- small builders -----------------------------------------------

    private Border Chip(string text, bool accent, Thickness margin)
    {
        var b = new Border { Style = (Style)FindResource("Chip"), Margin = margin };
        if (accent) { b.Background = (Brush)FindResource("AccentSoft"); b.BorderThickness = new Thickness(0); }
        b.Child = new TextBlock { Text = text, Foreground = (Brush)FindResource(accent ? "Accent" : "TextDim"), FontSize = 11 };
        return b;
    }

    private Button TextButton(string text, string style, Action onClick, Thickness margin = default, double height = 30, double fontSize = 13)
    {
        var btn = new Button { Style = (Style)FindResource(style), Content = new TextBlock { Text = text }, Margin = margin, Height = height, FontSize = fontSize };
        btn.Click += (_, _) => onClick();
        return btn;
    }

    private Button TextButton(string text, string style, Func<Task> onClick, Thickness margin = default)
    {
        var btn = new Button { Style = (Style)FindResource(style), Content = new TextBlock { Text = text }, Margin = margin, Height = 30 };
        btn.Click += async (_, _) => await onClick();
        return btn;
    }

    private FrameworkElement Hint(string text) =>
        new TextBlock { Text = text, Style = (Style)FindResource("Faint"), Margin = new Thickness(2, 4, 0, 0) };

    // ---- actions -------------------------------------------------------

    private async Task Action(ProjectInfo p, string action)
    {
        if (_client is null) return;
        try { await _client.ProjectActionAsync(p.name, action); await RefreshAsync(); }
        catch { /* ignore */ }
    }

    private static void OpenExternal(string target)
    {
        try { Process.Start(new ProcessStartInfo(target) { UseShellExecute = true }); } catch { /* ignore */ }
    }

    private void OnNew(object sender, RoutedEventArgs e) { }
    private void OnGroup(object sender, RoutedEventArgs e) { }
    private void OnAdopt(object sender, RoutedEventArgs e) { }

    private static bool Under(string dir, string root)
    {
        static string N(string p) => p.Replace('\\', '/').TrimEnd('/').ToLowerInvariant();
        var d = N(dir);
        var r = N(root);
        return d == r || d.StartsWith(r + "/");
    }
}
