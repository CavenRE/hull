using System.Diagnostics;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;

namespace Hull.Gui;

/// <summary>
/// Cluster routes editor + console (built in code-behind): lists adopted
/// clusters, every URL they serve (or would serve), and lets you assign or
/// remove routes and set the base domain / ingress mode. All state lives in the
/// daemon; this only calls the /v1/clusters endpoints.
/// </summary>
public sealed class ClustersView : UserControl, IRefreshable
{
    private readonly HullClient? _client;
    private readonly StackPanel _list;

    public ClustersView(HullClient? client)
    {
        _client = client;
        _list = new StackPanel { MaxWidth = 880, HorizontalAlignment = HorizontalAlignment.Left, Margin = new Thickness(28) };
        Content = new ScrollViewer { VerticalScrollBarVisibility = ScrollBarVisibility.Auto, Content = _list };
        Loaded += async (_, _) => await RefreshAsync();
    }

    public async Task RefreshAsync()
    {
        _list.Children.Clear();
        _list.Children.Add(Header("Clusters", "Adopted multi-container stacks and the URLs they serve."));
        if (_client is null) { _list.Children.Add(Muted("Daemon not running.")); return; }

        List<ClusterInfo> clusters;
        try { clusters = await _client.ClustersAsync(); }
        catch (Exception ex) { _list.Children.Add(Muted(ex.Message)); return; }

        if (clusters.Count == 0)
        {
            _list.Children.Add(Muted("No clusters yet. Adopt one with:  hull cluster add <dir> --root <subdir>"));
            return;
        }
        foreach (var c in clusters) _list.Children.Add(ClusterCard(c));
    }

    // ---- card ----------------------------------------------------------

    private Border ClusterCard(ClusterInfo c)
    {
        var body = new StackPanel();

        // Title row: name + state + ingress.
        var title = new Grid { Margin = new Thickness(0, 0, 0, 4) };
        title.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        title.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        var name = new TextBlock { Text = c.name, FontSize = 17, FontWeight = FontWeights.SemiBold, Foreground = Brush("Text") };
        Grid.SetColumn(name, 0);
        title.Children.Add(name);
        var state = Chip(c.State, c.running ? "Green" : "TextFaint");
        Grid.SetColumn(state, 1);
        title.Children.Add(state);
        body.Children.Add(title);

        body.Children.Add(new TextBlock
        {
            Text = $"base domain: {c.BaseDomainText}      ingress: {c.IngressText}",
            Foreground = Brush("TextDim"),
            FontSize = 12,
            Margin = new Thickness(0, 0, 0, 12),
        });

        // Console: every served URL.
        var urls = c.AllUrls.ToList();
        if (urls.Count > 0)
        {
            body.Children.Add(SectionLabel("URLs"));
            foreach (var u in urls) body.Children.Add(UrlRow(u));
        }

        // Routes with a remove button.
        body.Children.Add(SectionLabel("Routes"));
        if (c.RouteList.Length == 0)
            body.Children.Add(Muted("No routes. Add one below."));
        foreach (var rt in c.RouteList) body.Children.Add(RouteRow(c, rt));

        // Actions.
        var actions = new StackPanel { Orientation = Orientation.Horizontal, Margin = new Thickness(0, 12, 0, 0) };
        actions.Children.Add(TextButton("+ Add route", (_, _) => ShowAddRoute(c)));
        actions.Children.Add(TextButton("Ingress…", (_, _) => ShowIngress(c)));
        body.Children.Add(actions);

        return Card(body);
    }

    private Border UrlRow(string url)
    {
        var g = new Grid { Margin = new Thickness(0, 2, 0, 2) };
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        var link = new TextBlock { Text = url, Foreground = Brush("Accent"), FontFamily = Mono(), FontSize = 12, VerticalAlignment = VerticalAlignment.Center };
        Grid.SetColumn(link, 0);
        g.Children.Add(link);
        var open = TextButton("Open", (_, _) => OpenUrl(url));
        var copy = TextButton("Copy", (_, _) => { try { Clipboard.SetText(url); MainWindow.Current?.Toast("Copied"); } catch { } });
        var right = new StackPanel { Orientation = Orientation.Horizontal };
        right.Children.Add(open);
        right.Children.Add(copy);
        Grid.SetColumn(right, 1);
        g.Children.Add(right);
        return new Border { Child = g };
    }

    private Border RouteRow(ClusterInfo c, ClusterRouteInfo rt)
    {
        var g = new Grid { Margin = new Thickness(0, 2, 0, 2) };
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        var alias = string.IsNullOrEmpty(rt.AliasText) ? "" : $"  (+{rt.AliasText})";
        var text = new TextBlock
        {
            Text = $"{rt.subdomain}{alias}  ->  {rt.service}:{rt.port}   {rt.ServedText}",
            Foreground = Brush("TextDim"),
            FontFamily = Mono(),
            FontSize = 12,
            VerticalAlignment = VerticalAlignment.Center,
        };
        Grid.SetColumn(text, 0);
        g.Children.Add(text);
        var rm = TextButton("Remove", (_, _) =>
            MainWindow.Current?.Run(() => _client!.RemoveClusterRouteAsync(c.name, rt.subdomain), $"Removed {rt.subdomain}"));
        Grid.SetColumn(rm, 1);
        g.Children.Add(rm);
        return new Border { Child = g };
    }

    // ---- dialogs -------------------------------------------------------

    private void ShowAddRoute(ClusterInfo c)
    {
        var panel = new StackPanel { Width = 380 };
        panel.Children.Add(DialogTitle($"Add a URL to {c.name}"));
        var sub = Field(panel, "Subdomain (e.g. api)", "");
        var svc = Field(panel, "Service (compose service name, e.g. management_api)", "");
        var port = Field(panel, "Port (e.g. 8081)", "");
        var alias = Field(panel, "Aliases (comma-separated, optional)", "");

        var save = new Button { Style = StyleRes("Btn"), Content = "Assign", HorizontalAlignment = HorizontalAlignment.Right, Margin = new Thickness(0, 14, 0, 0) };
        save.Click += (_, _) =>
        {
            if (!int.TryParse(port.Text.Trim(), out var p) || p <= 0) { MainWindow.Current?.Toast("Port must be a number"); return; }
            var aliases = alias.Text.Split(',', StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries);
            var body = new { service = svc.Text.Trim(), port = p, aliases };
            MainWindow.Current?.CloseDialog();
            MainWindow.Current?.Run(() => _client!.SetClusterRouteAsync(c.name, sub.Text.Trim(), body), $"Route {sub.Text.Trim()} assigned");
        };
        panel.Children.Add(save);
        MainWindow.Current?.ShowDialog(CardWrap(panel));
    }

    private void ShowIngress(ClusterInfo c)
    {
        var panel = new StackPanel { Width = 380 };
        panel.Children.Add(DialogTitle($"Ingress for {c.name}"));

        var baseDomain = Field(panel, "Base domain (blank = the TLD)", c.base_domain ?? "");
        panel.Children.Add(new TextBlock { Text = "Ingress mode", Foreground = Brush("TextDim"), FontSize = 12, Margin = new Thickness(0, 10, 0, 4) });
        var combo = new ComboBox { Margin = new Thickness(0, 0, 0, 4) };
        foreach (var m in new[] { "none", "delegate", "hull" }) combo.Items.Add(m);
        combo.SelectedItem = string.IsNullOrEmpty(c.ingress) ? "none" : c.ingress;
        panel.Children.Add(combo);

        var save = new Button { Style = StyleRes("Btn"), Content = "Save", HorizontalAlignment = HorizontalAlignment.Right, Margin = new Thickness(0, 14, 0, 0) };
        save.Click += (_, _) =>
        {
            var body = new { base_domain = baseDomain.Text.Trim(), ingress = (combo.SelectedItem as string) ?? "none" };
            MainWindow.Current?.CloseDialog();
            MainWindow.Current?.Run(() => _client!.SetClusterConfigAsync(c.name, body), $"{c.name} updated");
        };
        panel.Children.Add(save);
        MainWindow.Current?.ShowDialog(CardWrap(panel));
    }

    // ---- small UI helpers ---------------------------------------------

    private FrameworkElement Header(string title, string sub)
    {
        var sp = new StackPanel { Margin = new Thickness(0, 0, 0, 18) };
        sp.Children.Add(new TextBlock { Text = title, FontSize = 24, FontWeight = FontWeights.Bold, Foreground = Brush("Text") });
        sp.Children.Add(new TextBlock { Text = sub, FontSize = 13, Foreground = Brush("TextDim"), Margin = new Thickness(0, 3, 0, 0) });
        return sp;
    }

    private TextBlock SectionLabel(string t) => new()
    {
        Text = t.ToUpperInvariant(),
        FontSize = 10,
        FontWeight = FontWeights.SemiBold,
        Foreground = Brush("TextFaint"),
        Margin = new Thickness(0, 10, 0, 4),
    };

    private TextBlock Muted(string t) => new() { Text = t, Foreground = Brush("TextDim"), FontSize = 13, Margin = new Thickness(0, 4, 0, 4), TextWrapping = TextWrapping.Wrap };

    private Border Chip(string text, string brush)
    {
        return new Border
        {
            Background = Brush("BgCard"),
            CornerRadius = new CornerRadius(6),
            Padding = new Thickness(8, 2, 8, 2),
            VerticalAlignment = VerticalAlignment.Center,
            Child = new TextBlock { Text = text, Foreground = Brush(brush), FontSize = 11 },
        };
    }

    private Border Card(UIElement child) => new()
    {
        Style = StyleRes("Card"),
        Padding = new Thickness(20),
        Margin = new Thickness(0, 0, 0, 14),
        Child = child,
    };

    private Border CardWrap(UIElement child) => new()
    {
        Style = StyleRes("Card"),
        Padding = new Thickness(20),
        Child = child,
    };

    private TextBlock DialogTitle(string t) => new() { Text = t, FontSize = 16, FontWeight = FontWeights.SemiBold, Foreground = Brush("Text"), Margin = new Thickness(0, 0, 0, 12) };

    private TextBox Field(StackPanel parent, string label, string initial)
    {
        parent.Children.Add(new TextBlock { Text = label, Foreground = Brush("TextDim"), FontSize = 12, Margin = new Thickness(0, 8, 0, 4) });
        var tb = new TextBox { Text = initial, Padding = new Thickness(6, 4, 6, 4) };
        parent.Children.Add(tb);
        return tb;
    }

    private Button TextButton(string text, RoutedEventHandler onClick)
    {
        var b = new Button
        {
            Content = text,
            Background = Brushes.Transparent,
            BorderThickness = new Thickness(0),
            Foreground = Brush("Accent"),
            FontSize = 12,
            Cursor = System.Windows.Input.Cursors.Hand,
            Padding = new Thickness(8, 2, 8, 2),
        };
        b.Click += onClick;
        return b;
    }

    private void OpenUrl(string url)
    {
        try { Process.Start(new ProcessStartInfo(url) { UseShellExecute = true }); }
        catch { MainWindow.Current?.Toast("Could not open " + url); }
    }

    private Brush Brush(string key) => (Brush)FindResource(key);
    private System.Windows.Style StyleRes(string key) => (System.Windows.Style)FindResource(key);
    private FontFamily Mono() => (FontFamily)FindResource("FontMono");
}
