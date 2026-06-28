using System.Windows;
using System.Windows.Controls;
using System.Windows.Documents;
using System.Windows.Input;
using System.Windows.Media;

namespace Hull.Gui;

public partial class ServicesView : UserControl, IRefreshable
{
    private readonly HullClient? _client;

    public ServicesView(HullClient? client)
    {
        InitializeComponent();
        _client = client;
        Loaded += async (_, _) =>
        {
            await RefreshAsync();
            if (Environment.GetEnvironmentVariable("HULL_AUTO_DIALOG") == "add") OnAdd(this, new RoutedEventArgs());
        };
    }

    public async Task RefreshAsync()
    {
        if (_client is null) return;
        try
        {
            var services = await _client.ServicesAsync();
            List.ItemsSource = services;
            EmptyMsg.Visibility = services.Count == 0 ? Visibility.Visible : Visibility.Collapsed;
            Sub.Text = $"{services.Count(s => s.running)} of {services.Count} running";
        }
        catch (Exception ex) { Ui.Toast(ex.Message); }
    }

    private static ServiceInfo? Ctx(object sender) => (sender as FrameworkElement)?.DataContext as ServiceInfo;

    private async void OnStart(object sender, RoutedEventArgs e) { if (Ctx(sender) is { } s) await Act(s, "start"); }
    private async void OnStop(object sender, RoutedEventArgs e) { if (Ctx(sender) is { } s) await Act(s, "stop"); }
    private async void OnRestart(object sender, RoutedEventArgs e) { if (Ctx(sender) is { } s) await Act(s, "restart"); }

    private async Task Act(ServiceInfo s, string action)
    {
        if (_client is null) return;
        await Ui.Run(() => _client.ServiceActionAsync(s.name, action), "");
        await RefreshAsync();
    }

    private void OnCopy(object sender, RoutedEventArgs e)
    {
        if ((sender as Button)?.Tag is string s && !string.IsNullOrEmpty(s)) Ui.Copy(s);
    }

    private void OnOpenUrl(object sender, RoutedEventArgs e)
    {
        if (Ctx(sender) is { } s && !string.IsNullOrEmpty(s.url)) Ui.OpenExternal(s.url!);
    }

    // ---- Add instance ------------------------------------------------
    private void OnAdd(object sender, RoutedEventArgs e)
    {
        string? selectedEngine = null;
        var versionBoxes = new Dictionary<string, TextBox>();
        var rows = new Dictionary<string, Border>();
        Button? addBtnRef = null;

        var list = new StackPanel();
        foreach (var g in Catalog.Groups)
        {
            list.Children.Add(new TextBlock { Text = g.Category.ToUpperInvariant(), Foreground = Ui.B("TextDim"), FontSize = 11, FontWeight = FontWeights.SemiBold, Margin = new Thickness(2, 12, 0, 6) });
            foreach (var it in g.Items)
            {
                string engine = it.Engine;
                var grid = new Grid();
                grid.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
                grid.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
                grid.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
                var ic = Ui.Glyph(Catalog.IconFor(engine), 16, "TextDim"); ic.Margin = new Thickness(0, 0, 12, 0);
                var mid = new StackPanel { VerticalAlignment = VerticalAlignment.Center };
                mid.Children.Add(new TextBlock { Text = Catalog.Label(engine), FontWeight = FontWeights.Medium });
                mid.Children.Add(new TextBlock { Text = it.Blurb, Style = Ui.S("Faint") });
                var ver = Ui.VersionBox(it.Versions, it.Versions[0], 120);
                versionBoxes[engine] = ver;
                Grid.SetColumn(ic, 0); Grid.SetColumn(mid, 1); Grid.SetColumn(ver, 2);
                grid.Children.Add(ic); grid.Children.Add(mid); grid.Children.Add(ver);
                var card = new Border { CornerRadius = new CornerRadius(9), BorderThickness = new Thickness(1), BorderBrush = Ui.B("Border"), Background = Ui.B("BgCard"), Padding = new Thickness(13, 11, 13, 11), Margin = new Thickness(0, 0, 0, 8), Cursor = Cursors.Hand, Child = grid };
                rows[engine] = card;
                card.MouseLeftButtonUp += (_, _) =>
                {
                    selectedEngine = engine;
                    if (addBtnRef is not null) addBtnRef.IsEnabled = true;
                    foreach (var (eng, c) in rows)
                    {
                        var on = eng == engine;
                        c.BorderBrush = Ui.B(on ? "Accent" : "Border");
                        c.BorderThickness = new Thickness(on ? 2 : 1);
                    }
                };
                list.Children.Add(card);
            }
        }

        var scroll = new ScrollViewer { VerticalScrollBarVisibility = ScrollBarVisibility.Auto, MaxHeight = 420, Content = list };
        var add = Ui.TextButton("Add instance", "", "BtnPrimary", async (_, _) =>
        {
            if (selectedEngine is null) { Ui.Toast("Pick an engine"); return; }
            var version = versionBoxes[selectedEngine].Text.Trim();
            Ui.CloseDialog();
            if (_client is not null)
                await Ui.RunJob(() => _client.PostForJobAsync("/v1/services", new { engine = selectedEngine, version }), $"Adding {Catalog.Label(selectedEngine)}…");
        });
        add.IsEnabled = false; // enabled once an engine is selected
        addBtnRef = add;
        Ui.ShowDialog(Ui.Dialog("Add instance", scroll, Ui.CancelButton(), add));
    }

    // ---- Open with… --------------------------------------------------
    private void OnOpenWith(object sender, RoutedEventArgs e)
    {
        if (Ctx(sender) is not { } s) return;
        var tld = Ui.Main?.Tld ?? "test";
        var body = new StackPanel { MinWidth = 360 };
        void Opt(string label, string glyph, Action act)
        {
            var b = Ui.TextButton(label, glyph, "Btn", (_, _) => { Ui.CloseDialog(); act(); });
            b.HorizontalAlignment = HorizontalAlignment.Stretch; b.HorizontalContentAlignment = HorizontalAlignment.Left;
            b.Margin = new Thickness(0, 0, 0, 8); b.Height = 38;
            body.Children.Add(b);
        }
        Opt("TablePlus", "database", () => Ui.OpenExternal(ConnUri(s, "tableplus")));
        Opt("DBeaver", "database", () => Ui.OpenExternal(ConnUri(s, "dbeaver")));
        Opt("Adminer (web)", "external", () => Ui.OpenExternal($"https://db.{tld}"));
        Opt("CLI — copy command", "copy", () => Ui.Copy(CliCommand(s)));
        Ui.ShowDialog(Ui.Dialog($"Open {s.name} with…", body, Ui.CancelButton("Close")));
    }

    private static string ConnUri(ServiceInfo s, string scheme)
    {
        var driver = s.engine switch { "postgres" => "postgresql", "mysql" => "mysql", "mariadb" => "mysql", _ => s.engine };
        var user = string.IsNullOrEmpty(s.username) ? "root" : s.username;
        return $"{scheme}://{driver}://{user}@{s.HostText}:{s.host_port}";
    }
    private static string CliCommand(ServiceInfo s) => s.engine switch
    {
        "postgres" => $"psql -h {s.HostText} -p {s.host_port} -U {(string.IsNullOrEmpty(s.username) ? "postgres" : s.username)}",
        "mysql" or "mariadb" => $"mysql -h {s.HostText} -P {s.host_port} -u {(string.IsNullOrEmpty(s.username) ? "root" : s.username)}",
        _ => $"{s.HostText}:{s.host_port}",
    };

    // ---- Link a project ----------------------------------------------
    private async void OnLink(object sender, RoutedEventArgs e)
    {
        if (Ctx(sender) is not { } s || _client is null) return;
        List<ProjectInfo> projects;
        try { projects = await _client.ProjectsAsync(); } catch (Exception ex) { Ui.Toast(ex.Message); return; }
        var linked = new HashSet<string>(s.linked_projects ?? Array.Empty<string>());
        var candidates = projects.Where(p => p.kind != "folder" && !linked.Contains(p.name)).ToList();
        var body = new StackPanel { MinWidth = 380 };
        if (candidates.Count == 0)
            body.Children.Add(new TextBlock { Text = "No unlinked projects in this folder.", Style = Ui.S("Muted") });
        foreach (var p in candidates)
        {
            var row = new Grid();
            row.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
            row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
            var ic = Ui.Glyph(p.kind == "cluster" ? "cube" : "sites", 15, "TextDim"); ic.Margin = new Thickness(0, 0, 10, 0);
            var name = new TextBlock { Text = p.name, VerticalAlignment = VerticalAlignment.Center };
            Grid.SetColumn(ic, 0); Grid.SetColumn(name, 1);
            row.Children.Add(ic); row.Children.Add(name);
            var card = new Border { CornerRadius = new CornerRadius(8), Padding = new Thickness(12, 9, 12, 9), Margin = new Thickness(0, 0, 0, 6), Cursor = Cursors.Hand, Background = Ui.B("BgCard"), BorderBrush = Ui.B("Border"), BorderThickness = new Thickness(1), Child = row };
            card.MouseLeftButtonUp += async (_, _) =>
            {
                Ui.CloseDialog();
                await Ui.RunJob(() => _client.PostForJobAsync($"/v1/services/{Uri.EscapeDataString(s.name)}/link", new { project = p.name }), $"Linking {s.name} to {p.name}…");
                await RefreshAsync();
            };
            body.Children.Add(card);
        }
        Ui.ShowDialog(Ui.Dialog($"Link a project to {s.name}", body, Ui.CancelButton("Done")));
    }

    // ---- Destroy -----------------------------------------------------
    private void OnDestroy(object sender, RoutedEventArgs e)
    {
        if (Ctx(sender) is not { } s) return;
        var body = new StackPanel { MinWidth = 400 };
        body.Children.Add(new TextBlock { Text = $"This stops and removes the {s.name} instance and its data volume. Projects linked to it will lose their connection.", Style = Ui.S("Muted"), TextWrapping = TextWrapping.Wrap, Margin = new Thickness(0, 0, 0, 12) });
        var hint = new TextBlock { Style = Ui.S("Faint"), Margin = new Thickness(0, 0, 0, 6) };
        hint.Inlines.Add(new Run("Type "));
        hint.Inlines.Add(new Run(s.name) { Foreground = Ui.B("Accent"), FontWeight = FontWeights.SemiBold });
        hint.Inlines.Add(new Run(" to confirm."));
        body.Children.Add(hint);
        var input = new TextBox { Style = Ui.S("MonoInput") };
        body.Children.Add(input);
        var destroy = Ui.TextButton("Destroy", "trash", "BtnDanger", async (_, _) =>
        {
            if (input.Text != s.name) { Ui.Toast("Name doesn't match"); return; }
            Ui.CloseDialog();
            if (_client is not null)
                await Ui.RunJob(() => _client.DeleteForJobAsync($"/v1/services/{Uri.EscapeDataString(s.name)}"), $"Destroying {s.name}…");
            await RefreshAsync();
        });
        Ui.ShowDialog(Ui.Dialog($"Destroy {s.name}?", body, Ui.CancelButton(), destroy));
    }
}
