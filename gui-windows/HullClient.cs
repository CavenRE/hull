using System.IO;
using System.Net.Http;
using System.Net.Http.Headers;
using System.Net.Http.Json;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace Hull.Gui;

/// <summary>A screen that can reload itself when the daemon pushes an event.</summary>
public interface IRefreshable
{
    Task RefreshAsync();
}

/// <summary>Discovery record written by a running daemon to ~/.hull/daemon.json.</summary>
public record DaemonInfo(int port, string token);

public record StatusInfo(
    string version, string tld, string[] roots,
    [property: JsonPropertyName("hull_home")] string hullHome);

/// <summary>One project as served by GET /v1/projects (see docs/api/openapi.yaml).</summary>
public record ProjectServiceInfo(string key, string engine, string? version, string mode, string? instance)
{
    public string ModeText => mode == "shared" ? "shared instance" : "dedicated container";
    public string Title => Friendly(engine) + (string.IsNullOrEmpty(version) ? "" : " " + version);
    public string Glyph => engine switch
    {
        "postgres" or "mysql" or "mariadb" => "database",
        "redis" or "memcached" => "cache",
        "mailpit" => "mail",
        "meilisearch" or "typesense" => "search",
        "minio" => "storage",
        _ => "tool",
    };
    private static string Friendly(string e) => e switch
    {
        "postgres" => "PostgreSQL", "mysql" => "MySQL", "mariadb" => "MariaDB", "redis" => "Redis",
        "mailpit" => "Mailpit", "meilisearch" => "Meilisearch", "minio" => "MinIO", _ => e,
    };
}

public record ProjectInfo(
    string name, string dir, string kind, string? url,
    bool running, string? php, bool served, string? group, string? error = null,
    ProjectServiceInfo[]? services = null)
{
    public string State => running ? "running" : "stopped";
    public bool IsFolder => kind == "folder";
    public string GroupName => string.IsNullOrEmpty(group) ? "Ungrouped" : group!;
    public ProjectServiceInfo[] LinkedServices => services ?? Array.Empty<ProjectServiceInfo>();
}

public record ServiceInfo(
    string name, string engine, string version, string container, bool running,
    string? url, string? host, int host_port, string? username, string[]? linked_projects)
{
    public string State => running ? "running" : "stopped";
    public string Endpoint => host_port > 0 ? $"{host ?? "127.0.0.1"}:{host_port}" : "";
    public string Linked => linked_projects is { Length: > 0 } ? "Linked  " + string.Join("  ", linked_projects) : "";
    public bool NotRunning => !running;
    public bool HasUrl => !string.IsNullOrEmpty(url);
    public bool HasPort => host_port > 0;
    public bool HasLinked => linked_projects is { Length: > 0 };
    public bool NoLinked => !HasLinked;
    public string HostText => host ?? "127.0.0.1";
    public string PortText => host_port > 0 ? host_port.ToString() : "";
    public string UserText => string.IsNullOrEmpty(username) ? "(none)" : username!;
    public string Badge => Friendly(engine) + (string.IsNullOrEmpty(version) || version == "latest" ? "" : " " + version);

    public string Glyph => engine switch
    {
        "postgres" or "mysql" or "mariadb" => "database",
        "redis" or "memcached" => "cache",
        "mailpit" => "mail",
        "meilisearch" or "typesense" => "search",
        "minio" => "storage",
        _ => "tool",
    };

    private static string Friendly(string e) => e switch
    {
        "postgres" => "PostgreSQL",
        "mysql" => "MySQL",
        "mariadb" => "MariaDB",
        "redis" => "Redis",
        "mailpit" => "Mailpit",
        "memcached" => "Memcached",
        "meilisearch" => "Meilisearch",
        "typesense" => "Typesense",
        "minio" => "MinIO",
        "adminer" => "Adminer",
        "redisinsight" => "RedisInsight",
        _ => e,
    };
}

public record Check(string name, string status, string detail);

public record Defaults(string php, string editor, string db_tool);

public record ConfigInfo(
    string tld, string[] roots, string? loopback, Defaults defaults, string[]? restart_required);

public record DependencyInfo(
    string name, string key, bool installed, string? version, bool running,
    string status, string blurb, string? install_url, string? install_hint, bool embedded);

/// <summary>
/// Thin client over the local hulld API. All logic lives in the daemon; this
/// only reads ~/.hull/daemon.json, authenticates with the bearer token, and
/// calls the frozen /v1 contract.
/// </summary>
public sealed class HullClient
{
    private static readonly JsonSerializerOptions JsonOpts = new() { PropertyNameCaseInsensitive = true };

    private readonly HttpClient _http;
    public int Port { get; }
    public string Token { get; }

    private HullClient(DaemonInfo info)
    {
        Port = info.port;
        Token = info.token;
        _http = new HttpClient { BaseAddress = new Uri($"http://127.0.0.1:{info.port}") };
        _http.DefaultRequestHeaders.Authorization = new AuthenticationHeaderValue("Bearer", info.token);
    }

    public static string DaemonFilePath()
    {
        var home = Environment.GetEnvironmentVariable("HULL_HOME");
        if (string.IsNullOrEmpty(home))
            home = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".hull");
        return Path.Combine(home, "daemon.json");
    }

    /// <summary>Reads the discovery file and verifies the daemon answers; null if not running.</summary>
    public static async Task<HullClient?> ConnectAsync(CancellationToken ct = default)
    {
        try
        {
            var path = DaemonFilePath();
            if (!File.Exists(path)) return null;
            var info = JsonSerializer.Deserialize<DaemonInfo>(await File.ReadAllTextAsync(path, ct), JsonOpts);
            if (info is null || info.port == 0) return null;
            var client = new HullClient(info);
            await client.StatusAsync(ct); // probe
            return client;
        }
        catch
        {
            return null;
        }
    }

    public Task<StatusInfo?> StatusAsync(CancellationToken ct = default) =>
        _http.GetFromJsonAsync<StatusInfo>("/v1/status", JsonOpts, ct);

    public async Task<List<ProjectInfo>> ProjectsAsync(CancellationToken ct = default) =>
        await _http.GetFromJsonAsync<List<ProjectInfo>>("/v1/projects", JsonOpts, ct) ?? new();

    public async Task ProjectActionAsync(string name, string action, CancellationToken ct = default)
    {
        var resp = await _http.PostAsync($"/v1/projects/{Uri.EscapeDataString(name)}/{action}", null, ct);
        resp.EnsureSuccessStatusCode();
    }

    public async Task<int> StopAllAsync(CancellationToken ct = default)
    {
        var resp = await _http.PostAsync("/v1/stop-all", null, ct);
        resp.EnsureSuccessStatusCode();
        var doc = await resp.Content.ReadFromJsonAsync<JsonElement>(cancellationToken: ct);
        return doc.TryGetProperty("stopped", out var s) ? s.GetInt32() : 0;
    }

    public async Task ShutdownAsync(CancellationToken ct = default)
    {
        try { await _http.PostAsync("/v1/shutdown", null, ct); } catch { /* daemon is exiting */ }
    }

    public async Task<List<ServiceInfo>> ServicesAsync(CancellationToken ct = default) =>
        await _http.GetFromJsonAsync<List<ServiceInfo>>("/v1/services", JsonOpts, ct) ?? new();

    public async Task ServiceActionAsync(string name, string action, CancellationToken ct = default)
    {
        var resp = await _http.PostAsync($"/v1/services/{Uri.EscapeDataString(name)}/{action}", null, ct);
        resp.EnsureSuccessStatusCode();
    }

    public Task<ConfigInfo?> ConfigAsync(CancellationToken ct = default) =>
        _http.GetFromJsonAsync<ConfigInfo>("/v1/config", JsonOpts, ct);

    public async Task PutConfigAsync(ConfigInfo cfg, CancellationToken ct = default)
    {
        var resp = await _http.PutAsJsonAsync("/v1/config", cfg, ct);
        resp.EnsureSuccessStatusCode();
    }

    public async Task<List<Check>> DoctorAsync(CancellationToken ct = default) =>
        await _http.GetFromJsonAsync<List<Check>>("/v1/doctor", JsonOpts, ct) ?? new();

    public async Task<List<DependencyInfo>> DependenciesAsync(CancellationToken ct = default) =>
        await _http.GetFromJsonAsync<List<DependencyInfo>>("/v1/dependencies", JsonOpts, ct) ?? new();

    /// <summary>
    /// Streams GET /v1/events (SSE). Invokes onRunning with the running compose
    /// project names on every event until cancelled. Token is passed via query
    /// since EventSource-style streams can't set headers.
    /// </summary>
    public async Task StreamEventsAsync(Action<List<string>> onRunning, CancellationToken ct)
    {
        var url = $"/v1/events?token={Uri.EscapeDataString(Token)}";
        using var stream = await _http.GetStreamAsync(url, ct);
        using var reader = new StreamReader(stream);
        while (!ct.IsCancellationRequested)
        {
            var line = await reader.ReadLineAsync(ct);
            if (line is null) break;
            if (!line.StartsWith("data:")) continue;
            var json = line[5..].Trim();
            if (json.Length == 0) continue;
            try
            {
                var ev = JsonSerializer.Deserialize<JsonElement>(json);
                if (ev.TryGetProperty("running", out var arr) && arr.ValueKind == JsonValueKind.Array)
                    onRunning(arr.EnumerateArray().Select(e => e.GetString() ?? "").ToList());
            }
            catch { /* ignore malformed event */ }
        }
    }
}
