using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Media;
using System.Windows.Threading;

namespace Hull.Gui;

public partial class NewProjectDialog : UserControl
{
    private readonly HullClient? _client;
    private readonly Action? _onDone;

    private string _base = "";
    private TextBox _name = null!;
    private TextBlock _preview = null!;
    private ComboBox _type = null!;
    private ComboBox _php = null!;
    private CheckBox _serve = null!;
    private TextBlock _domain = null!;
    private StackPanel _phpCol = null!, _serveRow = null!, _servicesSection = null!, _containersSection = null!, _clusterOpts = null!;
    private StackPanel _serviceRows = null!, _cardsHost = null!;
    private TextBox _customPath = null!;
    private StackPanel _customRow = null!;
    private CheckBox _managed = null!;
    private TextBox _subroot = null!;

    private static readonly Dictionary<string, int> DefaultPorts = new()
    {
        ["node"] = 3000, ["python"] = 8000, ["php"] = 9000, ["nginx"] = 80, ["httpd"] = 80, ["caddy"] = 80,
        ["redis"] = 6379, ["postgres"] = 5432, ["mysql"] = 3306, ["mariadb"] = 3306, ["mongo"] = 27017,
        ["golang"] = 8080, ["ruby"] = 3000, ["rabbitmq"] = 5672, ["meilisearch"] = 7700, ["minio"] = 9000,
    };

    public NewProjectDialog(HullClient? client, Action? onDone)
    {
        InitializeComponent();
        _client = client;
        _onDone = onDone;
        Build();
        Loaded += (_, _) => _name.Focus();
    }

    private void Build()
    {
        // Location
        Body.Children.Add(Ui.FieldLabel("Location"));
        var roots = Ui.Main?.Config?.roots ?? Array.Empty<string>();
        _base = roots.Length > 0 ? roots[0] : "C:/";
        var seg = new StackPanel { Orientation = Orientation.Horizontal };
        var cells = new List<Border>();
        var labels = roots.Select(LastSeg).Append("Custom…").ToArray();
        var paths = roots.Append("__custom").ToArray();
        for (int i = 0; i < labels.Length; i++)
        {
            int idx = i;
            var t = new TextBlock { Text = labels[i], FontSize = 12.5, FontWeight = FontWeights.Medium, Padding = new Thickness(13, 5, 13, 5) };
            var b = new Border { CornerRadius = new CornerRadius(6), Cursor = Cursors.Hand, Child = t };
            b.MouseLeftButtonUp += (_, _) =>
            {
                for (int j = 0; j < cells.Count; j++) { var on = j == idx; cells[j].Background = on ? Ui.B("Accent") : Brushes.Transparent; ((TextBlock)cells[j].Child).Foreground = on ? Ui.B("TextOnAccent") : Ui.B("TextDim"); }
                if (paths[idx] == "__custom") { _customRow.Visibility = Visibility.Visible; _base = _customPath.Text.Length > 0 ? _customPath.Text : "C:/"; }
                else { _customRow.Visibility = Visibility.Collapsed; _base = paths[idx]; }
                UpdatePreview();
            };
            cells.Add(b); seg.Children.Add(b);
        }
        Body.Children.Add(new Border { Background = Ui.B("BgInset"), BorderBrush = Ui.B("CtrlBorder"), BorderThickness = new Thickness(1), CornerRadius = new CornerRadius(8), Padding = new Thickness(3), HorizontalAlignment = HorizontalAlignment.Left, Child = seg, Margin = new Thickness(0, 0, 0, 8) });
        _customPath = new TextBox { Style = Ui.S("MonoInput") };
        var browse = Ui.TextButton("Browse", "folder", "Btn", (_, _) => { var p = Ui.PickFolder("Choose a base folder"); if (p != null) { _customPath.Text = p; _base = p; UpdatePreview(); } });
        browse.Margin = new Thickness(8, 0, 0, 0);
        _customRow = new StackPanel { Orientation = Orientation.Horizontal, Margin = new Thickness(0, 0, 0, 14), Visibility = Visibility.Collapsed };
        _customPath.Width = 360; _customPath.TextChanged += (_, _) => { _base = _customPath.Text; UpdatePreview(); };
        _customRow.Children.Add(_customPath); _customRow.Children.Add(browse);
        Body.Children.Add(_customRow);
        // paint first cell active
        if (cells.Count > 0) { cells[0].Background = Ui.B("Accent"); ((TextBlock)cells[0].Child).Foreground = Ui.B("TextOnAccent"); }

        // Name + preview
        Body.Children.Add(Ui.FieldLabel("Name"));
        _name = new TextBox { Style = Ui.S("MonoInput") };
        _name.TextChanged += (_, _) => UpdatePreview();
        Body.Children.Add(_name);
        _preview = new TextBlock { Style = Ui.S("Faint"), Margin = new Thickness(0, 6, 0, 14) };
        Body.Children.Add(_preview);

        // Type + PHP
        var grid = new Grid { Margin = new Thickness(0, 0, 0, 14) };
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        var typeCol = new StackPanel { Margin = new Thickness(0, 0, 8, 0) };
        typeCol.Children.Add(Ui.FieldLabel("Type"));
        _type = Ui.Select(new[] { ("Laravel", "Laravel"), ("WordPress", "WordPress"), ("Plain PHP", "Plain PHP"), ("App", "App"), ("Cluster", "Cluster") }, "Laravel");
        _type.SelectionChanged += (_, _) => SyncType();
        typeCol.Children.Add(_type);
        _phpCol = new StackPanel { Margin = new Thickness(8, 0, 0, 0) };
        _phpCol.Children.Add(Ui.FieldLabel("PHP version"));
        _php = Ui.VersionBox(new[] { "8.4", "8.3", "8.2", "8.1" }, "8.4", double.NaN);
        _phpCol.Children.Add(_php);
        Grid.SetColumn(typeCol, 0); Grid.SetColumn(_phpCol, 1);
        grid.Children.Add(typeCol); grid.Children.Add(_phpCol);
        Body.Children.Add(grid);

        // Serve toggle + domain preview
        _serveRow = new StackPanel { Orientation = Orientation.Horizontal, Margin = new Thickness(0, 0, 0, 14) };
        _serve = Ui.Toggle(true, _ => UpdatePreview());
        _serveRow.Children.Add(_serve);
        _serveRow.Children.Add(new TextBlock { Text = "Serve a domain", VerticalAlignment = VerticalAlignment.Center, Margin = new Thickness(10, 0, 14, 0) });
        _domain = new TextBlock { Style = Ui.S("Faint"), VerticalAlignment = VerticalAlignment.Center };
        _serveRow.Children.Add(_domain);
        Body.Children.Add(_serveRow);

        // Containers section (App/Cluster)
        _containersSection = new StackPanel { Margin = new Thickness(0, 0, 0, 6), Visibility = Visibility.Collapsed };
        _containersSection.Children.Add(Ui.FieldLabel("Containers"));
        _containersSection.Children.Add(new TextBlock { Text = "Each container is a Docker Hub image — search, pick, repeat.", Style = Ui.S("Help"), Margin = new Thickness(0, 0, 0, 8) });
        _cardsHost = new StackPanel();
        _containersSection.Children.Add(_cardsHost);
        var addCard = Ui.TextButton("Add container", "plus", "BtnSm", (_, _) => _cardsHost.Children.Add(ContainerCard()));
        addCard.HorizontalAlignment = HorizontalAlignment.Left; addCard.Margin = new Thickness(0, 4, 0, 0);
        _containersSection.Children.Add(addCard);
        Body.Children.Add(_containersSection);

        // Cluster options
        _clusterOpts = new StackPanel { Margin = new Thickness(0, 10, 0, 6), Visibility = Visibility.Collapsed };
        var mrow = new StackPanel { Orientation = Orientation.Horizontal };
        _managed = Ui.Toggle(true, _ => { });
        mrow.Children.Add(_managed);
        mrow.Children.Add(new TextBlock { Text = "Hull-managed (generates & owns the compose file)", VerticalAlignment = VerticalAlignment.Center, Margin = new Thickness(10, 0, 0, 0), Foreground = Ui.B("TextDim") });
        _clusterOpts.Children.Add(mrow);
        var srow = new StackPanel { Orientation = Orientation.Horizontal, Margin = new Thickness(0, 10, 0, 0) };
        srow.Children.Add(new TextBlock { Text = "Compose sub-folder", VerticalAlignment = VerticalAlignment.Center, Foreground = Ui.B("TextDim"), Margin = new Thickness(0, 0, 10, 0) });
        _subroot = new TextBox { Style = Ui.S("MonoInput"), Width = 160 };
        srow.Children.Add(_subroot);
        _clusterOpts.Children.Add(srow);
        Body.Children.Add(_clusterOpts);

        // Services to provision (PHP types)
        _servicesSection = new StackPanel();
        _servicesSection.Children.Add(Ui.FieldLabel("Services to provision"));
        _servicesSection.Children.Add(new TextBlock { Text = "Databases, caches and tools to create and link from the start.", Style = Ui.S("Help"), Margin = new Thickness(0, 0, 0, 8) });
        _serviceRows = new StackPanel();
        _servicesSection.Children.Add(_serviceRows);
        var addSvc = Ui.TextButton("Add service", "plus", "BtnSm", (_, _) => _serviceRows.Children.Add(ServiceRow()));
        addSvc.HorizontalAlignment = HorizontalAlignment.Left; addSvc.Margin = new Thickness(0, 4, 0, 0);
        _servicesSection.Children.Add(addSvc);
        Body.Children.Add(_servicesSection);

        UpdatePreview();
        SyncType();
    }

    private void SyncType()
    {
        var type = Ui.SelectedVal(_type);
        bool isApp = type == "App", isCluster = type == "Cluster", isContainers = isApp || isCluster;
        _phpCol.Visibility = isContainers ? Visibility.Hidden : Visibility.Visible;
        _serveRow.Visibility = isCluster ? Visibility.Collapsed : Visibility.Visible;
        _containersSection.Visibility = isContainers ? Visibility.Visible : Visibility.Collapsed;
        _clusterOpts.Visibility = isCluster ? Visibility.Visible : Visibility.Collapsed;
        _servicesSection.Visibility = isContainers ? Visibility.Collapsed : Visibility.Visible;
        if (isContainers && _cardsHost.Children.Count == 0) _cardsHost.Children.Add(ContainerCard());
        CreateBtn.Content = isCluster ? "Create cluster" : "Create project";
        UpdatePreview();
    }

    private void UpdatePreview()
    {
        var slug = Slug(_name.Text);
        _preview.Inlines.Clear();
        _preview.Inlines.Add(new System.Windows.Documents.Run((_base?.TrimEnd('/', '\\') ?? "") + "/"));
        _preview.Inlines.Add(new System.Windows.Documents.Run(slug.Length > 0 ? slug : "…") { Foreground = Ui.B("Text"), FontWeight = FontWeights.SemiBold });
        if (_domain is not null)
        {
            var tld = Ui.Main?.Tld ?? "test";
            _domain.Text = (_serve?.IsChecked == true) ? $"→ https://{(slug.Length > 0 ? slug : "…")}.{tld}" : "headless — no domain";
        }
    }

    // ---- service repeater row ----------------------------------------
    private FrameworkElement ServiceRow()
    {
        var row = new Grid { Margin = new Thickness(0, 0, 0, 6) };
        row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(130) });
        row.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        var engines = new List<(string, string)> { ("sqlite", "SQLite") };
        foreach (var (k, m) in Catalog.Engines) if (k != "sqlite") engines.Add((k, m.Label));
        var eng = Ui.Select(engines, "postgres"); eng.Margin = new Thickness(0, 0, 8, 0);
        var ver = Ui.VersionBox(VersionsFor("postgres"), null, 122); ver.Margin = new Thickness(0, 0, 8, 0);
        eng.SelectionChanged += (_, _) => { ver.Items.Clear(); foreach (var v in VersionsFor(Ui.SelectedVal(eng))) ver.Items.Add(v); if (ver.Items.Count > 0) ver.SelectedIndex = 0; };
        var rm = Ui.IconButton("x", (_, _) => _serviceRows.Children.Remove(row), 15, "TextFaint", "Remove");
        Grid.SetColumn(eng, 0); Grid.SetColumn(ver, 1); Grid.SetColumn(rm, 2);
        row.Children.Add(eng); row.Children.Add(ver); row.Children.Add(rm);
        row.Tag = (eng, ver);
        return row;
    }

    private static string[] VersionsFor(string engine)
    {
        foreach (var g in Catalog.Groups) foreach (var it in g.Items) if (it.Engine == engine) return it.Versions;
        return new[] { "latest" };
    }

    // ---- container card (App/Cluster) --------------------------------
    private FrameworkElement ContainerCard()
    {
        var card = new Border { Background = Ui.B("BgCard"), BorderBrush = Ui.B("Border"), BorderThickness = new Thickness(1), CornerRadius = new CornerRadius(9), Padding = new Thickness(12), Margin = new Thickness(0, 0, 0, 8) };
        var sp = new StackPanel();
        var head = new Grid();
        head.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        head.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        var cname = new TextBox { Style = Ui.S("Input") }; cname.Tag = "cname";
        var rm = Ui.IconButton("x", (_, _) => _cardsHost.Children.Remove(card), 15, "TextFaint", "Remove");
        Grid.SetColumn(cname, 0); Grid.SetColumn(rm, 1);
        head.Children.Add(cname); head.Children.Add(rm);
        sp.Children.Add(head);
        // image search
        var search = new TextBox { Style = Ui.S("Input"), Margin = new Thickness(0, 8, 0, 0) };
        sp.Children.Add(search);
        var results = new StackPanel { Margin = new Thickness(0, 6, 0, 0) };
        sp.Children.Add(results);
        // selection row
        var selRow = new StackPanel { Orientation = Orientation.Horizontal, Margin = new Thickness(0, 8, 0, 0), Visibility = Visibility.Collapsed };
        var chosenChip = new Border { Style = Ui.S("ChipAccent"), Child = new TextBlock { FontSize = 11, Foreground = Ui.B("Accent") } };
        var ver = new TextBox { Style = Ui.S("MonoInput"), Width = 110, Margin = new Thickness(8, 0, 0, 0) }; ver.Tag = "ver";
        var port = new TextBox { Style = Ui.S("MonoInput"), Width = 70, Margin = new Thickness(8, 0, 0, 0) }; port.Tag = "port";
        var serveTog = Ui.Toggle(true, _ => { });
        selRow.Children.Add(chosenChip);
        selRow.Children.Add(new TextBlock { Text = "ver", VerticalAlignment = VerticalAlignment.Center, Margin = new Thickness(8, 0, 0, 0), Foreground = Ui.B("TextFaint"), FontSize = 11 });
        selRow.Children.Add(ver);
        selRow.Children.Add(new TextBlock { Text = "port", VerticalAlignment = VerticalAlignment.Center, Margin = new Thickness(8, 0, 0, 0), Foreground = Ui.B("TextFaint"), FontSize = 11 });
        selRow.Children.Add(port);
        selRow.Children.Add(new TextBlock { Text = "serve", VerticalAlignment = VerticalAlignment.Center, Margin = new Thickness(10, 0, 6, 0), Foreground = Ui.B("TextFaint"), FontSize = 11 });
        selRow.Children.Add(serveTog);
        sp.Children.Add(selRow);

        card.Tag = new ContainerCardRefs(cname, chosenChip, ver, port, serveTog);

        DispatcherTimer? debounce = null;
        void RenderResults(List<RegistryImage> imgs)
        {
            results.Children.Clear();
            foreach (var im in imgs.Take(6))
            {
                var name = im.name;
                var rg = new Grid { Margin = new Thickness(0, 0, 0, 4) };
                rg.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
                rg.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
                rg.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
                var ic = Ui.Glyph("cube", 15, "TextDim"); ic.Margin = new Thickness(0, 0, 9, 0);
                var mid = new StackPanel { VerticalAlignment = VerticalAlignment.Center };
                mid.Children.Add(new TextBlock { Text = name, FontWeight = FontWeights.Medium, FontSize = 12.5 });
                if (!string.IsNullOrEmpty(im.description)) mid.Children.Add(new TextBlock { Text = im.description, Style = Ui.S("Faint"), TextTrimming = TextTrimming.CharacterEllipsis });
                var badge = im.official ? new Border { Style = Ui.S("Chip"), VerticalAlignment = VerticalAlignment.Center, Child = new TextBlock { Text = "OFFICIAL", FontSize = 9.5, Foreground = Ui.B("Accent") } } : null;
                Grid.SetColumn(ic, 0); Grid.SetColumn(mid, 1);
                rg.Children.Add(ic); rg.Children.Add(mid);
                if (badge != null) { Grid.SetColumn(badge, 2); rg.Children.Add(badge); }
                var rb = new Border { CornerRadius = new CornerRadius(7), Padding = new Thickness(9, 6, 9, 6), Cursor = Cursors.Hand, Child = rg };
                rb.MouseEnter += (_, _) => rb.Background = Ui.B("BgCard2");
                rb.MouseLeave += (_, _) => rb.Background = Brushes.Transparent;
                rb.MouseLeftButtonUp += async (_, _) =>
                {
                    var baseName = name.Contains('/') ? name[(name.LastIndexOf('/') + 1)..] : name;
                    ((TextBlock)chosenChip.Child).Text = name;
                    chosenChip.Tag = name;
                    if (string.IsNullOrEmpty(cname.Text)) cname.Text = baseName;
                    if (DefaultPorts.TryGetValue(baseName, out var dp)) port.Text = dp.ToString();
                    selRow.Visibility = Visibility.Visible;
                    results.Children.Clear();
                    if (_client is not null) { var tags = await _client.ImageTagsAsync(name); if (tags.Count > 0) ver.Text = tags[0]; }
                };
                results.Children.Add(rb);
            }
        }
        void DoSearch()
        {
            var term = search.Text.Trim();
            var fallback = Catalog.DockerImages
                .Where(d => term.Length == 0 || d.Name.Contains(term, StringComparison.OrdinalIgnoreCase))
                .Select(d => new RegistryImage(d.Name, d.Desc, d.Official, 0)).ToList();
            RenderResults(fallback);
            if (_client is not null && term.Length > 0)
                _ = _client.SearchImagesAsync(term).ContinueWith(t =>
                {
                    if (t.IsCompletedSuccessfully && t.Result.Count > 0 && search.Text.Trim() == term)
                        Dispatcher.InvokeAsync(() => RenderResults(t.Result));
                });
        }
        search.TextChanged += (_, _) => { debounce?.Stop(); debounce = new DispatcherTimer { Interval = TimeSpan.FromMilliseconds(250) }; debounce.Tick += (_, _) => { debounce!.Stop(); DoSearch(); }; debounce.Start(); };
        cname.Tag = "cname";
        DoSearch(); // popular by default
        return card;
    }

    private record ContainerCardRefs(TextBox Name, Border Chip, TextBox Ver, TextBox Port, CheckBox Serve);

    // ---- submit ------------------------------------------------------
    private async void OnCreate(object sender, RoutedEventArgs e)
    {
        if (_client is null) return;
        var name = _name.Text.Trim();
        if (name.Length == 0) { Status.Text = "Give the project a name."; return; }
        var type = Ui.SelectedVal(_type);

        if (type is "App" or "Cluster")
        {
            var containers = new List<object>();
            foreach (var child in _cardsHost.Children)
                if (child is Border b && b.Tag is ContainerCardRefs r && r.Chip.Tag is string image)
                {
                    var cn = r.Name.Text.Trim();
                    if (cn.Length == 0 || image.Length == 0) continue;
                    int.TryParse(r.Port.Text.Trim(), out var pnum);
                    containers.Add(new { name = cn, image, version = r.Ver.Text.Trim(), port = pnum, serve = r.Serve.IsChecked == true });
                }
            if (containers.Count == 0) { Status.Text = "Add a container: name it and pick an image."; return; }
            var body = new Dictionary<string, object> { ["name"] = name, ["root"] = _base, ["managed"] = type == "Cluster" ? _managed.IsChecked == true : true, ["containers"] = containers };
            if (type == "Cluster" && _subroot.Text.Trim().Length > 0) body["compose_root"] = _subroot.Text.Trim();
            Close();
            await Ui.RunJob(() => _client.PostForJobAsync("/v1/clusters/create", body), $"Creating cluster {name}…");
            _onDone?.Invoke();
            return;
        }

        // PHP site
        var template = type switch { "WordPress" => "wordpress", "Plain PHP" => "plain", _ => "laravel" };
        string db = ""; bool redis = false; var extras = new List<string>();
        foreach (var child in _serviceRows.Children)
            if (child is Grid g && g.Tag is ValueTuple<ComboBox, ComboBox> t)
            {
                var en = Ui.SelectedVal(t.Item1);
                if (en == "sqlite") continue;
                if (en is "postgres" or "mysql" or "mariadb" && db.Length == 0) db = en;
                else if (en == "redis") redis = true;
                else extras.Add(en);
            }
        if (db.Length == 0 && template == "wordpress") db = "mariadb";
        var req = new Dictionary<string, object> { ["name"] = name, ["template"] = template, ["serve"] = _serve.IsChecked == true, ["redis"] = redis };
        if (db.Length > 0) req["db"] = db;
        var php = (_php.SelectedItem ?? _php.Text)?.ToString() ?? "";
        if (php.Length > 0) req["php"] = php;
        Close();
        await Ui.RunJob(() => _client.PostForJobAsync("/v1/projects", req), $"Creating {name}…" + (extras.Count > 0 ? $" (then link: {string.Join(", ", extras)})" : ""));
        _onDone?.Invoke();
    }

    private void OnCancel(object sender, RoutedEventArgs e) => Close();
    private void Close() => (Window.GetWindow(this) as MainWindow)?.CloseDialog();

    private static string LastSeg(string path)
    {
        var t = path.TrimEnd('/', '\\');
        var i = t.LastIndexOfAny(new[] { '/', '\\' });
        return i >= 0 ? t[(i + 1)..] : t;
    }

    private static string Slug(string s)
    {
        s = s.ToLowerInvariant().Trim();
        var sb = new System.Text.StringBuilder();
        bool lastHyphen = false;
        foreach (var c in s)
        {
            if (char.IsLetterOrDigit(c) && c < 128) { sb.Append(c); lastHyphen = false; }
            else if (c is ' ' or '_' or '-' or '.') { if (sb.Length > 0 && !lastHyphen) { sb.Append('-'); lastHyphen = true; } }
        }
        return sb.ToString().Trim('-');
    }
}
