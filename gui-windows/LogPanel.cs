using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;

namespace Hull.Gui;

/// <summary>
/// Reusable live-log surface: appends classified lines, filters by level +
/// search, optional follow/auto-scroll. Used by the Logs screen and the Sites
/// → Logs tab.
/// </summary>
public sealed class LogPanel : Border
{
    private readonly StackPanel _host = new() { Margin = new Thickness(10, 8, 10, 8) };
    private readonly ScrollViewer _scroll;
    private readonly List<(string text, string level)> _all = new();
    private string _level = "all";
    private string _search = "";
    public bool Follow { get; set; } = true;

    public LogPanel()
    {
        Background = Ui.B("BgInset");
        BorderBrush = Ui.B("Border");
        BorderThickness = new Thickness(1);
        CornerRadius = new CornerRadius(8);
        _scroll = new ScrollViewer { VerticalScrollBarVisibility = ScrollBarVisibility.Auto, Content = _host };
        Child = _scroll;
    }

    public static string Classify(string msg)
    {
        var m = msg.ToLowerInvariant();
        if (m.Contains("error") || m.Contains("fatal") || m.Contains("panic") || m.Contains(" err ")) return "error";
        if (m.Contains("warn")) return "warn";
        return "info";
    }

    public void Append(string raw)
    {
        var level = Classify(raw);
        _all.Add((raw, level));
        if (_all.Count > 4000) _all.RemoveRange(0, 1000);
        if (Passes(raw, level)) AddLine(raw, level);
    }

    public void SetLevel(string level) { _level = level; Rerender(); }
    public void SetSearch(string search) { _search = search.Trim().ToLowerInvariant(); Rerender(); }

    public void Clear() { _all.Clear(); _host.Children.Clear(); }

    public void ShowHint(string text)
    {
        _host.Children.Clear();
        _host.Children.Add(new TextBlock { Text = text, Foreground = Ui.B("TextFaint"), FontFamily = Ui.Mono, FontSize = 12, Margin = new Thickness(2) });
    }

    private bool Passes(string raw, string level)
    {
        if (_level != "all" && level != _level) return false;
        if (_search.Length > 0 && !raw.ToLowerInvariant().Contains(_search)) return false;
        return true;
    }

    private void AddLine(string raw, string level)
    {
        var key = level switch { "error" => "Red", "warn" => "Amber", _ => "TextDim" };
        _host.Children.Add(new TextBlock
        {
            Text = raw,
            Foreground = Ui.B(key),
            FontFamily = Ui.Mono,
            FontSize = 12,
            TextWrapping = TextWrapping.NoWrap,
            Margin = new Thickness(0, 0.5, 0, 0.5),
        });
        if (Follow) _scroll.ScrollToEnd();
    }

    private void Rerender()
    {
        _host.Children.Clear();
        foreach (var (text, level) in _all) if (Passes(text, level)) AddLine(text, level);
    }
}
