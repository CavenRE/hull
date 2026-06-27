using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;

namespace Hull.Gui;

public partial class MailView : UserControl, IRefreshable
{
    private readonly HullClient? _client;
    private List<ServiceInfo> _services = new();
    private List<ProjectInfo> _projects = new();

    public MailView(HullClient? client)
    {
        InitializeComponent();
        _client = client;
        Loaded += async (_, _) => await RefreshAsync();
    }

    public async Task RefreshAsync()
    {
        if (_client is null) return;
        try { _services = await _client.ServicesAsync(); } catch { }
        try { _projects = await _client.ProjectsAsync(); } catch { }
        Render();
    }

    private void Render()
    {
        Body.Children.Clear();
        var mailpit = _services.FirstOrDefault(s => s.engine == "mailpit");
        if (mailpit is null) { RenderEmpty(); return; }
        RenderActive(mailpit);
    }

    private void RenderEmpty()
    {
        var box = new StackPanel { HorizontalAlignment = HorizontalAlignment.Center, Margin = new Thickness(0, 80, 0, 0), MaxWidth = 420 };
        var ic = new Border { Width = 52, Height = 52, CornerRadius = new CornerRadius(14), Background = Ui.B("AccentSoft"), HorizontalAlignment = HorizontalAlignment.Center, Child = Ui.Glyph("mail", 24, "Accent") };
        box.Children.Add(ic);
        box.Children.Add(new TextBlock { Text = "Catch outgoing mail", FontSize = 18, FontWeight = FontWeights.SemiBold, HorizontalAlignment = HorizontalAlignment.Center, Margin = new Thickness(0, 16, 0, 6) });
        box.Children.Add(new TextBlock { Text = "Add Mailpit to capture every email your projects send — nothing leaves your machine. Wire it into any Laravel site in one click.", Style = Ui.S("Muted"), TextWrapping = TextWrapping.Wrap, TextAlignment = TextAlignment.Center, Margin = new Thickness(0, 0, 0, 16) });
        var add = Ui.TextButton("Add Mailpit", "plus", "BtnPrimary", async (_, _) =>
        {
            if (_client is not null)
                await Ui.RunJob(() => _client.PostForJobAsync("/v1/services", new { engine = "mailpit", version = "latest" }), "Adding Mailpit…");
            await RefreshAsync();
        });
        add.HorizontalAlignment = HorizontalAlignment.Center;
        box.Children.Add(add);
        Body.Children.Add(box);
    }

    private void RenderActive(ServiceInfo mailpit)
    {
        var tld = Ui.Main?.Tld ?? "test";
        var inbox = string.IsNullOrEmpty(mailpit.url) ? $"https://mail.{tld}" : mailpit.url!;

        // Status card
        Body.Children.Add(Ui.SectionLabel("Mailpit"));
        var head = new Grid();
        head.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        head.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        head.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        var dot = Ui.Dot(mailpit.running ? "running" : "stopped"); dot.Margin = new Thickness(0, 0, 10, 0);
        var info = new StackPanel { VerticalAlignment = VerticalAlignment.Center };
        info.Children.Add(new TextBlock { Text = mailpit.running ? "Running" : "Stopped", FontWeight = FontWeights.Medium });
        info.Children.Add(new TextBlock { Text = "SMTP 127.0.0.1:1025  ·  Inbox " + inbox.Replace("https://", ""), Style = Ui.S("Faint") });
        var open = Ui.TextButton("Open Mailpit", "external", "BtnSm", (_, _) => Ui.OpenExternal(inbox), 13);
        Grid.SetColumn(dot, 0); Grid.SetColumn(info, 1); Grid.SetColumn(open, 2);
        head.Children.Add(dot); head.Children.Add(info); head.Children.Add(open);
        Body.Children.Add(new Border { Style = Ui.S("Card"), Margin = new Thickness(0, 0, 0, 16), Child = head });

        // Laravel .env block
        Body.Children.Add(Ui.SectionLabel("Laravel .env"));
        Body.Children.Add(Wrap(Ui.CodeBlock(
            "MAIL_MAILER=smtp\nMAIL_HOST=hull-mailpit\nMAIL_PORT=1025\nMAIL_USERNAME=null\nMAIL_PASSWORD=null\nMAIL_ENCRYPTION=null\nMAIL_FROM_ADDRESS=\"hello@example." + tld + "\"")));

        // SMTP block
        Body.Children.Add(Ui.SectionLabel("SMTP settings"));
        Body.Children.Add(Wrap(Ui.CodeBlock(
            "Host:        127.0.0.1\nPort:        1025\nEncryption:  none\nUsername:    (none)\nPassword:    (none)")));

        // Wired apps
        Body.Children.Add(Ui.SectionLabel("Wired apps"));
        var card = new StackPanel();
        var laravel = _projects.Where(p => p.kind == "laravel").ToList();
        if (laravel.Count == 0)
            card.Children.Add(new TextBlock { Text = "No Laravel sites yet.", Style = Ui.S("Muted") });
        for (int i = 0; i < laravel.Count; i++)
        {
            if (i > 0) card.Children.Add(new Border { Style = Ui.S("Hairline"), Margin = new Thickness(0, 4, 0, 4) });
            card.Children.Add(WiredRow(laravel[i], mailpit));
        }
        Body.Children.Add(new Border { Style = Ui.S("Card"), Child = card });
    }

    private FrameworkElement WiredRow(ProjectInfo p, ServiceInfo mailpit)
    {
        bool wired = p.LinkedServices.Any(l => l.key == "mail");
        var g = new Grid { Margin = new Thickness(0, 6, 0, 6) };
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        var ic = Ui.Glyph("sites", 15, "TextDim"); ic.Margin = new Thickness(0, 0, 10, 0);
        var mid = new StackPanel { VerticalAlignment = VerticalAlignment.Center };
        mid.Children.Add(new TextBlock { Text = p.name, FontWeight = FontWeights.Medium });
        mid.Children.Add(new TextBlock { Text = wired ? "Wired to Mailpit" : "Not wired", Style = Ui.S("Faint") });
        Button btn = wired
            ? Ui.TextButton("Unwire", "unlink", "BtnSm", async (_, _) =>
            {
                if (_client is not null) await Ui.RunJob(() => _client.PostForJobAsync($"/v1/projects/{Uri.EscapeDataString(p.name)}/unlink", new { key = "mail" }), $"Unwired {p.name}");
                await RefreshAsync();
            }, 13)
            : Ui.TextButton("Wire", "link", "BtnSm", async (_, _) =>
            {
                if (_client is not null) await Ui.RunJob(() => _client.PostForJobAsync($"/v1/services/{Uri.EscapeDataString(mailpit.name)}/link", new { project = p.name }), $"Wired {p.name} to Mailpit");
                await RefreshAsync();
            }, 13);
        Grid.SetColumn(ic, 0); Grid.SetColumn(mid, 1); Grid.SetColumn(btn, 2);
        g.Children.Add(ic); g.Children.Add(mid); g.Children.Add(btn);
        return g;
    }

    private static Border Wrap(FrameworkElement el) => new() { Margin = new Thickness(0, 0, 0, 16), Child = el };
}
