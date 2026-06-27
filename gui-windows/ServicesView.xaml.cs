using System.Windows;
using System.Windows.Controls;

namespace Hull.Gui;

public partial class ServicesView : UserControl, IRefreshable
{
    private readonly HullClient? _client;

    public ServicesView(HullClient? client)
    {
        InitializeComponent();
        _client = client;
        Loaded += async (_, _) => await RefreshAsync();
    }

    public async Task RefreshAsync()
    {
        if (_client is null) return;
        try
        {
            var services = await _client.ServicesAsync();
            List.ItemsSource = services;
            EmptyMsg.Visibility = services.Count == 0
                ? System.Windows.Visibility.Visible
                : System.Windows.Visibility.Collapsed;
        }
        catch { /* ignore */ }
    }

    private async void OnRefresh(object sender, RoutedEventArgs e) => await RefreshAsync();
    private async void OnStart(object sender, RoutedEventArgs e) => await Act(sender, "start");
    private async void OnStop(object sender, RoutedEventArgs e) => await Act(sender, "stop");

    private async Task Act(object sender, string action)
    {
        if (_client is null) return;
        if ((sender as FrameworkElement)?.DataContext is not ServiceInfo s) return;
        try
        {
            await _client.ServiceActionAsync(s.name, action);
            await RefreshAsync();
        }
        catch { /* ignore */ }
    }
}
