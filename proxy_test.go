package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestProxyDirector_StripsAcceptEncoding(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, "<html><body>ok</body></html>")
	}))
	defer upstream.Close()

	proxy, _ := newDesignProxy(upstream.URL, 9001)
	ps := httptest.NewServer(proxy)
	defer ps.Close()

	req, _ := http.NewRequest("GET", ps.URL+"/", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d; Accept-Encoding was not stripped", resp.StatusCode)
	}
}

func TestProxyDirector_PreservesCookieAndAuth(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "session=abc" || r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, "<html><body>ok</body></html>")
	}))
	defer upstream.Close()

	proxy, _ := newDesignProxy(upstream.URL, 9001)
	ps := httptest.NewServer(proxy)
	defer ps.Close()

	req, _ := http.NewRequest("GET", ps.URL+"/", nil)
	req.Header.Set("Cookie", "session=abc")
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d; Cookie/Authorization not forwarded", resp.StatusCode)
	}
}

func TestProxyDirector_SetsUpstreamHost(t *testing.T) {
	var gotHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, "<html><body>ok</body></html>")
	}))
	defer upstream.Close()
	upstreamURL, _ := url.Parse(upstream.URL)

	proxy, _ := newDesignProxy(upstream.URL, 9001)
	ps := httptest.NewServer(proxy)
	defer ps.Close()

	resp, _ := http.Get(ps.URL + "/")
	if resp != nil {
		resp.Body.Close()
	}
	if gotHost != upstreamURL.Host {
		t.Errorf("Host = %q, want %q", gotHost, upstreamURL.Host)
	}
}

// Suppress unused
var _ = io.ReadAll
var _ = strings.Contains
var _ = net.Listen
