//go:build linux && installer

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	webview "github.com/webview/webview_go"
)

// runGUI shows the Hull-themed installer window (WebKitGTK). The install runs in
// a goroutine and streams progress to the page. Unlike the Windows WebView2
// build there's no frameless/custom-chrome mode here, so the window keeps its
// native title bar , which is the expected look on Linux desktops anyway.
func runGUI(def InstallOpts) {
	w := webview.New(false)
	if w == nil {
		// No display / WebKitGTK , fall back to a headless install so the user
		// still ends up set up.
		_ = install(def, func(string, int) {})
		return
	}
	defer w.Destroy()

	w.SetTitle("Install Hull")
	w.SetSize(520, 540, webview.HintFixed)

	_ = w.Bind("fitWindow", func(h int) {
		if h < 200 {
			h = 200
		}
		w.Dispatch(func() { w.SetSize(520, h, webview.HintFixed) })
	})
	_ = w.Bind("hullClose", func() { w.Terminate() })
	_ = w.Bind("hullOpen", func() {
		launchHull(def.Dir)
		w.Terminate()
	})
	_ = w.Bind("hullInstall", func(gui, addPath, menu, autostart, service bool) {
		go func() {
			o := InstallOpts{
				Dir: def.Dir, GUI: gui, AddPath: addPath,
				Menu: menu, Autostart: autostart, Service: service,
			}
			err := install(o, func(msg string, pct int) {
				m, _ := json.Marshal(msg)
				w.Dispatch(func() { w.Eval(fmt.Sprintf("hullProgress(%d,%s)", pct, string(m))) })
			})
			w.Dispatch(func() {
				if err != nil {
					em, _ := json.Marshal(err.Error())
					w.Eval("hullError(" + string(em) + ")")
				} else {
					w.Eval("hullDone()")
				}
			})
		}()
	})

	w.SetHtml(installerHTML(def.Dir))
	w.Run()
}

func installerHTML(dir string) string {
	dirJSON, _ := json.Marshal(dir)
	return strings.Replace(installerPage, "__DIR_JSON__", string(dirJSON), 1)
}

const installerPage = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>
  :root {
    --bg:#242423; --line:rgba(232,237,223,.12);
    --text:#e8eddf; --dim:#b3b9ac; --faint:#7e8377;
    --gold:#f5cb5c; --gold-press:#e6b942; --ink:#242423; --green:#7fb069; --red:#e0685f;
    --mono:ui-monospace,"Cascadia Code",Consolas,monospace;
  }
  * { box-sizing:border-box; }
  html,body { height:100%; margin:0; }
  body {
    background:var(--bg); color:var(--text);
    font-family:"Inter",system-ui,sans-serif; font-size:14px;
    display:flex; justify-content:center; align-items:flex-start; user-select:none;
    cursor:default;
  }
  .wrap { width:472px; padding:30px 36px 36px; }
  .brand { display:flex; align-items:center; gap:12px; font-size:25px; font-weight:700; letter-spacing:-.02em; }
  .logo { width:38px; height:38px; border-radius:11px; background:var(--gold); color:var(--ink);
          display:grid; place-items:center; font-weight:800; font-size:23px; }
  .tag { color:var(--dim); margin:10px 0 24px; }
  .loc { color:var(--faint); font-size:12.5px; margin-bottom:16px; }
  .loc code { font-family:var(--mono); color:var(--dim); word-break:break-all; }
  .opt { display:flex; align-items:center; gap:11px; padding:11px 0; border-top:1px solid var(--line);
         cursor:pointer; font-size:14px; }
  .opt:last-of-type { border-bottom:1px solid var(--line); }
  .opt input { width:17px; height:17px; accent-color:var(--gold); cursor:pointer; }
  .opt b { font-family:var(--mono); }
  .opt .sub { color:var(--faint); font-size:12.5px; }
  .btn { appearance:none; border:0; border-radius:9px; background:var(--gold); color:var(--ink);
         font-weight:600; font-size:14.5px; padding:12px 20px; cursor:pointer; margin-top:24px; width:100%; }
  .btn:hover { background:var(--gold-press); }
  .btn.ghost { background:transparent; color:var(--dim); border:1px solid var(--line); }
  .btn.ghost:hover { color:var(--text); }
  .row { display:flex; gap:10px; }
  .row .btn { margin-top:0; }
  .bar { height:8px; border-radius:999px; background:#1d1e1c; overflow:hidden; margin:40px 0 12px; }
  .fill { height:100%; width:0; background:var(--gold); transition:width .25s ease; }
  .status { color:var(--dim); font-size:13px; min-height:18px; }
  .check { width:54px; height:54px; border-radius:50%; background:var(--green); color:#fff;
           display:grid; place-items:center; font-size:30px; margin:24px auto 14px; }
  .donemsg { text-align:center; font-size:18px; font-weight:600; margin-bottom:22px; }
  .errmsg { color:var(--red); background:rgba(224,104,95,.12); border:1px solid rgba(224,104,95,.35);
            border-radius:9px; padding:12px 14px; margin:18px 0; font-size:13px; word-break:break-word; }
  [hidden] { display:none !important; }
</style>
</head>
<body>
  <div class="wrap">
    <div class="brand"><span class="logo">H</span>Hull</div>
    <div class="tag">A local environment for your sites &amp; apps , GUI, daemon and CLI.</div>

    <div id="opts">
      <div class="loc">Installs to <code id="dir"></code></div>
      <label class="opt"><input type="checkbox" id="gui" checked> Install the desktop app <span class="sub">, uncheck for CLI only</span></label>
      <label class="opt"><input type="checkbox" id="path" checked> Add <b>hull</b> to your PATH</label>
      <label class="opt gui-only"><input type="checkbox" id="menu" checked> Add to the applications menu</label>
      <label class="opt gui-only"><input type="checkbox" id="login"> Launch Hull at login</label>
      <label class="opt"><input type="checkbox" id="service"> Run the daemon in the background <span class="sub">, systemd&nbsp;user service</span></label>
      <button class="btn" id="go">Install Hull</button>
    </div>

    <div id="prog" hidden>
      <div class="bar"><div class="fill" id="fill"></div></div>
      <div class="status" id="status">Starting…</div>
    </div>

    <div id="done" hidden>
      <div class="check">&#10003;</div>
      <div class="donemsg">Hull installed</div>
      <div class="row"><button class="btn" id="open">Open Hull</button><button class="btn ghost" id="close">Close</button></div>
    </div>

    <div id="err" hidden>
      <div class="errmsg" id="errmsg"></div>
      <button class="btn ghost" id="closeerr">Close</button>
    </div>
  </div>

<script>
  const DIR = __DIR_JSON__;
  document.getElementById('dir').textContent = DIR;
  const views = ['opts','prog','done','err'];
  const fit = () => requestAnimationFrame(() => {
    const h = Math.ceil(document.querySelector('.wrap').getBoundingClientRect().height) + 8;
    if (window.fitWindow) fitWindow(h);
  });
  const show = id => { views.forEach(v => document.getElementById(v).hidden = (v !== id)); fit(); };

  // Desktop-app toggle: CLI-only hides the GUI-specific options.
  const guiBox = document.getElementById('gui');
  guiBox.addEventListener('change', () => {
    document.querySelectorAll('.gui-only').forEach(el => el.hidden = !guiBox.checked);
    fit();
  });

  let installedGui = true;
  document.getElementById('go').onclick = () => {
    installedGui = guiBox.checked;
    show('prog');
    hullInstall(
      guiBox.checked,
      document.getElementById('path').checked,
      document.getElementById('menu').checked,
      document.getElementById('login').checked,
      document.getElementById('service').checked
    );
  };
  window.hullProgress = (pct, msg) => {
    document.getElementById('fill').style.width = pct + '%';
    document.getElementById('status').textContent = msg;
  };
  window.hullDone = () => { document.getElementById('open').hidden = !installedGui; show('done'); };
  window.hullError = m => { document.getElementById('errmsg').textContent = m; show('err'); };
  document.getElementById('open').onclick = () => hullOpen();
  document.getElementById('close').onclick = () => hullClose();
  document.getElementById('closeerr').onclick = () => hullClose();

  window.addEventListener('load', fit);
  fit();
</script>
</body>
</html>`
