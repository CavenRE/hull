using System.Windows;
using System.Windows.Controls;

namespace Hull.Gui;

public partial class DashboardView : UserControl, IRefreshable
{
    private readonly HullClient? _client;

    public DashboardView(HullClient? client)
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
            var projects = await _client.ProjectsAsync();
            var services = await _client.ServicesAsync();
            var checks = await _client.DoctorAsync();

            ProjectsValue.Text = projects.Count.ToString();
            RunningValue.Text = projects.Count(p => p.running).ToString();
            ServicesValue.Text = $"{services.Count(s => s.running)}/{services.Count}";
            HealthList.ItemsSource = checks;
        }
        catch { /* ignore */ }
    }

    private async void OnRefresh(object sender, RoutedEventArgs e) => await RefreshAsync();

    private async void OnStopAll(object sender, RoutedEventArgs e)
    {
        if (_client is null) return;
        try
        {
            await _client.StopAllAsync();
            await RefreshAsync();
        }
        catch { /* ignore */ }
    }
}
