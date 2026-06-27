using System.Diagnostics;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Documents;

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
            EmptyMsg.Visibility = services.Count == 0 ? Visibility.Visible : Visibility.Collapsed;
            Sub.Text = $"{services.Count(s => s.running)} of {services.Count} running";
        }
        catch { /* ignore */ }
    }

    private async void OnStart(object sender, RoutedEventArgs e) => await Act(sender, "start");
    private async void OnStop(object sender, RoutedEventArgs e) => await Act(sender, "stop");

    private async Task Act(object sender, string action)
    {
        if (_client is null || (sender as FrameworkElement)?.DataContext is not ServiceInfo s) return;
        try { await _client.ServiceActionAsync(s.name, action); await RefreshAsync(); }
        catch { /* ignore */ }
    }

    private void OnCopy(object sender, RoutedEventArgs e)
    {
        if ((sender as Button)?.Tag is string s && !string.IsNullOrEmpty(s))
        {
            try { Clipboard.SetText(s); } catch { /* clipboard busy */ }
        }
    }

    private void OnOpenUrl(object sender, RoutedEventArgs e)
    {
        if (sender is Hyperlink { DataContext: ServiceInfo s } && !string.IsNullOrEmpty(s.url))
            OpenExternal(s.url!);
    }

    private static void OpenExternal(string url)
    {
        try { Process.Start(new ProcessStartInfo(url) { UseShellExecute = true }); } catch { /* ignore */ }
    }

    // Dialogs (add / open-with / link / destroy) land in a later pass; wired as
    // no-ops for now so the layout is exercised without destructive actions.
    private void OnAdd(object sender, RoutedEventArgs e) { }
    private void OnOpenWith(object sender, RoutedEventArgs e) { }
    private void OnLink(object sender, RoutedEventArgs e) { }
    private void OnDestroy(object sender, RoutedEventArgs e) { }
}
