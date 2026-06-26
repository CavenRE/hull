using System.Windows;
using WinForms = System.Windows.Forms;
using Drawing = System.Drawing;

namespace Hull.Gui;

/// <summary>
/// WPF application shell. Owns the tray icon and close-to-tray behaviour so the
/// daemon keeps running when the window is closed (the GUI is optional , the
/// CLI and daemon work without it).
/// </summary>
public partial class App : System.Windows.Application
{
    private WinForms.NotifyIcon? _tray;
    private MainWindow? _main;

    private void OnStartup(object sender, StartupEventArgs e)
    {
        _main = new MainWindow();
        // Close hides to tray instead of exiting.
        _main.Closing += (_, args) =>
        {
            args.Cancel = true;
            _main.Hide();
        };

        _tray = new WinForms.NotifyIcon
        {
            Icon = Drawing.SystemIcons.Application,
            Visible = true,
            Text = "Hull",
        };
        var menu = new WinForms.ContextMenuStrip();
        menu.Items.Add("Show Hull", null, (_, _) => ShowMain());
        menu.Items.Add("Quit", null, (_, _) => QuitApp());
        _tray.ContextMenuStrip = menu;
        _tray.DoubleClick += (_, _) => ShowMain();

        _main.Show();
    }

    private void ShowMain()
    {
        if (_main is null) return;
        _main.Show();
        _main.WindowState = WindowState.Normal;
        _main.Activate();
    }

    private void QuitApp()
    {
        if (_tray is not null)
        {
            _tray.Visible = false;
            _tray.Dispose();
            _tray = null;
        }
        Shutdown();
    }

    private void OnExit(object sender, ExitEventArgs e) => _tray?.Dispose();
}
