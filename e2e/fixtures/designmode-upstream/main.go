// Package main is the design-mode E2E upstream fixture.
// Single-file deterministic HTTP server with 9 routes used by the
// design-mode Playwright project. See plans/2026-05-06-design-mode-phase-f-e2e.md.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	port := flag.Int("port", 0, "TCP port to listen on (0 = pick a free port)")
	flag.Parse()

	mux := http.NewServeMux()
	registerRoot(mux)
	registerDashboard(mux)
	registerRedirectSame(mux)
	registerRedirectCross(mux)
	registerSPA(mux)
	registerMutator(mux)
	registerWS(mux)
	registerCookie(mux)
	registerSlow(mux)
	registerWidgets(mux)
	registerShiftMutator(mux)

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	// Stable stdout contract used by setup-fixtures-designmode.sh.
	fmt.Printf("listening on http://%s\n", ln.Addr().String())
	_ = os.Stdout.Sync()

	srv := &http.Server{Handler: mux}
	if err := srv.Serve(ln); err != nil && !strings.Contains(err.Error(), "Server closed") {
		log.Fatal(err)
	}
}

func registerRoot(mux *http.ServeMux) {
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, rootHTML)
	})
}

const rootHTML = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>upstream root</title></head>
<body>
<header id="page-header"><h1 id="title">Upstream</h1></header>
<main id="main">
  <ul class="card-list">
    <li class="card"><h2 class="card-title">Card A</h2><p>Body A</p></li>
    <li class="card"><h2 class="card-title">Card B</h2><p>Body B</p></li>
    <li class="card"><h2 class="card-title">Card C</h2><p>Body C</p></li>
  </ul>
  <button id="primary-btn">Primary</button>
  <button id="secondary-btn">Secondary</button>
  <a href="/dashboard" id="dash-link">Dashboard</a>
</main>
</body>
</html>`

func registerDashboard(mux *http.ServeMux) {
	mux.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, dashboardHTML)
	})
}

const dashboardHTML = `<!doctype html>
<html><head><meta charset="utf-8"><title>dashboard</title></head>
<body><main><h1 id="dash-title">Dashboard</h1><a href="/" id="home-link">Home</a></main></body></html>`

func registerRedirectSame(mux *http.ServeMux) {
	mux.HandleFunc("/redirect-same", func(w http.ResponseWriter, r *http.Request) {
		// Absolute URL pointing at upstream's own host so the proxy's
		// same-origin Location-rewrite path is exercised.
		target := "http://" + r.Host + "/dashboard"
		http.Redirect(w, r, target, http.StatusFound)
	})
}

func registerRedirectCross(mux *http.ServeMux) {
	mux.HandleFunc("/redirect-cross", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.invalid/x", http.StatusFound)
	})
}

func registerSPA(mux *http.ServeMux) {
	mux.HandleFunc("/spa", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, spaHTML)
	})
}

const spaHTML = `<!doctype html>
<html><head><meta charset="utf-8"><title>spa</title></head>
<body>
<main>
  <h1 id="spa-title">SPA root</h1>
  <button id="push-btn">Push /spa/section</button>
</main>
<script>
document.getElementById('push-btn').addEventListener('click', function () {
  history.pushState({}, '', '/spa/section');
  document.getElementById('spa-title').textContent = 'SPA section';
});
</script>
</body></html>`

const mutatorHTML = `<!doctype html>
<html><head><meta charset="utf-8"><title>mutator</title></head>
<body>
<main>
  <h1 id="mut-title">Mutator</h1>
  <ul id="mut-list">
    <li class="mut-item" data-stable="0">Stable item</li>
  </ul>
</main>
<script>
let i = 0;
setInterval(function () {
  const list = document.getElementById('mut-list');
  if (i % 2 === 0) {
    const li = document.createElement('li');
    li.className = 'mut-ephemeral';
    li.textContent = 'ephemeral ' + i;
    list.appendChild(li);
  } else {
    const e = list.querySelector('.mut-ephemeral');
    if (e) e.remove();
  }
  i++;
}, 200);
</script>
</body></html>`

func registerMutator(mux *http.ServeMux) {
	mux.HandleFunc("/mutator", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, mutatorHTML)
	})
}

var wsUpgrader = websocket.Upgrader{
	// Tests run on localhost; permissive origin check is fine here.
	CheckOrigin: func(r *http.Request) bool { return true },
}

func registerWS(mux *http.ServeMux) {
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			mt, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			if err := c.WriteMessage(mt, data); err != nil {
				return
			}
		}
	})
}

func registerCookie(mux *http.ServeMux) {
	mux.HandleFunc("/cookie", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "foo=bar; Domain=upstream.test; Path=/")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<!doctype html><html><body>cookie set</body></html>")
	})
}

// /widgets — page with an input, a draggable element, a focusable button, and
// a shadow-DOM host. Used by agent.designmode.spec for suppression carve-outs
// and shadow-DOM error emission.
const widgetsHTML = `<!doctype html>
<html><head><meta charset="utf-8"><title>widgets</title></head>
<body>
<main>
  <h1 id="widgets-title">Widgets</h1>
  <input id="widgets-input" type="text" placeholder="type here">
  <button id="widgets-btn" type="button">Activate</button>
  <div id="widgets-draggable" draggable="true" style="width:100px;height:32px;background:#eee;">drag me</div>
  <div id="shadow-host"></div>
</main>
<script>
(function () {
  var host = document.getElementById('shadow-host');
  var sr = host.attachShadow({ mode: 'open' });
  sr.innerHTML = '<button id="shadow-btn" style="padding:8px 16px;">in shadow</button>';
  // Track activations so tests can confirm suppression.
  window.__widgetsBtnActivations = 0;
  document.getElementById('widgets-btn').addEventListener('click', function () {
    window.__widgetsBtnActivations++;
  });
  window.__widgetsDragStarts = 0;
  var d = document.getElementById('widgets-draggable');
  d.addEventListener('dragstart', function () { window.__widgetsDragStarts++; });
}());
</script>
</body></html>`

func registerWidgets(mux *http.ServeMux) {
	mux.HandleFunc("/widgets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, widgetsHTML)
	})
}

// /shift-mutator — page with a stable target and a button that shifts the
// target down by appending a tall element above it. Used to verify marker
// reposition on DOM mutation.
const shiftMutatorHTML = `<!doctype html>
<html><head><meta charset="utf-8"><title>shift-mutator</title></head>
<body>
<main>
  <h1 id="sm-title">Shift Mutator</h1>
  <div id="sm-spacer-host"></div>
  <button id="sm-target" type="button">Target</button>
  <button id="sm-shift-btn" type="button">Shift target down</button>
  <button id="sm-mass-btn" type="button">Mass-mutate (300 nodes)</button>
  <button id="sm-class-btn" type="button">Thrash classes (no childList)</button>
</main>
<script>
(function () {
  document.getElementById('sm-shift-btn').addEventListener('click', function () {
    var host = document.getElementById('sm-spacer-host');
    var s = document.createElement('div');
    s.style.height = '120px';
    s.style.background = '#fafafa';
    s.textContent = 'spacer';
    host.appendChild(s);
  });
  document.getElementById('sm-mass-btn').addEventListener('click', function () {
    var host = document.getElementById('sm-spacer-host');
    var frag = document.createDocumentFragment();
    for (var i = 0; i < 300; i++) {
      var s = document.createElement('span');
      s.textContent = 'm' + i;
      frag.appendChild(s);
    }
    host.appendChild(frag);
  });
  document.getElementById('sm-class-btn').addEventListener('click', function () {
    var t = document.getElementById('sm-target');
    for (var i = 0; i < 50; i++) {
      t.classList.toggle('thrash-' + (i % 3));
    }
  });
}());
</script>
</body></html>`

func registerShiftMutator(mux *http.ServeMux) {
	mux.HandleFunc("/shift-mutator", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, shiftMutatorHTML)
	})
}

func registerSlow(mux *http.ServeMux) {
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<!doctype html><html><body>slow</body></html>")
	})
}
