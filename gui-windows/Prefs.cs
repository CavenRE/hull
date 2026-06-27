using System.IO;
using System.Text.Json;
using System.Windows;

namespace Hull.Gui;

/// <summary>
/// GUI-local preferences (theme + startup behaviour) persisted to
/// {HULL_HOME|~/.hull}/gui.json. These are GUI concerns, not daemon state, so
/// they live beside the daemon's files but are owned entirely by this app.
/// </summary>
public sealed class GuiPrefs
{
    public string theme { get; set; } = "auto";              // auto | light | dark
    public bool start_daemon_on_launch { get; set; } = true;
    public bool restore_running { get; set; } = true;
    public bool close_to_tray { get; set; } = true;
    public bool check_updates { get; set; } = true;

    private static readonly JsonSerializerOptions Opts = new() { WriteIndented = true, PropertyNameCaseInsensitive = true };

    public static string Path()
    {
        var home = Environment.GetEnvironmentVariable("HULL_HOME");
        if (string.IsNullOrEmpty(home))
            home = System.IO.Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".hull");
        return System.IO.Path.Combine(home, "gui.json");
    }

    public static GuiPrefs Load()
    {
        try
        {
            var p = Path();
            if (File.Exists(p))
                return JsonSerializer.Deserialize<GuiPrefs>(File.ReadAllText(p), Opts) ?? new GuiPrefs();
        }
        catch { /* fall back to defaults */ }
        return new GuiPrefs();
    }

    public void Save()
    {
        try
        {
            var p = Path();
            Directory.CreateDirectory(System.IO.Path.GetDirectoryName(p)!);
            File.WriteAllText(p, JsonSerializer.Serialize(this, Opts));
        }
        catch { /* best effort */ }
    }
}

/// <summary>
/// Applies and live-swaps the colour palette. The palette is merged dictionary
/// index 0 in App.xaml; replacing it updates every DynamicResource brush.
/// </summary>
public static class ThemeManager
{
    public static GuiPrefs Prefs { get; private set; } = new();
    public static string Current { get; private set; } = "auto";
    public static event Action? Changed;

    public static void Init()
    {
        Prefs = GuiPrefs.Load();
        // HULL_THEME env overrides the stored pref (used by the screenshot harness).
        var env = Environment.GetEnvironmentVariable("HULL_THEME");
        Apply(string.IsNullOrEmpty(env) ? Prefs.theme : env, persist: false);
    }

    public static bool EffectiveIsDark(string theme)
    {
        if (theme == "light") return false;
        if (theme == "dark") return true;
        return SystemPrefersDark(); // auto
    }

    public static void Apply(string theme, bool persist = true)
    {
        if (theme != "light" && theme != "dark" && theme != "auto") theme = "auto";
        Current = theme;
        var dark = EffectiveIsDark(theme);
        var app = System.Windows.Application.Current;
        if (app is not null)
        {
            var uri = new Uri(dark ? "Palette.Dark.xaml" : "Palette.Light.xaml", UriKind.Relative);
            var dict = new ResourceDictionary { Source = uri };
            if (app.Resources.MergedDictionaries.Count > 0) app.Resources.MergedDictionaries[0] = dict;
            else app.Resources.MergedDictionaries.Add(dict);
        }
        if (persist) { Prefs.theme = theme; Prefs.Save(); }
        Changed?.Invoke();
    }

    private static bool SystemPrefersDark()
    {
        try
        {
            using var k = Microsoft.Win32.Registry.CurrentUser.OpenSubKey(
                @"Software\Microsoft\Windows\CurrentVersion\Themes\Personalize");
            var v = k?.GetValue("AppsUseLightTheme");
            if (v is int i) return i == 0;
        }
        catch { /* default dark */ }
        return true;
    }
}
