namespace Hull.Gui;

/// <summary>
/// Static engine + version catalog, mirrored from gui/dist/data.js so the
/// Add-instance and New-project flows have offline defaults. Live versions can
/// later come from /v1/registry/*; these are the curated fallbacks.
/// </summary>
public static class Catalog
{
    public record EngineMeta(string Label, string Category, string Icon, bool IsDatabase);
    public record Item(string Engine, string Blurb, string[] Versions);
    public record Group(string Category, Item[] Items);

    public static readonly Dictionary<string, EngineMeta> Engines = new()
    {
        ["postgres"]   = new("PostgreSQL", "Database", "database", true),
        ["mysql"]      = new("MySQL", "Database", "database", true),
        ["mariadb"]    = new("MariaDB", "Database", "database", true),
        ["redis"]      = new("Redis", "Cache", "cache", false),
        ["memcached"]  = new("Memcached", "Cache", "cache", false),
        ["meilisearch"]= new("Meilisearch", "Search", "search2", false),
        ["typesense"]  = new("Typesense", "Search", "search2", false),
        ["minio"]      = new("MinIO", "Storage", "storage", false),
        ["mailpit"]    = new("Mailpit", "Mail", "mail", false),
        ["adminer"]    = new("Adminer", "Tool", "tool", false),
        ["sqlite"]     = new("SQLite", "Database", "database", true),
    };

    public static readonly Group[] Groups =
    {
        new("Database", new[]
        {
            new Item("postgres", "Object-relational SQL database.", new[] { "16", "15", "14" }),
            new Item("mysql", "The world's most-used SQL database.", new[] { "8.4", "8.0" }),
            new Item("mariadb", "Community MySQL fork.", new[] { "lts", "11", "10.11" }),
        }),
        new("Cache", new[]
        {
            new Item("redis", "In-memory key-value store.", new[] { "alpine", "7", "6" }),
            new Item("memcached", "Distributed memory cache.", new[] { "alpine" }),
        }),
        new("Search", new[]
        {
            new Item("meilisearch", "Lightning-fast full-text search.", new[] { "v1.11" }),
            new Item("typesense", "Typo-tolerant search engine.", new[] { "27.1" }),
        }),
        new("Storage", new[] { new Item("minio", "S3-compatible object storage.", new[] { "latest" }) }),
        new("Mail", new[] { new Item("mailpit", "Catches outgoing mail for testing.", new[] { "latest" }) }),
        new("Tool", new[] { new Item("adminer", "Web database management UI.", new[] { "latest" }) }),
    };

    public static string Label(string engine) => Engines.TryGetValue(engine, out var m) ? m.Label : engine;
    public static string IconFor(string engine) => Engines.TryGetValue(engine, out var m) ? m.Icon : "tool";
    public static bool IsDatabase(string engine) => Engines.TryGetValue(engine, out var m) && m.IsDatabase;

    // Popular Docker Hub images for the App/Cluster container search (offline fallback).
    public record Image(string Name, string Desc, bool Official, string? Ns, string Pulls);
    public static readonly Image[] DockerImages =
    {
        new("node", "Node.js JavaScript runtime", true, null, "1B+"),
        new("python", "Python interpreter + pip", true, null, "1B+"),
        new("php", "PHP with FPM / CLI variants", true, null, "500M+"),
        new("nginx", "High-performance web server", true, null, "1B+"),
        new("redis", "In-memory data store", true, null, "1B+"),
        new("postgres", "PostgreSQL relational database", true, null, "1B+"),
        new("mysql", "MySQL relational database", true, null, "1B+"),
        new("golang", "Go toolchain", true, null, "500M+"),
        new("ruby", "Ruby interpreter + bundler", true, null, "100M+"),
        new("caddy", "Web server with automatic HTTPS", true, null, "100M+"),
        new("alpine", "Minimal Linux base image", true, null, "1B+"),
        new("oven/bun", "Fast all-in-one JS runtime", false, "oven/bun", "10M+"),
    };
}
