using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Media;

namespace Hull.Gui;

public partial class WizardView : UserControl
{
    private readonly HullClient? _client;
    private readonly Action<bool>? _onDone; // arg: restart needed
    private int _step;
    private string _folder = "";
    private string _tld = "test";
    private int _octet = 1;
    private DependencyInfo? _docker;
    private bool _applied, _done, _restart;
    private readonly List<string> _manual = new();

    private static readonly (string id, string label, string icon)[] StepDefs =
    {
        ("welcome", "Welcome", "sites"), ("docker", "Docker", "cube"), ("folder", "Projects", "folder"),
        ("domain", "Domain", "globe"), ("services", "Services", "services"), ("finish", "Finish", "check"),
    };

    private readonly (string engine, string version, string icon, string name, string blurb, bool on)[] _starters =
    {
        ("postgres", "16", "database", "PostgreSQL", "Relational database", false),
        ("mysql", "8.4", "database", "MySQL", "Relational database", false),
        ("redis", "alpine", "cache", "Redis", "In-memory cache & queues", false),
        ("mailpit", "latest", "mail", "Mailpit", "Catches outgoing email", true),
    };
    private bool[] _starterOn;

    public WizardView(HullClient? client, ConfigInfo? cfg, Action<bool>? onDone)
    {
        InitializeComponent();
        _client = client;
        _onDone = onDone;
        _starterOn = _starters.Select(s => s.on).ToArray();
        if (cfg is not null)
        {
            if (cfg.roots.Length > 0) _folder = cfg.roots[0];
            _tld = cfg.tld;
            if (cfg.loopback is { Length: > 0 } lb) { var p = lb.Split('.'); if (p.Length == 4) int.TryParse(p[3], out _octet); }
        }
        if (int.TryParse(Environment.GetEnvironmentVariable("HULL_WIZARD_STEP"), out var st) && st >= 0 && st < StepDefs.Length) _step = st;
        Loaded += async (_, _) => { Render(); await ProbeDocker(); };
    }

    private async Task ProbeDocker()
    {
        if (_client is null) return;
        try { _docker = (await _client.DependenciesAsync()).FirstOrDefault(d => d.key == "docker"); } catch { }
        if (StepDefs[_step].id == "docker") Render();
    }

    private void Render()
    {
        // rail
        Steps.Children.Clear();
        for (int i = 0; i < StepDefs.Length; i++)
        {
            var (id, label, icon) = StepDefs[i];
            var done = i < _step; var on = i == _step;
            var dot = new Border { Width = 22, Height = 22, CornerRadius = new CornerRadius(11), Background = Ui.B(on ? "Accent" : done ? "Green" : "BgCard2"), Margin = new Thickness(0, 0, 11, 0), VerticalAlignment = VerticalAlignment.Center };
            dot.Child = Ui.Glyph(done ? "check" : icon, 12, on || done ? "TextOnAccent" : "TextDim");
            var row = new StackPanel { Orientation = Orientation.Horizontal, Margin = new Thickness(0, 3, 0, 3) };
            row.Children.Add(dot);
            row.Children.Add(new TextBlock { Text = label, VerticalAlignment = VerticalAlignment.Center, Foreground = Ui.B(on ? "Accent" : done ? "Text" : "TextDim"), FontWeight = on ? FontWeights.SemiBold : FontWeights.Medium, FontSize = 13 });
            var host = new Border { Padding = new Thickness(11, 7, 11, 7), CornerRadius = new CornerRadius(9), Background = on ? Ui.B("AccentSoft") : Brushes.Transparent, Child = row };
            Steps.Children.Add(host);
        }
        Pane.Children.Clear();
        Foot.Children.Clear();
        switch (StepDefs[_step].id)
        {
            case "welcome": Welcome(); break;
            case "docker": Docker(); break;
            case "folder": Folder(); break;
            case "domain": Domain(); break;
            case "services": Services(); break;
            case "finish": Finish(); break;
        }
    }

    private void Eyebrow(string text) => Pane.Children.Add(new TextBlock { Text = text.ToUpperInvariant(), Foreground = Ui.B("Accent"), FontSize = 11.5, FontWeight = FontWeights.Bold, Margin = new Thickness(0, 0, 0, 9) });
    private void H(string text) => Pane.Children.Add(new TextBlock { Text = text, FontSize = 25, FontWeight = FontWeights.Bold, Margin = new Thickness(0, 0, 0, 8) });
    private void Sub(string text) => Pane.Children.Add(new TextBlock { Text = text, Foreground = Ui.B("TextDim"), FontSize = 14, TextWrapping = TextWrapping.Wrap, Margin = new Thickness(0, 0, 0, 24), MaxWidth = 460, HorizontalAlignment = HorizontalAlignment.Left });

    private void Welcome()
    {
        var box = new StackPanel { HorizontalAlignment = HorizontalAlignment.Center, Margin = new Thickness(0, 24, 0, 0) };
        box.Children.Add(new Border { Width = 78, Height = 78, CornerRadius = new CornerRadius(20), Background = Ui.B("Accent"), HorizontalAlignment = HorizontalAlignment.Center, Child = new TextBlock { Text = "H", FontSize = 44, FontWeight = FontWeights.Bold, Foreground = Ui.B("TextOnAccent"), HorizontalAlignment = HorizontalAlignment.Center, VerticalAlignment = VerticalAlignment.Center } });
        box.Children.Add(new TextBlock { Text = "Welcome to Hull", FontSize = 25, FontWeight = FontWeights.Bold, HorizontalAlignment = HorizontalAlignment.Center, Margin = new Thickness(0, 22, 0, 8) });
        box.Children.Add(new TextBlock { Text = "A local environment for your sites and apps — automatic HTTPS, shared databases, and a real domain for every project. Let's get a few basics set up.", Foreground = Ui.B("TextDim"), FontSize = 14, TextWrapping = TextWrapping.Wrap, TextAlignment = TextAlignment.Center, MaxWidth = 440 });
        Pane.Children.Add(box);
        FootNav("Get started");
    }

    private void Docker()
    {
        Eyebrow("System check"); H("Docker");
        Sub("Hull runs your sites in containers, so it needs Docker. It's the only thing Hull doesn't bundle itself.");
        var ready = _docker is not null && (_docker.status == "ok" || _docker.status == "embedded");
        var checking = _docker is null;
        var banner = new Border { Background = Ui.B("BgCard"), BorderBrush = Ui.B("Border"), BorderThickness = new Thickness(1), CornerRadius = new CornerRadius(12), Padding = new Thickness(16), Margin = new Thickness(0, 0, 0, 16) };
        var g = new StackPanel { Orientation = Orientation.Horizontal };
        g.Children.Add(new Border { Width = 38, Height = 38, CornerRadius = new CornerRadius(10), Background = Ui.B(ready ? "Green" : "Amber"), Opacity = 0.18, Margin = new Thickness(0, 0, 13, 0) });
        var ti = new StackPanel { VerticalAlignment = VerticalAlignment.Center };
        ti.Children.Add(new TextBlock { Text = checking ? "Checking for Docker…" : ready ? "Docker is ready" : "Docker not detected", FontWeight = FontWeights.SemiBold, FontSize = 14 });
        ti.Children.Add(new TextBlock { Text = checking ? "Looking for a running engine…" : ready ? (_docker!.name + (string.IsNullOrEmpty(_docker.version) ? "" : " · " + _docker.version)) : "Install Docker Desktop, start it, then re-check.", Foreground = Ui.B("TextDim"), FontSize = 13 });
        g.Children.Add(ti);
        banner.Child = g;
        Pane.Children.Add(banner);
        if (!ready && !checking)
        {
            var btns = new StackPanel { Orientation = Orientation.Horizontal };
            var get = Ui.TextButton("Get Docker Desktop", "cube", "BtnPrimary", (_, _) => Ui.OpenExternal(_docker?.install_url ?? "https://www.docker.com/products/docker-desktop/"), 15);
            var recheck = Ui.TextButton("Re-check", "restart", "Btn", async (_, _) => { _docker = null; Render(); await ProbeDocker(); }, 14);
            recheck.Margin = new Thickness(8, 0, 0, 0);
            btns.Children.Add(get); btns.Children.Add(recheck);
            Pane.Children.Add(btns);
            Pane.Children.Add(new TextBlock { Text = "You can continue setup now and install Docker afterwards — sites just won't start until it's running.", Style = Ui.S("Help") });
        }
        FootNav("Continue");
    }

    private void Folder()
    {
        Eyebrow("Where your projects live"); H("Projects folder");
        Sub("Hull scans this folder for sites and offers it as the home for new ones. You can add more folders later in Settings.");
        var box = new Border { Background = Ui.B("BgCard"), BorderBrush = Ui.B("Border"), BorderThickness = new Thickness(1), CornerRadius = new CornerRadius(11), Padding = new Thickness(15, 13, 15, 13) };
        var g = new Grid();
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        var ic = Ui.Glyph("folder", 17, "Accent"); ic.Margin = new Thickness(0, 0, 10, 0);
        var path = new TextBlock { Text = _folder.Length > 0 ? _folder : "Choose a folder…", FontFamily = Ui.Mono, FontSize = 13, Foreground = Ui.B("Text"), VerticalAlignment = VerticalAlignment.Center, TextTrimming = TextTrimming.CharacterEllipsis };
        var change = Ui.TextButton("Change…", "", "BtnSm", (_, _) => { var p = Ui.PickFolder("Choose a folder for your projects"); if (p != null) { _folder = p; Render(); } });
        Grid.SetColumn(ic, 0); Grid.SetColumn(path, 1); Grid.SetColumn(change, 2);
        g.Children.Add(ic); g.Children.Add(path); g.Children.Add(change);
        box.Child = g;
        Pane.Children.Add(box);
        FootNav("Continue");
    }

    private void Domain()
    {
        Eyebrow("How your sites are reached"); H("Local domain");
        Sub("Every project gets a real hostname with trusted HTTPS. Pick the suffix and the loopback address Hull binds.");
        Pane.Children.Add(Ui.FieldLabel("Top-level domain"));
        var tld = new TextBox { Style = Ui.S("MonoInput"), Width = 160, Text = "." + _tld, HorizontalAlignment = HorizontalAlignment.Left };
        tld.LostKeyboardFocus += (_, _) => { _tld = tld.Text.Trim().TrimStart('.').ToLowerInvariant(); if (_tld.Length == 0) _tld = "test"; tld.Text = "." + _tld; UpdatePreview(); };
        Pane.Children.Add(tld);
        Pane.Children.Add(new TextBlock { Text = "Loopback address", Style = Ui.S("FieldLabel"), Margin = new Thickness(0, 16, 0, 5) });
        var row = new StackPanel { Orientation = Orientation.Horizontal };
        row.Children.Add(new TextBlock { Text = "127.0.0.", FontFamily = Ui.Mono, Foreground = Ui.B("TextFaint"), VerticalAlignment = VerticalAlignment.Center });
        var octetTb = new TextBlock { Text = _octet.ToString(), FontFamily = Ui.Mono, Foreground = Ui.B("Text"), VerticalAlignment = VerticalAlignment.Center, MinWidth = 14, Margin = new Thickness(2, 0, 6, 0) };
        var up = Ui.IconButton("chevup", (_, _) => { _octet = Math.Min(8, _octet + 1); octetTb.Text = _octet.ToString(); UpdatePreview(); }, 11, "TextDim");
        var dn = Ui.IconButton("chevdown", (_, _) => { _octet = Math.Max(1, _octet - 1); octetTb.Text = _octet.ToString(); UpdatePreview(); }, 11, "TextDim");
        var steps = new StackPanel(); steps.Children.Add(up); steps.Children.Add(dn);
        row.Children.Add(octetTb); row.Children.Add(steps);
        Pane.Children.Add(row);
        _preview = new TextBlock { Style = Ui.S("Help"), Margin = new Thickness(0, 16, 0, 0) };
        Pane.Children.Add(_preview);
        UpdatePreview();
        FootNav("Continue");
    }
    private TextBlock? _preview;
    private void UpdatePreview() { if (_preview is not null) _preview.Text = $"A project named shop will be reachable at https://shop.{_tld}, served from 127.0.0.{_octet}."; }

    private void Services()
    {
        Eyebrow("Optional starters"); H("Base services");
        Sub("Shared databases and tools, available to every project. Pick any you know you'll want — you can add or remove these anytime in Services.");
        for (int i = 0; i < _starters.Length; i++)
        {
            int idx = i;
            var s = _starters[i];
            var g = new Grid();
            g.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
            g.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
            g.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
            var ic = new Border { Width = 40, Height = 40, CornerRadius = new CornerRadius(10), Background = Ui.B("AccentSoft"), Margin = new Thickness(0, 0, 14, 0), Child = Ui.Glyph(s.icon, 19, "Accent") };
            var mid = new StackPanel { VerticalAlignment = VerticalAlignment.Center };
            mid.Children.Add(new TextBlock { Text = s.name, FontWeight = FontWeights.SemiBold, FontSize = 14 });
            mid.Children.Add(new TextBlock { Text = s.blurb, Foreground = Ui.B("TextDim"), FontSize = 13 });
            var tog = Ui.Toggle(_starterOn[idx], v => _starterOn[idx] = v);
            Grid.SetColumn(ic, 0); Grid.SetColumn(mid, 1); Grid.SetColumn(tog, 2);
            g.Children.Add(ic); g.Children.Add(mid); g.Children.Add(tog);
            var card = new Border { Background = Ui.B("BgCard"), BorderBrush = Ui.B(_starterOn[idx] ? "Accent" : "Border"), BorderThickness = new Thickness(_starterOn[idx] ? 1.5 : 1), CornerRadius = new CornerRadius(12), Padding = new Thickness(15), Margin = new Thickness(0, 0, 0, 11), Child = g };
            Pane.Children.Add(card);
        }
        FootNav("Continue");
    }

    private void Finish()
    {
        if (_applied) { ApplyPane(); return; }
        Eyebrow("Almost there"); H("Review & finish");
        Sub("Here's what Hull will set up. You can change any of it later in Settings.");
        Pane.Children.Add(ReviewRow("folder", "Projects", _folder));
        Pane.Children.Add(ReviewRow("globe", "Domain & address", $"*.{_tld} → 127.0.0.{_octet}"));
        var chosen = _starters.Where((_, i) => _starterOn[i]).Select(s => s.name);
        Pane.Children.Add(ReviewRow("services", "Base services", chosen.Any() ? string.Join(", ", chosen) : "None — add them later"));
        var back = Ui.TextButton("Back", "", "Btn", (_, _) => { _step--; Render(); });
        var apply = Ui.TextButton("Apply & finish", "check", "BtnPrimary", async (_, _) => await Apply(), 15);
        FootButtons(back, apply);
    }

    private Border ReviewRow(string icon, string title, string val)
    {
        var g = new StackPanel { Orientation = Orientation.Horizontal };
        g.Children.Add(new Border { Width = 38, Height = 38, CornerRadius = new CornerRadius(10), Background = Ui.B("AccentSoft"), Margin = new Thickness(0, 0, 13, 0), Child = Ui.Glyph(icon, 19, "Accent") });
        var mid = new StackPanel { VerticalAlignment = VerticalAlignment.Center };
        mid.Children.Add(new TextBlock { Text = title, FontWeight = FontWeights.SemiBold, FontSize = 14 });
        mid.Children.Add(new TextBlock { Text = val, Foreground = Ui.B("TextDim"), FontFamily = Ui.Mono, FontSize = 12.5, TextTrimming = TextTrimming.CharacterEllipsis });
        g.Children.Add(mid);
        return new Border { Background = Ui.B("BgCard"), BorderBrush = Ui.B("Border"), BorderThickness = new Thickness(1), CornerRadius = new CornerRadius(12), Padding = new Thickness(15), Margin = new Thickness(0, 0, 0, 11), Child = g };
    }

    private StackPanel? _tasksHost;
    private void ApplyPane()
    {
        Eyebrow("Setting things up"); H(_done ? "You're all set" : "Applying your setup…");
        Sub(_done ? "Hull is configured and ready. Service images keep downloading in the background." : "This only takes a moment.");
        _tasksHost = new StackPanel();
        Pane.Children.Add(_tasksHost);
        PaintTasks();
        if (_done)
        {
            var open = Ui.TextButton("Open Hull", "play", "BtnPrimary", (_, _) => _onDone?.Invoke(_restart), 15);
            FootButtons(null, open);
        }
        else FootButtons(null, Disabled("Working…"));
    }

    private readonly List<(string label, string state, string detail)> _tasks = new();
    private void PaintTasks()
    {
        if (_tasksHost is null) return;
        _tasksHost.Children.Clear();
        foreach (var (label, state, detail) in _tasks)
        {
            var g = new StackPanel { Orientation = Orientation.Horizontal, Margin = new Thickness(0, 6, 0, 6) };
            var glyph = state == "ok" ? "check" : state == "warn" ? "alert" : "restart";
            var brush = state == "ok" ? "Green" : state == "warn" ? "Amber" : "Accent";
            g.Children.Add(new Border { Width = 24, Height = 24, CornerRadius = new CornerRadius(12), BorderBrush = Ui.B(brush), BorderThickness = new Thickness(2), Margin = new Thickness(0, 0, 12, 0), Child = Ui.Glyph(glyph, 12, brush) });
            var mid = new StackPanel { VerticalAlignment = VerticalAlignment.Center };
            mid.Children.Add(new TextBlock { Text = label, FontWeight = FontWeights.Medium, FontSize = 14 });
            if (!string.IsNullOrEmpty(detail)) mid.Children.Add(new TextBlock { Text = detail, Foreground = Ui.B("TextDim"), FontSize = 12 });
            g.Children.Add(mid);
            _tasksHost.Children.Add(g);
        }
        if (_manual.Count > 0)
        {
            var mh = new Border { Background = Ui.B("BgCard"), BorderBrush = Ui.B("Border"), BorderThickness = new Thickness(1), CornerRadius = new CornerRadius(11), Padding = new Thickness(14), Margin = new Thickness(0, 14, 0, 0) };
            var sp = new StackPanel();
            sp.Children.Add(new TextBlock { Text = "Two steps need a terminal with admin rights — run these once:", Foreground = Ui.B("TextDim"), FontSize = 12, Margin = new Thickness(0, 0, 0, 8) });
            foreach (var m in _manual) sp.Children.Add(Ui.CodeBlock(m));
            mh.Child = sp;
            _tasksHost.Children.Add(mh);
        }
    }

    private async Task Apply()
    {
        _applied = true; _done = false; _restart = false; _manual.Clear();
        var chosen = _starters.Where((_, i) => _starterOn[i]).ToList();
        _tasks.Clear();
        _tasks.Add(("Saving configuration", "run", ""));
        _tasks.Add((chosen.Count > 0 ? $"Provisioning {chosen.Count} service(s)" : "Services", "idle", chosen.Count > 0 ? "" : "none selected"));
        _tasks.Add(("Local HTTPS & DNS", "idle", ""));
        Render();
        void Set(int i, string state, string detail) { _tasks[i] = (_tasks[i].label, state, detail); PaintTasks(); }

        if (_client is not null)
        {
            try
            {
                var cfg = Ui.Main?.Config;
                var defaults = cfg?.defaults ?? new Defaults("", "", "tableplus");
                var body = new ConfigInfo(_tld, new[] { _folder }, $"127.0.0.{_octet}", defaults, null);
                await _client.PutConfigAsync(body);
                Set(0, "ok", "saved");
            }
            catch (Exception ex) { Set(0, "warn", ex.Message); }

            if (chosen.Count > 0)
            {
                Set(1, "run", "");
                int ok = 0;
                foreach (var s in chosen) { try { await _client.PostForJobAsync("/v1/services", new { engine = s.engine, version = s.version }); ok++; } catch { } }
                Set(1, ok == chosen.Count ? "ok" : "warn", $"{ok}/{chosen.Count} starting · images download in the background");
            }
            else Set(1, "ok", "none selected");

            Set(2, "run", "");
            try
            {
                var res = await _client.ReapplyAsync();
                var steps = res?.steps ?? Array.Empty<ReapplyStep>();
                _manual.AddRange(steps.Where(s => s.status == "manual").Select(s => s.manual!).Where(m => !string.IsNullOrEmpty(m)).Distinct());
                var okN = steps.Count(s => s.status == "ok");
                Set(2, _manual.Count > 0 ? "warn" : "ok", _manual.Count > 0 ? $"{okN} applied · {_manual.Count} need a terminal" : $"{okN} steps applied");
            }
            catch (Exception ex) { Set(2, "warn", ex.Message); }
        }
        _done = true;
        Render();
    }

    // ---- footer helpers ----------------------------------------------
    private void FootNav(string nextLabel)
    {
        var back = _step == 0 ? null : Ui.TextButton("Back", "", "Btn", (_, _) => { _step--; Render(); });
        var next = Ui.TextButton(nextLabel, "chevright", "BtnPrimary", (_, _) =>
        {
            if (StepDefs[_step].id == "folder" && _folder.Length == 0) { Ui.Toast("Choose a projects folder first"); return; }
            _step++; Render();
        }, 14);
        FootButtons(back, next);
    }

    private void FootButtons(Button? left, Button right)
    {
        Foot.ColumnDefinitions.Clear();
        Foot.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        Foot.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        Foot.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        if (left is not null) { Grid.SetColumn(left, 0); Foot.Children.Add(left); }
        Grid.SetColumn(right, 2); Foot.Children.Add(right);
    }

    private Button Disabled(string text)
    {
        var b = Ui.TextButton(text, "", "Btn", (_, _) => { });
        b.IsEnabled = false;
        return b;
    }
}
