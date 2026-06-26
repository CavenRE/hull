using System.Windows;
using System.Windows.Controls;

namespace Hull.Gui;

public partial class SettingsView : UserControl, IRefreshable
{
    private readonly HullClient? _client;
    private ConfigInfo? _cfg;

    public SettingsView(HullClient? client)
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
            _cfg = await _client.ConfigAsync();
            if (_cfg is not null)
            {
                TldBox.Text = _cfg.tld;
                EditorBox.Text = _cfg.defaults.editor;
                DbToolBox.Text = _cfg.defaults.db_tool;
                RootsList.ItemsSource = _cfg.roots;
            }
            DoctorList.ItemsSource = await _client.DoctorAsync();
        }
        catch { /* ignore */ }
    }

    private async void OnSave(object sender, RoutedEventArgs e)
    {
        if (_client is null || _cfg is null) return;
        try
        {
            var updated = _cfg with
            {
                tld = TldBox.Text.Trim(),
                defaults = _cfg.defaults with
                {
                    editor = EditorBox.Text.Trim(),
                    db_tool = DbToolBox.Text.Trim(),
                },
            };
            await _client.PutConfigAsync(updated);
            _cfg = updated;
            SaveStatus.Text = "Saved.";
        }
        catch (Exception ex)
        {
            SaveStatus.Text = "Error: " + ex.Message;
        }
    }

    private async void OnStopAll(object sender, RoutedEventArgs e)
    {
        if (_client is null) return;
        try { await _client.StopAllAsync(); SaveStatus.Text = "Stopped all."; }
        catch { /* ignore */ }
    }

    private async void OnStopDaemon(object sender, RoutedEventArgs e)
    {
        if (_client is null) return;
        await _client.ShutdownAsync();
        SaveStatus.Text = "Daemon stopping…";
    }
}
