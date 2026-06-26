using System.Collections.ObjectModel;
using System.Windows;

namespace Hull.Gui;

public partial class MainWindow : Window
{
    public ObservableCollection<ProjectInfo> Projects { get; } = new();

    private HullClient? _client;
    private CancellationTokenSource? _events;

    public MainWindow()
    {
        InitializeComponent();
        ProjectList.ItemsSource = Projects;
        Loaded += async (_, _) => await ConnectAndLoad();
        Closed += (_, _) => _events?.Cancel();
    }

    private async Task ConnectAndLoad()
    {
        _client = await HullClient.ConnectAsync();
        if (_client is null)
        {
            StatusText.Text = "Daemon not running , start it with `hulld`";
            return;
        }
        var status = await _client.StatusAsync();
        StatusText.Text = status is null ? "Connected" : $"v{status.version}  ·  .{status.tld}";
        await Refresh();
        StartEvents();
    }

    private async Task Refresh()
    {
        if (_client is null) return;
        try
        {
            var projects = await _client.ProjectsAsync();
            Projects.Clear();
            foreach (var p in projects) Projects.Add(p);
        }
        catch (Exception ex)
        {
            StatusText.Text = "Error: " + ex.Message;
        }
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
                    _ => Dispatcher.InvokeAsync(async () => await Refresh()),
                    ct);
            }
            catch { /* stream ended or cancelled */ }
        }, ct);
    }

    private async void OnRefresh(object sender, RoutedEventArgs e) => await Refresh();

    private async void OnStopAll(object sender, RoutedEventArgs e)
    {
        if (_client is null) return;
        try
        {
            var n = await _client.StopAllAsync();
            StatusText.Text = $"Stopped {n} project(s)/service(s)";
            await Refresh();
        }
        catch (Exception ex)
        {
            StatusText.Text = "Error: " + ex.Message;
        }
    }

    private async void OnStart(object sender, RoutedEventArgs e) => await Action(sender, "start");
    private async void OnStop(object sender, RoutedEventArgs e) => await Action(sender, "stop");
    private async void OnRestart(object sender, RoutedEventArgs e) => await Action(sender, "restart");

    private async Task Action(object sender, string action)
    {
        if (_client is null) return;
        if ((sender as FrameworkElement)?.DataContext is not ProjectInfo p) return;
        try
        {
            await _client.ProjectActionAsync(p.name, action);
            await Refresh();
        }
        catch (Exception ex)
        {
            StatusText.Text = "Error: " + ex.Message;
        }
    }
}
