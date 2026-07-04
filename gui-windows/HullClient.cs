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
    ProjectServiceInfo[]? services = null, ClusterRouteInfo[]? routes = null)
{
    public string State => running ? "running" : "stopped";
    public bool NotRunning => !running;
    public bool IsFolder => kind == "folder";
    public bool IsCluster => kind == "cluster";
    public string GroupName => string.IsNullOrEmpty(group) ? "Ungrouped" : group!;
    public ProjectServiceInfo[] LinkedServices => services ?? Array.Empty<ProjectServiceInfo>();
    public ClusterRouteInfo[] RouteList => routes ?? Array.Empty<ClusterRouteInfo>();
    public string DisplayKind => kind switch { "plain" => "php", _ => kind };
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
    public bool HasUser => !string.IsNullOrEmpty(username);
    public bool HasLinked => linked_projects is { Length: > 0 };
    public bool NoLinked => !HasLinked;
    public bool IsDatabase => engine is "postgres" or "mysql" or "mariadb";
    public string HostText => host ?? "127.0.0.1";
    public string PortText => host_port > 0 ? host_port.ToString() : "";
    public string PortLabel => host_port > 0 ? host_port.ToString() : "not running";
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

/// <summary>A started background job (image pull, create, rebuild, …).</summary>
public record JobInfo(string id, string kind, string status);
public record JobRef(JobInfo job);
public record ErrorEnvelope(string error);

public record Defaults(string php, string editor, string db_tool);

public record ConfigInfo(
    string tld, string[] roots, string? loopback, Defaults defaults, string[]? restart_required);

public record DependencyInfo(
    string name, string key, bool installed, string? version, bool running,
    string status, string blurb, string? install_url, string? install_hint, bool embedded);

public record ReapplyStep(string name, string status, string detail, string? manual);
public record ReapplyResult(ReapplyStep[]? steps);
public record RegistryImage(string name, string? description, bool official, int stars, string? pulls = null);

public record JobSummary(string id, string kind, string status, string? created, string[]? lines)
{
    public string Time => created is { Length: >= 19 } ? created[11..19] : "";
    public string Last => lines is { Length: > 0 } ? lines[^1] : "";
    public string DotState => status switch { "done" => "ok", "failed" => "error", _ => "warn" };
    public string Kindly => (kind ?? "").Replace('_', ' ').Replace('-', ' ');
}

public record RootGroups(string[]? groups);
public record GroupsStore(Dictionary<string, RootGroups>? roots, Dictionary<string, string>? members);

/// <summary>One cluster route (subdomain → service:port), from ProjectInfo.routes / GET /v1/clusters.</summary>
public record ClusterRouteInfo(string key, string subdomain, string service, int port, bool served,
    string[]? aliases = null, string[]? hosts = null)
{
    public string[] HostList => hosts ?? Array.Empty<string>();
    public string PrimaryUrl => HostList.Length > 0 ? "https://" + HostList[0] : "";
    public string AllUrls => string.Join("  ", HostList.Select(h => "https://" + h));
    public string AliasText => aliases is { Length: > 0 } ? string.Join(", ", aliases) : "";
    public string ServedText => served ? "served" : "not served";
}

/// <summary>One cluster from GET /v1/clusters.</summary>
public record ClusterInfo(
    string name, string dir, string? compose_root, bool running,
    string? base_domain, string? ingress, ClusterRouteInfo[]? routes)
{
    public string State => running ? "running" : "stopped";
    public ClusterRouteInfo[] RouteList => routes ?? Array.Empty<ClusterRouteInfo>();
    public int RouteCount => RouteList.Length;
    public string BaseDomainText => string.IsNullOrEmpty(base_domain) ? "(the TLD)" : base_domain!;
    public string IngressText => ingress switch
    {
        "hull" => "hull (Hull serves the URLs)",
        "delegate" => "delegate (ingress container)",
        _ => "none (the cluster serves itself)",
    };
    // Every fully-qualified URL across all routes, for the cluster console list.
    public IEnumerable<string> AllUrls =>
        RouteList.Where(r => r.served).SelectMany(r => r.HostList).Select(h => "https://" + h);
}

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

    public async Task CreateProjectAsync(object req, CancellationToken ct = default)
    {
        var resp = await _http.PostAsJsonAsync("/v1/projects", req, ct);
        resp.EnsureSuccessStatusCode();
    }

    public async Task ImportAsync(string name, CancellationToken ct = default)
    {
        var resp = await _http.PostAsJsonAsync("/v1/imports", new { name }, ct);
        resp.EnsureSuccessStatusCode();
    }

    public Task<GroupsStore?> GroupsAsync(CancellationToken ct = default) =>
        _http.GetFromJsonAsync<GroupsStore>("/v1/groups", JsonOpts, ct);

    public async Task PutGroupsAsync(GroupsStore store, CancellationToken ct = default)
    {
        var resp = await _http.PutAsJsonAsync("/v1/groups", store, ct);
        resp.EnsureSuccessStatusCode();
    }

    public async Task OpenAsync(string name, string target, CancellationToken ct = default)
    {
        var resp = await _http.PostAsJsonAsync($"/v1/projects/{Uri.EscapeDataString(name)}/open", new { target }, ct);
        resp.EnsureSuccessStatusCode();
    }

    public async Task<List<string>> VolumesAsync(string name, CancellationToken ct = default) =>
        await _http.GetFromJsonAsync<List<string>>($"/v1/projects/{Uri.EscapeDataString(name)}/volumes", JsonOpts, ct) ?? new();

    public async Task PatchProjectAsync(string name, object body, CancellationToken ct = default)
    {
        using var req = new HttpRequestMessage(HttpMethod.Patch, $"/v1/projects/{Uri.EscapeDataString(name)}") { Content = JsonContent.Create(body) };
        var resp = await _http.SendAsync(req, ct);
        resp.EnsureSuccessStatusCode();
    }

    public async Task AddServiceAsync(object req, CancellationToken ct = default)
    {
        var resp = await _http.PostAsJsonAsync("/v1/services", req, ct);
        resp.EnsureSuccessStatusCode();
    }

    public async Task DeleteServiceAsync(string name, CancellationToken ct = default)
    {
        var resp = await _http.DeleteAsync($"/v1/services/{Uri.EscapeDataString(name)}", ct);
        resp.EnsureSuccessStatusCode();
    }

    public async Task LinkServiceAsync(string name, string project, CancellationToken ct = default)
    {
        var resp = await _http.PostAsJsonAsync($"/v1/services/{Uri.EscapeDataString(name)}/link", new { project }, ct);
        resp.EnsureSuccessStatusCode();
    }

    public async Task<List<ServiceInfo>> ServicesAsync(CancellationToken ct = default) =>
        await _http.GetFromJsonAsync<List<ServiceInfo>>("/v1/services", JsonOpts, ct) ?? new();

    public async Task<List<ClusterInfo>> ClustersAsync(CancellationToken ct = default) =>
        await _http.GetFromJsonAsync<List<ClusterInfo>>("/v1/clusters", JsonOpts, ct) ?? new();

    /// <summary>Assigns a URL (subdomain) to a cluster service. body: {service, port, aliases?, serve?}.</summary>
    public async Task SetClusterRouteAsync(string name, string subdomain, object body, CancellationToken ct = default)
    {
        var resp = await _http.PutAsJsonAsync($"/v1/clusters/{Uri.EscapeDataString(name)}/routes/{Uri.EscapeDataString(subdomain)}", body, ct);
        await EnsureOkAsync(resp, ct);
    }

    public async Task RemoveClusterRouteAsync(string name, string subdomain, CancellationToken ct = default)
    {
        var resp = await _http.DeleteAsync($"/v1/clusters/{Uri.EscapeDataString(name)}/routes/{Uri.EscapeDataString(subdomain)}", ct);
        await EnsureOkAsync(resp, ct);
    }

    /// <summary>Sets a cluster's base_domain and/or ingress. body: {base_domain?, ingress?}.</summary>
    public async Task SetClusterConfigAsync(string name, object body, CancellationToken ct = default)
    {
        var resp = await _http.PutAsJsonAsync($"/v1/clusters/{Uri.EscapeDataString(name)}", body, ct);
        await EnsureOkAsync(resp, ct);
    }

    // EnsureOkAsync surfaces the daemon's {"error":...} message on failure.
    private static async Task EnsureOkAsync(HttpResponseMessage resp, CancellationToken ct)
    {
        if (resp.IsSuccessStatusCode) return;
        string msg = $"HTTP {(int)resp.StatusCode}";
        try { var e = await resp.Content.ReadFromJsonAsync<ErrorEnvelope>(JsonOpts, ct); if (!string.IsNullOrEmpty(e?.error)) msg = e!.error; } catch { }
        throw new HttpRequestException(msg);
    }

    /// <summary>
    /// Sends a request whose response may be a JobRef ({"job":{...}}). Returns
    /// the started job, or null for plain 2xx/204 responses. Throws the daemon's
    /// {"error":...} message on failure.
    /// </summary>
    public async Task<JobInfo?> SendForJobAsync(HttpMethod method, string path, object? body = null, CancellationToken ct = default)
    {
        using var req = new HttpRequestMessage(method, path);
        if (body is not null) req.Content = JsonContent.Create(body);
        var resp = await _http.SendAsync(req, ct);
        if (!resp.IsSuccessStatusCode)
        {
            string msg = $"HTTP {(int)resp.StatusCode}";
            try { var e = await resp.Content.ReadFromJsonAsync<ErrorEnvelope>(JsonOpts, ct); if (!string.IsNullOrEmpty(e?.error)) msg = e!.error; } catch { }
            throw new HttpRequestException(msg);
        }
        if (resp.StatusCode == System.Net.HttpStatusCode.NoContent) return null;
        try { var jr = await resp.Content.ReadFromJsonAsync<JobRef>(JsonOpts, ct); return jr?.job; } catch { return null; }
    }

    public Task<JobInfo?> PostForJobAsync(string path, object? body = null, CancellationToken ct = default) =>
        SendForJobAsync(HttpMethod.Post, path, body, ct);

    public Task<JobInfo?> DeleteForJobAsync(string path, CancellationToken ct = default) =>
        SendForJobAsync(HttpMethod.Delete, path, null, ct);

    public Task<JobInfo?> PatchForJobAsync(string path, object body, CancellationToken ct = default) =>
        SendForJobAsync(HttpMethod.Patch, path, body, ct);

    /// <summary>Streams container logs over SSE. query is e.g. "project=scratch&amp;tail=200".</summary>
    public async Task StreamLogsAsync(string query, Action<string> onLine, CancellationToken ct)
    {
        var url = $"/v1/logs?{query}&token={Uri.EscapeDataString(Token)}";
        using var stream = await _http.GetStreamAsync(url, ct);
        using var reader = new StreamReader(stream);
        while (!ct.IsCancellationRequested)
        {
            var line = await reader.ReadLineAsync(ct);
            if (line is null) break;
            if (line.StartsWith("data:")) onLine(line[5..].TrimStart());
        }
    }

    /// <summary>Streams a job's live log over SSE. onLine per data line; onDone(error,failed) when the job ends.</summary>
    public async Task StreamJobAsync(string id, Action<string> onLine, Action<string?, bool> onDone, CancellationToken ct)
    {
        var url = $"/v1/jobs/{Uri.EscapeDataString(id)}/stream?token={Uri.EscapeDataString(Token)}";
        using var stream = await _http.GetStreamAsync(url, ct);
        using var reader = new StreamReader(stream);
        string evt = "message";
        string data = "";
        while (!ct.IsCancellationRequested)
        {
            var line = await reader.ReadLineAsync(ct);
            if (line is null) break;
            if (line.Length == 0)
            {
                if (data.Length > 0 || evt != "message")
                {
                    if (evt == "done")
                    {
                        bool failed = false; string? err = null;
                        try { var j = JsonSerializer.Deserialize<JsonElement>(data); if (j.TryGetProperty("status", out var st)) failed = st.GetString() == "failed"; if (j.TryGetProperty("error", out var er)) err = er.GetString(); } catch { }
                        onDone(err, failed);
                    }
                    else if (data.Length > 0) onLine(data);
                }
                evt = "message"; data = "";
                continue;
            }
            if (line.StartsWith("event:")) evt = line[6..].Trim();
            else if (line.StartsWith("data:")) data = (data.Length > 0 ? data + "\n" : "") + line[5..].TrimStart();
        }
    }

    public async Task ServiceActionAsync(string name, string action, CancellationToken ct = default)
    {
        var resp = await _http.PostAsync($"/v1/services/{Uri.EscapeDataString(name)}/{action}", null, ct);
        resp.EnsureSuccessStatusCode();
    }

    public async Task<List<JobSummary>> JobsAsync(CancellationToken ct = default)
    {
        try { return await _http.GetFromJsonAsync<List<JobSummary>>("/v1/jobs", JsonOpts, ct) ?? new(); }
        catch { return new(); }
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

    public async Task<List<RegistryImage>> SearchImagesAsync(string q, CancellationToken ct = default)
    {
        try { return await _http.GetFromJsonAsync<List<RegistryImage>>($"/v1/registry/search?q={Uri.EscapeDataString(q)}", JsonOpts, ct) ?? new(); }
        catch { return new(); }
    }

    public async Task<List<string>> ImageTagsAsync(string repo, CancellationToken ct = default)
    {
        try { return await _http.GetFromJsonAsync<List<string>>($"/v1/registry/tags?repo={Uri.EscapeDataString(repo)}", JsonOpts, ct) ?? new(); }
        catch { return new(); }
    }

    public async Task<ReapplyResult?> ReapplyAsync(CancellationToken ct = default)
    {
        var resp = await _http.PostAsync("/v1/setup/reapply", null, ct);
        resp.EnsureSuccessStatusCode();
        return await resp.Content.ReadFromJsonAsync<ReapplyResult>(JsonOpts, ct);
    }

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
