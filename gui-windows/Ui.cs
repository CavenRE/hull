using System.Diagnostics;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Media;
using System.Windows.Shapes;

namespace Hull.Gui;

/// <summary>
/// Shared UI facade + widget factory (the WPF analogue of the Tauri `App.*`
/// helpers). Centralises toast/dialog/job plumbing and the reusable controls
/// every screen composes, so views stop reinventing them and stop swallowing
/// errors silently.
/// </summary>
public static class Ui
{
    public static MainWindow? Main => MainWindow.Current;
    public static HullClient? Client => Main?.Client;

    public static Brush B(string key) => (Brush)System.Windows.Application.Current.FindResource(key);
    public static Style S(string key) => (Style)System.Windows.Application.Current.FindResource(key);
    public static FontFamily Mono => (FontFamily)System.Windows.Application.Current.FindResource("FontMono");

    // ---- facade ------------------------------------------------------
    public static void Toast(string msg) => Main?.Toast(msg);
    public static void ShowDialog(FrameworkElement el) => Main?.ShowDialog(el);
    public static void CloseDialog() => Main?.CloseDialog();
    public static Task Reload() => Main?.Reload() ?? Task.CompletedTask;
    public static void Navigate(string route) => Main?.Navigate(route);

    /// <summary>Run an action; toast errors, toast okMsg + reload on success.</summary>
    public static Task Run(Func<Task> call, string okMsg = "") => Main?.Run(call, okMsg) ?? Task.CompletedTask;
    /// <summary>Run a job-returning action; stream it in the status bar or toast + reload.</summary>
    public static Task RunJob(Func<Task<JobInfo?>> call, string okMsg = "") => Main?.RunJob(call, okMsg) ?? Task.CompletedTask;

    public static void OpenExternal(string url)
    {
        try { Process.Start(new ProcessStartInfo(url) { UseShellExecute = true }); } catch { Toast("Couldn't open " + url); }
    }

    public static void Copy(string text)
    {
        try { Clipboard.SetText(text); Toast("Copied to clipboard"); } catch { /* clipboard busy */ }
    }

    public static string? PickFolder(string title)
    {
        try
        {
            var d = new Microsoft.Win32.OpenFolderDialog { Title = title, Multiselect = false };
            return d.ShowDialog() == true ? d.FolderName : null;
        }
        catch { return null; }
    }

    public static string? PickFile(string title, string filter)
    {
        try
        {
            var d = new Microsoft.Win32.OpenFileDialog { Title = title, Filter = filter };
            return d.ShowDialog() == true ? d.FileName : null;
        }
        catch { return null; }
    }

    // ---- widget factory ----------------------------------------------
    public static Icon Glyph(string glyph, double size, string brushKey) =>
        new() { Glyph = glyph, Width = size, Height = size, Brush = B(brushKey), VerticalAlignment = VerticalAlignment.Center };

    public static Ellipse Dot(string state)
    {
        var key = state switch { "running" => "Green", "error" => "Red", "ok" => "Green", "warn" => "Amber", _ => "TextFaint" };
        return new Ellipse { Width = 8, Height = 8, Fill = B(key), VerticalAlignment = VerticalAlignment.Center };
    }

    public static TextBlock SectionLabel(string text) => new()
    {
        Text = text.ToUpperInvariant(), Style = S("SectionLabel"),
    };

    public static TextBlock FieldLabel(string text) => new() { Text = text, Style = S("FieldLabel") };

    public static Border Chip(string text, bool accent = false)
    {
        var tb = new TextBlock { Text = text, FontSize = 11.5, Foreground = B(accent ? "Accent" : "TextDim"), VerticalAlignment = VerticalAlignment.Center };
        return new Border { Style = S(accent ? "ChipAccent" : "Chip"), Child = tb };
    }

    public static Button TextButton(string text, string glyph, string styleKey, RoutedEventHandler onClick, double iconSize = 14)
    {
        var sp = new StackPanel { Orientation = Orientation.Horizontal };
        if (!string.IsNullOrEmpty(glyph))
        {
            var fg = styleKey == "BtnPrimary" ? "TextOnAccent" : styleKey == "BtnDanger" ? "Red" : "Text";
            sp.Children.Add(Glyph(glyph, iconSize, fg));
            sp.Children.Add(new TextBlock { Text = text, Margin = new Thickness(6, 0, 0, 0), VerticalAlignment = VerticalAlignment.Center });
        }
        else sp.Children.Add(new TextBlock { Text = text, VerticalAlignment = VerticalAlignment.Center });
        var btn = new Button { Style = S(styleKey), Content = sp };
        btn.Click += onClick;
        return btn;
    }

    public static Button IconButton(string glyph, RoutedEventHandler onClick, double size = 16, string brushKey = "TextDim", string? tip = null)
    {
        var btn = new Button { Style = S("IconBtn"), Content = Glyph(glyph, size, brushKey) };
        if (tip is not null) btn.ToolTip = tip;
        btn.Click += onClick;
        return btn;
    }

    public static Button LinkButton(string text, string glyph, RoutedEventHandler onClick)
    {
        var sp = new StackPanel { Orientation = Orientation.Horizontal };
        if (!string.IsNullOrEmpty(glyph)) sp.Children.Add(Glyph(glyph, 13, "TextDim"));
        sp.Children.Add(new TextBlock { Text = text, Margin = new Thickness(glyph is null ? 0 : 6, 0, 0, 0), VerticalAlignment = VerticalAlignment.Center });
        var btn = new Button { Style = S("LinkBtn"), Content = sp };
        btn.Click += onClick;
        return btn;
    }

    /// <summary>Segmented control. Returns the container; reports the chosen index.</summary>
    public static Border Segmented(string[] options, int selected, Action<int> onSelect)
    {
        var panel = new StackPanel { Orientation = Orientation.Horizontal };
        var buttons = new List<Border>();
        void Paint()
        {
            for (int i = 0; i < buttons.Count; i++)
            {
                var on = i == selected;
                buttons[i].Background = on ? B("Accent") : Brushes.Transparent;
                ((TextBlock)buttons[i].Child).Foreground = on ? B("TextOnAccent") : B("TextDim");
            }
        }
        for (int i = 0; i < options.Length; i++)
        {
            int idx = i;
            var t = new TextBlock { Text = options[i], FontSize = 12.5, FontWeight = FontWeights.Medium, Padding = new Thickness(13, 5, 13, 5), VerticalAlignment = VerticalAlignment.Center };
            var b = new Border { CornerRadius = new CornerRadius(6), Cursor = Cursors.Hand, Child = t };
            b.MouseLeftButtonUp += (_, _) => { selected = idx; Paint(); onSelect(idx); };
            buttons.Add(b);
            panel.Children.Add(b);
        }
        Paint();
        return new Border { Background = B("BgInset"), BorderBrush = B("CtrlBorder"), BorderThickness = new Thickness(1), CornerRadius = new CornerRadius(8), Padding = new Thickness(3), Child = panel, HorizontalAlignment = HorizontalAlignment.Left };
    }

    /// <summary>Tab bar with an accent underline on the active tab.</summary>
    public static StackPanel Tabs(string[] labels, int selected, Action<int> onSelect)
    {
        var panel = new StackPanel { Orientation = Orientation.Horizontal };
        var unders = new List<Border>();
        var texts = new List<TextBlock>();
        void Paint()
        {
            for (int i = 0; i < unders.Count; i++)
            {
                var on = i == selected;
                unders[i].Background = on ? B("Accent") : Brushes.Transparent;
                texts[i].Foreground = on ? B("Text") : B("TextDim");
                texts[i].FontWeight = on ? FontWeights.SemiBold : FontWeights.Medium;
            }
        }
        for (int i = 0; i < labels.Length; i++)
        {
            int idx = i;
            var t = new TextBlock { Text = labels[i], FontSize = 13.5, Margin = new Thickness(0, 0, 0, 8), VerticalAlignment = VerticalAlignment.Center };
            var u = new Border { Height = 2, CornerRadius = new CornerRadius(1), VerticalAlignment = VerticalAlignment.Bottom };
            var stack = new StackPanel { Margin = new Thickness(0, 0, 22, 0) };
            stack.Children.Add(t);
            stack.Children.Add(u);
            var host = new Border { Cursor = Cursors.Hand, Child = stack };
            host.MouseLeftButtonUp += (_, _) => { selected = idx; Paint(); onSelect(idx); };
            unders.Add(u); texts.Add(t);
            panel.Children.Add(host);
        }
        Paint();
        return panel;
    }

    public static CheckBox Toggle(bool isOn, Action<bool> onChange)
    {
        var cb = new CheckBox { Style = S("ToggleSwitch"), IsChecked = isOn };
        cb.Checked += (_, _) => onChange(true);
        cb.Unchecked += (_, _) => onChange(false);
        return cb;
    }

    /// <summary>A label : value row with a copy button (the Services connection block).</summary>
    public static Grid ConnRow(string label, string value, bool faint = false)
    {
        var g = new Grid { Margin = new Thickness(0, 0, 0, 0), Height = 34 };
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(96) });
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        g.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        var l = new TextBlock { Text = label, Foreground = B("TextFaint"), FontSize = 12.5, VerticalAlignment = VerticalAlignment.Center };
        var v = new TextBlock { Text = value, Foreground = B(faint ? "TextFaint" : "Text"), FontFamily = Mono, FontSize = 12.5, FontStyle = faint ? FontStyles.Italic : FontStyles.Normal, VerticalAlignment = VerticalAlignment.Center };
        Grid.SetColumn(l, 0); Grid.SetColumn(v, 1);
        g.Children.Add(l); g.Children.Add(v);
        if (!faint)
        {
            var copy = IconButton("copy", (_, _) => Copy(value), 14, "TextFaint", "Copy");
            Grid.SetColumn(copy, 2); g.Children.Add(copy);
        }
        return g;
    }

    /// <summary>A bordered, mono, copyable code block.</summary>
    public static Border CodeBlock(string text)
    {
        var tb = new TextBox
        {
            Text = text, IsReadOnly = true, BorderThickness = new Thickness(0), Background = Brushes.Transparent,
            Foreground = B("TextDim"), FontFamily = Mono, FontSize = 12, TextWrapping = TextWrapping.Wrap,
            VerticalScrollBarVisibility = ScrollBarVisibility.Auto,
        };
        var copy = IconButton("copy", (_, _) => Copy(text), 14, "TextFaint", "Copy");
        copy.HorizontalAlignment = HorizontalAlignment.Right; copy.VerticalAlignment = VerticalAlignment.Top;
        var grid = new Grid();
        grid.Children.Add(tb);
        grid.Children.Add(copy);
        return new Border { Background = B("BgInset"), BorderBrush = B("Border"), BorderThickness = new Thickness(1), CornerRadius = new CornerRadius(8), Padding = new Thickness(12, 10, 12, 10), Child = grid };
    }

    /// <summary>Engine version picker (themed ComboBox; populated by caller).</summary>
    public static ComboBox VersionBox(IEnumerable<string> versions, string? selected = null, double width = 130)
    {
        var cb = new ComboBox { Style = S("Select"), ItemContainerStyle = S("SelectItem"), Width = width, IsEditable = false };
        foreach (var v in versions) cb.Items.Add(v);
        cb.SelectedItem = selected is not null && cb.Items.Contains(selected) ? selected : (cb.Items.Count > 0 ? cb.Items[0] : null);
        return cb;
    }

    public static ComboBox Select(IEnumerable<(string val, string text)> options, string? selectedVal = null, double width = double.NaN)
    {
        var cb = new ComboBox { Style = S("Select"), ItemContainerStyle = S("SelectItem") };
        if (!double.IsNaN(width)) cb.Width = width;
        foreach (var (val, text) in options) cb.Items.Add(new ComboBoxItem { Content = text, Tag = val });
        if (selectedVal is not null)
            foreach (ComboBoxItem it in cb.Items) if ((string)it.Tag == selectedVal) { cb.SelectedItem = it; break; }
        if (cb.SelectedItem is null && cb.Items.Count > 0) cb.SelectedIndex = 0;
        return cb;
    }

    public static string SelectedVal(ComboBox cb) =>
        cb.SelectedItem is ComboBoxItem it ? (string)it.Tag : (cb.SelectedItem?.ToString() ?? "");

    // ---- dialog scaffold ---------------------------------------------
    /// <summary>Builds a standard dialog card: title, body, footer buttons.</summary>
    public static Border Dialog(string title, UIElement body, params Button[] footer)
    {
        var head = new TextBlock { Text = title, FontSize = 16, FontWeight = FontWeights.SemiBold, Margin = new Thickness(0, 0, 0, 14) };
        var foot = new StackPanel { Orientation = Orientation.Horizontal, HorizontalAlignment = HorizontalAlignment.Right, Margin = new Thickness(0, 18, 0, 0) };
        foreach (var b in footer) { b.Margin = new Thickness(8, 0, 0, 0); foot.Children.Add(b); }
        var panel = new StackPanel { MinWidth = 420, MaxWidth = 560 };
        panel.Children.Add(head);
        panel.Children.Add(body);
        panel.Children.Add(foot);
        return new Border { Style = S("Card"), Padding = new Thickness(22), Child = panel, MaxHeight = 640 };
    }

    public static Button CancelButton(string text = "Cancel")
    {
        var b = new Button { Style = S("Btn"), Content = text };
        b.Click += (_, _) => CloseDialog();
        return b;
    }
}
