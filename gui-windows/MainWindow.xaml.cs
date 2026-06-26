using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;

namespace Hull.Gui;

public partial class MainWindow : Window
{
    private HullClient? _client;
    private CancellationTokenSource? _events;

    public MainWindow()
    {
        InitializeComponent();
        Nav.ItemsSource = new[] { "Dashboard", "Sites", "Services", "Mail", "Logs", "Settings" };
        Loaded += async (_, _) => await ConnectAndLoad();
        Closed += (_, _) => _events?.Cancel();
    }

    private async Task ConnectAndLoad()
    {
        _client = await HullClient.ConnectAsync();
        if (_client is null)
        {
            DaemonDot.Fill = (Brush)FindResource("TextFaint");
            DaemonText.Text = "daemon not running";
        }
        else
        {
            var status = await _client.StatusAsync();
            DaemonDot.Fill = (Brush)FindResource("Green");
            DaemonText.Text = status is null ? "connected" : $"v{status.version} · .{status.tld}";
            StartEvents();
        }
        Nav.SelectedIndex = 0; // triggers OnNav -> first screen
    }

    private void OnNav(object sender, SelectionChangedEventArgs e)
    {
        if (Nav.SelectedItem is not string screen) return;
        ContentHost.Content = screen switch
        {
            "Dashboard" => new DashboardView(_client),
            "Sites" => new SitesView(_client),
            "Services" => new ServicesView(_client),
            "Settings" => new SettingsView(_client),
            "Mail" => new PlaceholderView("Mail", "Mailpit inbox , coming to the native GUI."),
            "Logs" => new PlaceholderView("Logs", "Live container logs , coming to the native GUI."),
            _ => ContentHost.Content,
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
            catch { /* stream ended or cancelled */ }
        }, ct);
    }
}
