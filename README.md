# Hull 🚢

Hull is a lightning-fast, highly modular, zero-config local development environment for Linux (Arch & Debian/Ubuntu). 

Inspired by tools like Laravel Valet and Sail, Hull leverages Docker and Caddy to instantly provision projects with auto-configured local domains (e.g., `*.test`), automatic SSL certificates, and zero port conflicts.

---

## ⚡ Features
* **One-Shot Scaffolding:** Spin up a new Laravel, WordPress, or Plain PHP project with a single command.
* **Global Routing:** A central Caddy reverse proxy automatically routes traffic to your containers. No more mapping `localhost:8080`.
* **Automatic SSL & DNS:** Installs a local Root CA and configures `dnsmasq` so `https://your-app.test` works out of the box.
* **Rootless & XDG Compliant:** Installs cleanly to `~/.hull` and runs containers securely mapped to your host user IDs.
* **Ephemeral Tooling:** Run `npm` or `composer` commands via temporary, disposable containers without polluting your host OS.

---

## 🚀 Installation

Hull features an OS-aware interactive setup wizard that automatically detects your system (Arch or Debian/Ubuntu), installs the required system dependencies (Docker, dnsmasq, fzf, etc.), and configures your local network.

Run the web installer directly from your terminal:

```bash
curl -sL [https://raw.githubusercontent.com/CavenRE/hull/main/install.sh](https://raw.githubusercontent.com/CavenRE/hull/main/install.sh) | bash
```

*During setup, you will be prompted to define your preferred `Sites` directory and your local TLD (default: `.test`).*

---

## 🛠️ Usage

Once installed, the `hull` command is available globally.

### Creating a New Project
```bash
hull new myapp --laravel
hull new myblog --wordpress
hull new api --plain
```
*This creates the folder, downloads the framework files, configures Docker, and boots the environment at `https://myapp.test`.*

### Managing Environments
Navigate to your project directory or use Hull's interactive menu from anywhere:
* `hull up` - Boots the environment (interactive if outside a project).
* `hull down` - Tears down the environment (interactive if outside a project).
* `hull status` - Displays a table of all active Hull containers and their ports.
* `hull logs` - Tails the Docker logs for the current project.
* `hull restart` - Restarts the current project or the global router.

### Developer Tooling
* `hull db` - Instantly opens the global Adminer database manager in your browser.
* `hull artisan <command>` - Runs an Artisan command natively against your active Laravel container.
* `hull npm <command>` - Runs an ephemeral Node container to execute NPM scripts (e.g., `hull npm run dev`).

---

## 🏗️ Architecture & Adding Templates

Hull is designed to be endlessly extensible. The core application lives in `~/.hull/`. 

To add a new framework (e.g., Node, Python, Go):
1. Create a new directory in `~/.hull/templates/` (e.g., `templates/node`).
2. Add a `compose.yaml` file to that directory.
3. Add the Hull labels to your web service so Caddy knows how to route it:
   ```yaml
   labels:
     caddy: {{SITE_NAME}}.{{TLD}}
     caddy.reverse_proxy: "{{upstreams 3000}}"
     caddy.tls: internal
   ```
4. Run `hull new my-app --node`!

---

## 🗑️ Uninstallation

If you ever need to remove Hull, the teardown script safely uninstalls the global infrastructure, removes the injected SSL certificates from your system trust stores, and cleans up the DNS overrides—all without touching your actual project files.

```bash
hull uninstall
```
