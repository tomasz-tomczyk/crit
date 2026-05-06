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
