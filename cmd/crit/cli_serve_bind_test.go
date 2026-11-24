package main

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/config"
)

// freePortBlock returns the first of n consecutive loopback ports that were
// free a moment ago. It skips the test when it can't find such a block.
func freePortBlock(t *testing.T, n int) int {
	t.Helper()
	for attempt := 0; attempt < 20; attempt++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		base := ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()
		if base+n-1 > 65535 {
			continue
		}
		if blockFree(base, n) {
			return base
		}
	}
	t.Skipf("no block of %d free loopback ports found", n)
	return 0
}

func blockFree(base, n int) bool {
	var held []net.Listener
	defer func() {
		for _, l := range held {
			_ = l.Close()
		}
	}()
	for p := base; p < base+n; p++ {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err != nil {
			return false
		}
		held = append(held, l)
	}
	return true
}

// occupy holds a loopback port until the test ends.
func occupy(t *testing.T, port int) {
	t.Helper()
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Skipf("port %d taken before the test could hold it: %v", port, err)
	}
	t.Cleanup(func() { _ = l.Close() })
}

func listenerPort(l net.Listener) int {
	return l.Addr().(*net.TCPAddr).Port
}

func closeAll(ls ...net.Listener) {
	for _, l := range ls {
		if l != nil {
			_ = l.Close()
		}
	}
}

func TestBindPorts(t *testing.T) {
	tests := []struct {
		name      string
		size      int   // ports in the requested range (1 = single port)
		busy      []int // offsets from the base held by another listener
		withProxy bool
		wantAPI   int // offset of the API port; -1 means an error is expected
		wantErr   []string
	}{
		{name: "single port", size: 1, wantAPI: 0},
		{name: "range binds first free port", size: 3, busy: []int{0}, wantAPI: 1},
		{name: "range all busy", size: 2, busy: []int{0, 1}, wantAPI: -1, wantErr: []string{"no free port in", "last error"}},
		{name: "live pair in range skips busy proxy port", size: 4, busy: []int{1}, withProxy: true, wantAPI: 2},
		{name: "live pair needs two ports inside range", size: 2, busy: []int{1}, withProxy: true, wantAPI: -1, wantErr: []string{"two consecutive free ports", "last error"}},
		{name: "live single port", size: 1, withProxy: true, wantAPI: 0},
		{name: "live single port with busy proxy port", size: 1, busy: []int{1}, withProxy: true, wantAPI: -1, wantErr: []string{"already in use"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := freePortBlock(t, tt.size+1)
			for _, off := range tt.busy {
				occupy(t, base+off)
			}
			req := config.PortRange{First: base, Last: base + tt.size - 1}

			api, proxy, err := bindPorts("127.0.0.1", req, tt.withProxy)
			defer closeAll(api, proxy)

			if tt.wantAPI < 0 {
				if err == nil {
					t.Fatalf("bindPorts(%v) bound %d, want error", req, listenerPort(api))
				}
				for _, s := range tt.wantErr {
					if !strings.Contains(err.Error(), s) {
						t.Errorf("error %q does not contain %q", err, s)
					}
				}
				if req.IsRange() && !strings.Contains(err.Error(), req.String()) {
					t.Errorf("error %q does not name the range %s", err, req)
				}
				return
			}
			if err != nil {
				t.Fatalf("bindPorts(%v): %v", req, err)
			}
			if got := listenerPort(api); got != base+tt.wantAPI {
				t.Errorf("api port = %d, want %d", got, base+tt.wantAPI)
			}
			if !tt.withProxy {
				if proxy != nil {
					t.Errorf("proxy listener bound without live mode")
				}
				return
			}
			if proxy == nil {
				t.Fatal("proxy listener is nil in live mode")
			}
			if got := listenerPort(proxy); got != listenerPort(api)+1 {
				t.Errorf("proxy port = %d, want %d", got, listenerPort(api)+1)
			}
		})
	}
}

func TestBindPorts_FixedLivePortWaitsForProxyPort(t *testing.T) {
	base := freePortBlock(t, 2)
	held, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", base+1))
	if err != nil {
		t.Skipf("port %d taken: %v", base+1, err)
	}
	// Release between the first attempt (0 ms) and the second (500 ms): the
	// first attempt fails, and a later one must bind both ports.
	released := make(chan struct{})
	go func() {
		time.Sleep(250 * time.Millisecond)
		_ = held.Close()
		close(released)
	}()
	defer func() { <-released }()

	api, proxy, err := bindPorts("127.0.0.1", config.SinglePort(base), true)
	if err != nil {
		t.Fatalf("bindPorts: %v", err)
	}
	defer closeAll(api, proxy)
	if listenerPort(api) != base || listenerPort(proxy) != base+1 {
		t.Fatalf("bound %d/%d, want %d/%d", listenerPort(api), listenerPort(proxy), base, base+1)
	}
}

func TestProxyListenHost(t *testing.T) {
	tests := []struct{ host, want string }{
		{host: "", want: "127.0.0.1"},
		{host: "127.0.0.1", want: "127.0.0.1"},
		{host: "0.0.0.0", want: "0.0.0.0"},
		{host: "::1", want: "::1"},
	}
	for _, tt := range tests {
		if got := proxyListenHost(tt.host); got != tt.want {
			t.Errorf("proxyListenHost(%q) = %q, want %q", tt.host, got, tt.want)
		}
	}
}

func TestBindPorts_RandomPort(t *testing.T) {
	api, proxy, err := bindPorts("127.0.0.1", config.PortRange{}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeAll(api, proxy)
	if listenerPort(api) == 0 || proxy != nil {
		t.Fatalf("api port %d, proxy %v", listenerPort(api), proxy)
	}
}
