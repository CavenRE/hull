//go:build windows && installer

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	webview "github.com/jchv/go-webview2"
)

// runGUI shows the Hull-themed installer window (frameless, custom controls).
// The actual install runs in a goroutine and streams progress to the page.
func runGUI(def InstallOpts) {
	w := webview.NewWithOptions(webview.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		WindowOptions: webview.WindowOptions{
			Title:  "Install Hull",
			Width:  520,
			Height: 560,
			Center: true,
		},
	})
	if w == nil {
		// No WebView2 runtime — install headless so the user still ends up set up.
		_ = install(def, func(string, int) {})
		return
	}
	defer w.Destroy()

	hwnd := uintptr(w.Window())
	makeFrameless(hwnd)

	_ = w.Bind("winMin", func() { minimizeWindow(hwnd) })
	_ = w.Bind("winDrag", func() { startWindowDrag(hwnd) })
	_ = w.Bind("hullClose", func() { w.Terminate() })
	_ = w.Bind("hullOpen", func() {
		launchHull(def.Dir)
		w.Terminate()
	})
	_ = w.Bind("hullInstall", func(addPath, shortcuts, autostart bool) {
		go func() {
			o := InstallOpts{Dir: def.Dir, AddPath: addPath, Shortcuts: shortcuts, Autostart: autostart}
			err := install(o, func(msg string, pct int) {
				m, _ := json.Marshal(msg)
				w.Dispatch(func() { w.Eval(fmt.Sprintf("hullProgress(%d,%s)", pct, m)) })
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
	return strings.ReplaceAll(installerPage, "__DIR_JSON__", string(dirJSON))
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
    font-family:"Segoe UI",system-ui,sans-serif; font-size:14px;
    display:flex; justify-content:center; align-items:flex-start; user-select:none;
    cursor:default;
  }

  /* inline window controls (app-style) */
  .winctrls { position:fixed; top:0; right:0; display:flex; z-index:10; }
  .winbtn { width:44px; height:32px; display:grid; place-items:center; background:transparent;
            border:0; color:var(--faint); cursor:pointer; }
  .winbtn:hover { background:rgba(232,237,223,.07); color:var(--text); }
  .winbtn.close:hover { background:var(--red); color:#fff; }
  .winbtn svg { width:13px; height:13px; }

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
  <div class="winctrls">
    <button class="winbtn" id="winmin" title="Minimize"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M5 12h14"/></svg></button>
    <button class="winbtn close" id="winclose" title="Close"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M6 6 18 18M18 6 6 18"/></svg></button>
  </div>

  <div class="wrap">
    <div class="brand"><span class="logo">H</span>Hull</div>
    <div class="tag">A local environment for your sites &amp; apps — GUI, daemon and CLI.</div>

    <div id="opts">
      <div class="loc">Installs to <code id="dir"></code></div>
      <label class="opt"><input type="checkbox" id="path" checked> Add <b>hull</b> to your PATH</label>
      <label class="opt"><input type="checkbox" id="desktop" checked> Create a desktop shortcut</label>
      <label class="opt"><input type="checkbox" id="login"> Launch Hull at login</label>
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
  const show = id => views.forEach(v => document.getElementById(v).hidden = (v !== id));

  // window controls
  document.getElementById('winmin').onclick = () => winMin();
  document.getElementById('winclose').onclick = () => hullClose();
  // drag the window by the top header area (not interactive elements)
  document.addEventListener('mousedown', e => {
    if (e.button !== 0) return;
    if (e.target.closest('button, input, label, a, code')) return;
    if (e.clientY <= 120) winDrag();
  });

  document.getElementById('go').onclick = () => {
    show('prog');
    hullInstall(
      document.getElementById('path').checked,
      document.getElementById('desktop').checked,
      document.getElementById('login').checked
    );
  };
  window.hullProgress = (pct, msg) => {
    document.getElementById('fill').style.width = pct + '%';
    document.getElementById('status').textContent = msg;
  };
  window.hullDone = () => show('done');
  window.hullError = m => { document.getElementById('errmsg').textContent = m; show('err'); };
  document.getElementById('open').onclick = () => hullOpen();
  document.getElementById('close').onclick = () => hullClose();
  document.getElementById('closeerr').onclick = () => hullClose();
</script>
</body>
</html>`
