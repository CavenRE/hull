using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Media;

namespace Hull.Gui;

public partial class SettingsView : UserControl, IRefreshable
{
    private readonly HullClient? _client;
    private ConfigInfo? _cfg;
    private List<Check> _doctor = new();
    private List<DependencyInfo> _deps = new();
    private int _tab;

    private static readonly string[] TabNames = { "General", "System", "Updates", "Advanced" };

    public SettingsView(HullClient? client)
    {
        InitializeComponent();
        _client = client;
        // Deep-link a tab for screenshots/tests (HULL_SETTINGS_TAB=0..3).
        if (int.TryParse(Environment.GetEnvironmentVariable("HULL_SETTINGS_TAB"), out var t) && t >= 0 && t < TabNames.Length) _tab = t;
        TabsHost.Child = Ui.Tabs(TabNames, _tab, i => { _tab = i; RenderTab(); });
        Loaded += async (_, _) => await RefreshAsync();
    }

    public async Task RefreshAsync()
    {
        if (_client is null) return;
        try { _cfg = await _client.ConfigAsync(); } catch { }
        try { _doctor = await _client.DoctorAsync(); } catch { }
        try { _deps = await _client.DependenciesAsync(); } catch { }
        RenderTab();
    }

    // ---- tab rendering ----------------------------------------------
    private void RenderTab()
    {
        Body.Children.Clear();
        switch (_tab)
        {
            case 0: GeneralTab(); break;
            case 1: SystemTab(); break;
            case 2: UpdatesTab(); break;
            default: AdvancedTab(); break;
        }
    }

    private void GeneralTab()
    {
        // Appearance
        Body.Children.Add(Ui.SectionLabel("Appearance"));
        var seg = Ui.Segmented(new[] { "Auto", "Light", "Dark" },
            ThemeManager.Current switch { "light" => 1, "dark" => 2, _ => 0 },
            i => ThemeManager.Apply(i switch { 1 => "light", 2 => "dark", _ => "auto" }));
        Body.Children.Add(Card(Row("Theme", "Follow the system appearance, or force light / dark.", seg)));

        // Project folders
        Body.Children.Add(Ui.SectionLabel("Project folders"));
        Body.Children.Add(Card(FoldersBody()));

        // Default tools
        Body.Children.Add(Ui.SectionLabel("Default tools"));
        Body.Children.Add(Card(DefaultsBody()));

        // Local domain
        Body.Children.Add(Ui.SectionLabel("Local domain"));
        Body.Children.Add(Card(DomainBody()));

        // Hull service
        Body.Children.Add(Ui.SectionLabel("Hull service"));
        var btns = new StackPanel { Orientation = Orientation.Horizontal };
        btns.Children.Add(Ui.TextButton("Restart", "restart", "BtnSm", async (_, _) => { if (Ui.Main is not null) await Ui.Main.RestartDaemonAsync(); }, 13));
        var stop = Ui.TextButton("Stop", "stop", "BtnDanger", async (_, _) => { if (Ui.Main is not null) await Ui.Main.StopDaemonAsync(); }, 13);
        stop.Margin = new Thickness(8, 0, 0, 0);
        btns.Children.Add(stop);
        Body.Children.Add(Card(Row("Daemon", "Restart to apply loopback or domain changes. Stopping shuts down all sites & services and releases ports 80/443.", btns)));
    }

    private FrameworkElement FoldersBody()
    {
        var sp = new StackPanel();
        sp.Children.Add(new TextBlock { Text = "Folders Hull scans for projects and offers as locations for new ones.", Style = Ui.S("Muted"), Margin = new Thickness(0, 0, 0, 12), TextWrapping = TextWrapping.Wrap });
        var roots = _cfg?.roots ?? Array.Empty<string>();
        for (int i = 0; i < roots.Length; i++)
        {
            int idx = i;
            var row = new Grid { Margin = new Thickness(0, 0, 0, 6) };
            row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(90) });
            row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
            row.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
            var bg = new Border { Background = Ui.B("BgInset"), CornerRadius = new CornerRadius(7), Padding = new Thickness(12, 9, 8, 9) };
            var inner = new Grid();
            inner.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(90) });
            inner.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
            inner.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
            var label = new TextBlock { Text = LastSeg(roots[i]), FontWeight = FontWeights.Medium, Foreground = Ui.B("Text"), VerticalAlignment = VerticalAlignment.Center };
            var path = new TextBlock { Text = roots[i], FontFamily = Ui.Mono, FontSize = 12, Foreground = Ui.B("TextDim"), VerticalAlignment = VerticalAlignment.Center, TextTrimming = TextTrimming.CharacterEllipsis, Margin = new Thickness(8, 0, 8, 0) };
            Grid.SetColumn(label, 0); Grid.SetColumn(path, 1);
            var ctrls = new StackPanel { Orientation = Orientation.Horizontal };
            var up = Ui.IconButton("chevup", (_, _) => MoveRoot(idx, -1), 13, "TextDim", "Move up"); up.IsEnabled = i > 0;
            var down = Ui.IconButton("chevdown", (_, _) => MoveRoot(idx, 1), 13, "TextDim", "Move down"); down.IsEnabled = i < roots.Length - 1;
            var del = Ui.IconButton("trash", (_, _) => RemoveRoot(idx), 13, "TextDim", "Remove");
            ctrls.Children.Add(up); ctrls.Children.Add(down); ctrls.Children.Add(del);
            Grid.SetColumn(ctrls, 2);
            inner.Children.Add(label); inner.Children.Add(path); inner.Children.Add(ctrls);
            bg.Child = inner;
            sp.Children.Add(bg);
        }
        sp.Children.Add(new TextBlock { Text = "Order sets how groups appear in Sites; the top folder wins if two contain the same project name.", Style = Ui.S("Help") });
        var add = Ui.TextButton("Add folder…", "plus", "BtnSm", (_, _) => AddRoot(), 14);
        add.HorizontalAlignment = HorizontalAlignment.Left; add.Margin = new Thickness(0, 12, 0, 0);
        sp.Children.Add(add);
        return sp;
    }

    private void AddRoot()
    {
        var p = Ui.PickFolder("Choose a folder Hull should scan for projects");
        if (p is null || _cfg is null) return;
        var norm = p.Replace('\\', '/');
        if (_cfg.roots.Any(r => r.Replace('\\', '/').Equals(norm, StringComparison.OrdinalIgnoreCase))) { Ui.Toast("That folder is already added"); return; }
        var roots = _cfg.roots.Append(p).ToArray();
        _ = SaveRoots(roots, "Folder added");
    }

    private void RemoveRoot(int i)
    {
        if (_cfg is null) return;
        if (_cfg.roots.Length <= 1) { Ui.Toast("Keep at least one folder"); return; }
        var roots = _cfg.roots.Where((_, j) => j != i).ToArray();
        _ = SaveRoots(roots, "Folder removed");
    }

    private void MoveRoot(int i, int dir)
    {
        if (_cfg is null) return;
        int j = i + dir;
        if (j < 0 || j >= _cfg.roots.Length) return;
        var roots = (string[])_cfg.roots.Clone();
        (roots[i], roots[j]) = (roots[j], roots[i]);
        _ = SaveRoots(roots, "Folder order updated");
    }

    private async Task SaveRoots(string[] roots, string msg)
    {
        if (_client is null || _cfg is null) return;
        var updated = _cfg with { roots = roots };
        try { await _client.PutConfigAsync(updated); _cfg = updated; Ui.Toast(msg); await Ui.Reload(); }
        catch (Exception ex) { Ui.Toast(ex.Message); }
    }

    private FrameworkElement DefaultsBody()
    {
        var sp = new StackPanel();
        sp.Children.Add(new TextBlock { Text = "The apps Hull opens projects and databases with.", Style = Ui.S("Muted"), Margin = new Thickness(0, 0, 0, 12) });
        var grid = new Grid { MaxWidth = 560, HorizontalAlignment = HorizontalAlignment.Left };
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        var editor = Ui.Select(new[] { ("", "System default"), ("code", "VS Code"), ("phpstorm", "PhpStorm"), ("cursor", "Cursor"), ("subl", "Sublime Text"), ("zed", "Zed") }, _cfg?.defaults.editor ?? "");
        editor.SelectionChanged += (_, _) => SaveDefault(editor: Ui.SelectedVal(editor));
        var db = Ui.Select(new[] { ("tableplus", "TablePlus"), ("dbeaver", "DBeaver"), ("adminer", "Adminer (web)"), ("cli", "CLI") }, _cfg?.defaults.db_tool ?? "tableplus");
        db.SelectionChanged += (_, _) => SaveDefault(dbTool: Ui.SelectedVal(db));
        var c0 = new StackPanel { Margin = new Thickness(0, 0, 8, 0) };
        c0.Children.Add(Ui.FieldLabel("Open in editor")); c0.Children.Add(editor);
        var c1 = new StackPanel { Margin = new Thickness(8, 0, 0, 0) };
        c1.Children.Add(Ui.FieldLabel("Database tool")); c1.Children.Add(db);
        Grid.SetColumn(c0, 0); Grid.SetColumn(c1, 1);
        grid.Children.Add(c0); grid.Children.Add(c1);
        sp.Children.Add(grid);
        return sp;
    }

    private void SaveDefault(string? editor = null, string? dbTool = null)
    {
        if (_client is null || _cfg is null) return;
        var d = _cfg.defaults with { editor = editor ?? _cfg.defaults.editor, db_tool = dbTool ?? _cfg.defaults.db_tool };
        var updated = _cfg with { defaults = d };
        _ = QuietSave(updated, "Default saved");
    }

    private FrameworkElement DomainBody()
    {
        var sp = new StackPanel();
        // Loopback address
        sp.Children.Add(Ui.FieldLabel("Loopback address"));
        sp.Children.Add(LoopbackEditor());
        sp.Children.Add(new TextBlock { Text = "127.0.0.1–.8, to coexist with another local proxy. Needs Hull's DNS (or your resolver pointed here); restart Hull to apply.", Style = Ui.S("Help") });
        // TLD
        var tldWrap = new StackPanel { Margin = new Thickness(0, 16, 0, 0) };
        tldWrap.Children.Add(Ui.FieldLabel("Top-level domain"));
        var tldRow = new StackPanel { Orientation = Orientation.Horizontal };
        var tld = new TextBox { Style = Ui.S("MonoInput"), Width = 130, Text = "." + (_cfg?.tld ?? "test") };
        tld.LostKeyboardFocus += (_, _) => CommitTld(tld);
        tld.KeyDown += (_, e) => { if (e.Key == Key.Enter) CommitTld(tld); };
        var rerun = Ui.TextButton("Re-run setup", "restart", "Btn", async (_, _) => await Reapply(), 14);
        rerun.Margin = new Thickness(10, 0, 0, 0);
        tldRow.Children.Add(tld); tldRow.Children.Add(rerun);
        tldWrap.Children.Add(tldRow);
        sp.Children.Add(tldWrap);
        sp.Children.Add(new TextBlock { Text = "Changing these rewrites every site's domain and re-issues certificates — Hull will ask for one admin prompt to update DNS.", Style = Ui.S("Help") });
        return sp;
    }

    private FrameworkElement LoopbackEditor()
    {
        int octet = 1;
        var lb = _cfg?.loopback ?? "127.0.0.1";
        var parts = lb.Split('.');
        if (parts.Length == 4) int.TryParse(parts[3], out octet);
        var row = new StackPanel { Orientation = Orientation.Horizontal };
        void Ro(string t)
        {
            var tb = new TextBlock { Text = t, FontFamily = Ui.Mono, Foreground = Ui.B("TextDim"), HorizontalAlignment = HorizontalAlignment.Center, VerticalAlignment = VerticalAlignment.Center };
            row.Children.Add(new Border { Width = 46, Height = 30, Background = Ui.B("BgInset"), BorderBrush = Ui.B("CtrlBorder"), BorderThickness = new Thickness(1), CornerRadius = new CornerRadius(7), Child = tb });
        }
        void Sep() => row.Children.Add(new TextBlock { Text = ".", Foreground = Ui.B("TextFaint"), VerticalAlignment = VerticalAlignment.Center, Margin = new Thickness(4, 0, 4, 0) });
        Ro("127"); Sep(); Ro("0"); Sep(); Ro("0"); Sep();
        var box = new Border { Background = Ui.B("BgInset"), BorderBrush = Ui.B("CtrlBorder"), BorderThickness = new Thickness(1), CornerRadius = new CornerRadius(7), Padding = new Thickness(10, 0, 4, 0), Height = 30 };
        var inner = new StackPanel { Orientation = Orientation.Horizontal };
        var val = new TextBlock { Text = octet.ToString(), FontFamily = Ui.Mono, Foreground = Ui.B("Text"), VerticalAlignment = VerticalAlignment.Center, MinWidth = 14 };
        var steps = new StackPanel { Margin = new Thickness(8, 0, 0, 0), VerticalAlignment = VerticalAlignment.Center };
        void SetOctet(int n)
        {
            n = Math.Max(1, Math.Min(8, n));
            val.Text = n.ToString();
            if (_cfg is null) return;
            _ = QuietSave(_cfg with { loopback = $"127.0.0.{n}" }, $"Loopback 127.0.0.{n} saved — restart to apply");
        }
        var up = Ui.IconButton("chevup", (_, _) => SetOctet(int.Parse(val.Text) + 1), 11, "TextDim"); up.Padding = new Thickness(2);
        var dn = Ui.IconButton("chevdown", (_, _) => SetOctet(int.Parse(val.Text) - 1), 11, "TextDim"); dn.Padding = new Thickness(2);
        steps.Children.Add(up); steps.Children.Add(dn);
        inner.Children.Add(val); inner.Children.Add(steps);
        box.Child = inner;
        row.Children.Add(box);
        return row;
    }

    private void CommitTld(TextBox tld)
    {
        if (_cfg is null) return;
        var v = tld.Text.Trim().TrimStart('.').ToLowerInvariant();
        if (string.IsNullOrEmpty(v) || v == _cfg.tld) { tld.Text = "." + _cfg.tld; return; }
        if (!System.Text.RegularExpressions.Regex.IsMatch(v, "^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"))
        { Ui.Toast("Enter a valid domain label, e.g. .test"); tld.Text = "." + _cfg.tld; return; }
        _ = QuietSave(_cfg with { tld = v }, $"Domain .{v} saved — restart to apply");
        tld.Text = "." + v;
    }

    private async Task Reapply()
    {
        if (_client is null) return;
        Ui.Toast("Re-applying setup…");
        try
        {
            var res = await _client.ReapplyAsync();
            var steps = res?.steps ?? Array.Empty<ReapplyStep>();
            var ok = steps.Count(s => s.status == "ok");
            var manual = steps.Where(s => s.status == "manual").Select(s => s.manual).Where(m => !string.IsNullOrEmpty(m)).ToList();
            Ui.Toast(manual.Count > 0 ? $"{ok} re-applied · {manual.Count} step(s) need a terminal" : $"Setup re-applied — {ok} step(s) OK");
            await Ui.Reload();
        }
        catch (Exception ex) { Ui.Toast("Re-apply failed: " + ex.Message); }
    }

    // ---- System tab -------------------------------------------------
    private void SystemTab()
    {
        Body.Children.Add(Ui.SectionLabel("Startup"));
        var sp = new StackPanel();
        var p = ThemeManager.Prefs;
        var rows = new (string, string, bool, Action<bool>)[]
        {
            ("Launch Hull at login", "Start automatically when you sign in.", StartupRegistration.IsEnabled(), v => StartupRegistration.Set(v)),
            ("Start daemon on launch", "Bring up the router and engine when Hull opens.", p.start_daemon_on_launch, v => { p.start_daemon_on_launch = v; p.Save(); }),
            ("Restore running sites", "Re-start sites that were running when you last quit.", p.restore_running, v => { p.restore_running = v; p.Save(); }),
            ("Keep running in the tray", "Closing the window keeps the daemon alive in the tray.", p.close_to_tray, v => { p.close_to_tray = v; p.Save(); }),
            ("Check for updates automatically", "Notify when dependency updates are available.", p.check_updates, v => { p.check_updates = v; p.Save(); }),
        };
        for (int i = 0; i < rows.Length; i++)
        {
            if (i > 0) sp.Children.Add(new Border { Style = Ui.S("Hairline"), Margin = new Thickness(0, 4, 0, 4) });
            sp.Children.Add(StartupRow(rows[i].Item1, rows[i].Item2, rows[i].Item3, rows[i].Item4));
        }
        Body.Children.Add(Card(sp));

        Body.Children.Add(Ui.SectionLabel("Doctor"));
        Body.Children.Add(Card(DoctorBody()));
    }

    private FrameworkElement StartupRow(string name, string desc, bool on, Action<bool> set)
    {
        var toggle = Ui.Toggle(on, v => { set(v); Ui.Toast(v ? "On" : "Off"); });
        var r = Row(name, desc, toggle);
        r.Margin = new Thickness(0, 6, 0, 6);
        return r;
    }

    private FrameworkElement DoctorBody()
    {
        var sp = new StackPanel();
        var head = new Grid { Margin = new Thickness(0, 0, 0, 8) };
        head.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        head.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        var p = new TextBlock { Text = "Health checks for the local environment.", Style = Ui.S("Muted"), VerticalAlignment = VerticalAlignment.Center };
        var run = Ui.TextButton("Run again", "restart", "BtnSm", async (_, _) => { Ui.Toast("Running checks…"); if (_client is not null) _doctor = await _client.DoctorAsync(); RenderTab(); }, 13);
        Grid.SetColumn(p, 0); Grid.SetColumn(run, 1);
        head.Children.Add(p); head.Children.Add(run);
        sp.Children.Add(head);
        if (_doctor.Count == 0)
            sp.Children.Add(new TextBlock { Text = "Couldn't reach the daemon's health checks.", Style = Ui.S("Faint") });
        foreach (var c in _doctor) sp.Children.Add(CheckRow(c.name, c.status, c.detail));
        return sp;
    }

    private FrameworkElement CheckRow(string name, string status, string detail)
    {
        var g = new Grid { Margin = new Thickness(0, 4, 0, 4) };
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(180) });
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        var dot = Ui.Dot(status == "ok" ? "ok" : status == "warn" ? "warn" : "error");
        dot.Margin = new Thickness(0, 0, 10, 0);
        var n = new TextBlock { Text = name, VerticalAlignment = VerticalAlignment.Center, TextTrimming = TextTrimming.CharacterEllipsis, Margin = new Thickness(0, 0, 14, 0) };
        var d = new TextBlock { Text = detail, Style = Ui.S("Faint"), VerticalAlignment = VerticalAlignment.Center, TextTrimming = TextTrimming.CharacterEllipsis };
        Grid.SetColumn(dot, 0); Grid.SetColumn(n, 1); Grid.SetColumn(d, 2);
        g.Children.Add(dot); g.Children.Add(n); g.Children.Add(d);
        return g;
    }

    // ---- Updates tab ------------------------------------------------
    private void UpdatesTab()
    {
        Body.Children.Add(Ui.SectionLabel("Dependencies"));
        var sp = new StackPanel();
        var head = new Grid { Margin = new Thickness(0, 0, 0, 8) };
        head.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        head.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        var p = new TextBlock { Text = "Docker is the only external dependency; routing, DNS, and TLS are built into Hull.", Style = Ui.S("Muted"), VerticalAlignment = VerticalAlignment.Center, TextWrapping = TextWrapping.Wrap };
        var recheck = Ui.TextButton("Re-check", "restart", "BtnSm", async (_, _) => { Ui.Toast("Re-checking…"); if (_client is not null) _deps = await _client.DependenciesAsync(); RenderTab(); }, 13);
        Grid.SetColumn(p, 0); Grid.SetColumn(recheck, 1);
        head.Children.Add(p); head.Children.Add(recheck);
        sp.Children.Add(head);
        foreach (var d in _deps) sp.Children.Add(DepRow(d));
        if (_deps.Count == 0) sp.Children.Add(new TextBlock { Text = "No dependency info.", Style = Ui.S("Faint") });
        Body.Children.Add(Card(sp));
    }

    private FrameworkElement DepRow(DependencyInfo d)
    {
        var g = new Grid { Margin = new Thickness(0, 6, 0, 6) };
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        var ic = Ui.Glyph("cube", 16, "TextDim"); ic.Margin = new Thickness(0, 0, 10, 0);
        var mid = new StackPanel { VerticalAlignment = VerticalAlignment.Center };
        mid.Children.Add(new TextBlock { Text = d.name + (string.IsNullOrEmpty(d.version) ? "" : " · " + d.version), FontWeight = FontWeights.Medium });
        mid.Children.Add(new TextBlock { Text = d.blurb, Style = Ui.S("Faint"), TextWrapping = TextWrapping.Wrap });
        var right = new StackPanel { Orientation = Orientation.Horizontal, VerticalAlignment = VerticalAlignment.Center };
        var (pillText, pillKey) = d.status switch
        {
            "ok" => ("Running", "Green"),
            "embedded" => ("Built-in", "Green"),
            "stopped" => ("Not running", "Amber"),
            _ => ("Not installed", "Red"),
        };
        var pill = new Border { Style = Ui.S("Chip"), Child = new TextBlock { Text = pillText, FontSize = 11.5, Foreground = Ui.B(pillKey) } };
        right.Children.Add(pill);
        if (d.status is "missing" or "stopped")
        {
            var btn = Ui.TextButton(d.status == "stopped" ? "Open Docker" : "Get Docker", "", "BtnSm", (_, _) => { if (!string.IsNullOrEmpty(d.install_url)) Ui.OpenExternal(d.install_url!); });
            btn.Margin = new Thickness(8, 0, 0, 0);
            right.Children.Add(btn);
        }
        Grid.SetColumn(ic, 0); Grid.SetColumn(mid, 1); Grid.SetColumn(right, 2);
        g.Children.Add(ic); g.Children.Add(mid); g.Children.Add(right);
        return g;
    }

    // ---- Advanced tab -----------------------------------------------
    private void AdvancedTab()
    {
        Body.Children.Add(Ui.SectionLabel("Danger zone"));
        var sp = new StackPanel();
        var clear = Ui.TextButton("Clear caches", "", "BtnSm", async (_, _) => { Ui.Toast("Caches cleared — projects re-detected"); await Ui.Reload(); });
        sp.Children.Add(Row("Clear caches & rebuild", "Flush Hull's derived state and re-detect every project.", clear));
        sp.Children.Add(new Border { Style = Ui.S("Hairline") });
        var reset = Ui.TextButton("Reset…", "trash", "BtnDanger", (_, _) => ShowResetDialog());
        sp.Children.Add(Row("Reset Hull", "Remove all configuration, certificates, and service volumes. Project files are untouched.", reset, dangerName: true));
        Body.Children.Add(new Border { Style = Ui.S("DangerCard"), Child = sp });
    }

    private void ShowResetDialog()
    {
        const string cmds = "# 1. Quit Hull (Settings › Stop, or close the app)\n# 2. Remove Hull's home (config, local CA, certs, derived state):\nrm -rf ~/.hull\n# 3. Remove Hull's shared-service volumes via Docker:\ndocker volume ls -q --filter name=hull | xargs -r docker volume rm";
        var body = new StackPanel { MaxWidth = 560 };
        body.Children.Add(new TextBlock { Text = "A full reset removes Hull's configuration, local certificate authority, and shared-service volumes. Your project files are never touched. Run these manually:", Style = Ui.S("Muted"), TextWrapping = TextWrapping.Wrap, Margin = new Thickness(0, 0, 0, 12) });
        body.Children.Add(Ui.CodeBlock(cmds));
        var copy = Ui.TextButton("Copy commands", "copy", "BtnPrimary", (_, _) => Ui.Copy(cmds), 14);
        Ui.ShowDialog(Ui.Dialog("Reset Hull", body, Ui.CancelButton("Close"), copy));
    }

    // ---- shared helpers ---------------------------------------------
    private Border Card(FrameworkElement content) =>
        new() { Style = Ui.S("Card"), Margin = new Thickness(0, 0, 0, 16), Child = content };

    private Grid Row(string name, string desc, FrameworkElement control, bool dangerName = false)
    {
        var g = new Grid();
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        var info = new StackPanel { VerticalAlignment = VerticalAlignment.Center, Margin = new Thickness(0, 0, 16, 0) };
        info.Children.Add(new TextBlock { Text = name, FontWeight = FontWeights.Medium, Foreground = Ui.B(dangerName ? "Red" : "Text") });
        info.Children.Add(new TextBlock { Text = desc, Style = Ui.S("Faint"), TextWrapping = TextWrapping.Wrap, Margin = new Thickness(0, 2, 0, 0) });
        control.VerticalAlignment = VerticalAlignment.Center;
        Grid.SetColumn(info, 0); Grid.SetColumn(control, 1);
        g.Children.Add(info); g.Children.Add(control);
        return g;
    }

    private async Task QuietSave(ConfigInfo updated, string msg)
    {
        if (_client is null) return;
        try { await _client.PutConfigAsync(updated); _cfg = updated; Ui.Toast(msg); }
        catch (Exception ex) { Ui.Toast(ex.Message); }
    }

    private static string LastSeg(string path)
    {
        var t = path.TrimEnd('/', '\\');
        var i = t.LastIndexOfAny(new[] { '/', '\\' });
        return i >= 0 ? t[(i + 1)..] : t;
    }
}
