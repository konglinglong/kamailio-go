// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - xhttp_prom module tests.
 */

package xhttp_prom

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestInit(t *testing.T) {
	m := New()
	if err := m.Init(&PromConfig{
		ListenAddr: "127.0.0.1:9091",
		Path:       "/metrics",
	}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if m.cfg == nil {
		t.Fatalf("cfg not set after Init()")
	}
	if m.cfg.ListenAddr != "127.0.0.1:9091" {
		t.Errorf("ListenAddr = %q", m.cfg.ListenAddr)
	}
	if m.cfg.Path != "/metrics" {
		t.Errorf("Path = %q", m.cfg.Path)
	}
	// nil config is accepted.
	if err := (&XHTTPPromModule{}).Init(nil); err != nil {
		t.Errorf("Init(nil) error = %v", err)
	}
}

func TestRegisterCounterAndInc(t *testing.T) {
	m := New()
	m.RegisterCounter("http_requests_total", "Total HTTP requests", []string{"method"})

	m.Inc("http_requests_total", map[string]string{"method": "get"})
	m.Inc("http_requests_total", map[string]string{"method": "get"})
	m.Inc("http_requests_total", map[string]string{"method": "post"})

	out := m.GetMetrics()
	if !strings.Contains(out, "# HELP http_requests_total Total HTTP requests") {
		t.Errorf("missing HELP line:\n%s", out)
	}
	if !strings.Contains(out, "# TYPE http_requests_total counter") {
		t.Errorf("missing TYPE counter line:\n%s", out)
	}
	if !strings.Contains(out, `http_requests_total{method="get"} 2`) {
		t.Errorf("missing counter value 2 for get:\n%s", out)
	}
	if !strings.Contains(out, `http_requests_total{method="post"} 1`) {
		t.Errorf("missing counter value 1 for post:\n%s", out)
	}
}

func TestRegisterGaugeAndSet(t *testing.T) {
	m := New()
	m.RegisterGauge("active_connections", "Active connections", nil)

	m.Set("active_connections", 42.5, nil)
	out := m.GetMetrics()
	if !strings.Contains(out, "# TYPE active_connections gauge") {
		t.Errorf("missing TYPE gauge line:\n%s", out)
	}
	if !strings.Contains(out, "active_connections 42.5") {
		t.Errorf("missing gauge value 42.5:\n%s", out)
	}

	// Update value.
	m.Set("active_connections", 7, nil)
	out = m.GetMetrics()
	if !strings.Contains(out, "active_connections 7") {
		t.Errorf("missing updated gauge value 7:\n%s", out)
	}
}

func TestRegisterHistogramAndObserve(t *testing.T) {
	m := New()
	m.RegisterHistogram("rpc_duration_seconds", "RPC duration",
		[]string{"service"}, []float64{0.5, 1.0, 2.0})

	// Values chosen to be exactly representable in float64 so the sum is
	// deterministic: 0.25 + 0.75 + 1.5 = 2.5.
	m.Observe("rpc_duration_seconds", 0.25, map[string]string{"service": "auth"})
	m.Observe("rpc_duration_seconds", 0.75, map[string]string{"service": "auth"})
	m.Observe("rpc_duration_seconds", 1.5, map[string]string{"service": "auth"})

	out := m.GetMetrics()
	if !strings.Contains(out, "# TYPE rpc_duration_seconds histogram") {
		t.Errorf("missing TYPE histogram line:\n%s", out)
	}
	// 0.25 <= 0.5; 0.75 <= 1.0; 1.5 <= 2.0.
	if !strings.Contains(out, `rpc_duration_seconds_bucket{service="auth",le="0.5"} 1`) {
		t.Errorf("missing bucket le=0.5 count 1:\n%s", out)
	}
	if !strings.Contains(out, `rpc_duration_seconds_bucket{service="auth",le="1"} 2`) {
		t.Errorf("missing bucket le=1 count 2:\n%s", out)
	}
	if !strings.Contains(out, `rpc_duration_seconds_bucket{service="auth",le="2"} 3`) {
		t.Errorf("missing bucket le=2 count 3:\n%s", out)
	}
	if !strings.Contains(out, `rpc_duration_seconds_bucket{service="auth",le="+Inf"} 3`) {
		t.Errorf("missing bucket le=+Inf count 3:\n%s", out)
	}
	if !strings.Contains(out, `rpc_duration_seconds_sum{service="auth"} 2.5`) {
		t.Errorf("missing sum 2.5:\n%s", out)
	}
	if !strings.Contains(out, `rpc_duration_seconds_count{service="auth"} 3`) {
		t.Errorf("missing count 3:\n%s", out)
	}
}

func TestGetMetricsSortedAndFormat(t *testing.T) {
	m := New()
	m.RegisterCounter("zebra_total", "zebra help", nil)
	m.RegisterCounter("alpha_total", "alpha help", nil)
	m.RegisterGauge("mid_gauge", "mid help", nil)
	m.Inc("zebra_total", nil)
	m.Inc("alpha_total", nil)
	m.Set("mid_gauge", 1, nil)

	out := m.GetMetrics()
	// Metrics should appear sorted by name.
	zi := strings.Index(out, "alpha_total")
	zj := strings.Index(out, "mid_gauge")
	zk := strings.Index(out, "zebra_total")
	if zi < 0 || zj < 0 || zk < 0 {
		t.Fatalf("missing metrics:\n%s", out)
	}
	if !(zi < zj && zj < zk) {
		t.Errorf("metrics not sorted by name:\n%s", out)
	}
}

func TestIncUnknownMetric(t *testing.T) {
	m := New()
	// Operating on an unregistered metric must be a safe no-op.
	m.Inc("nope", nil)
	m.Set("nope", 1, nil)
	m.Observe("nope", 1, nil)
	if out := m.GetMetrics(); strings.Contains(out, "nope") {
		t.Errorf("unknown metric should not appear:\n%s", out)
	}
}

func TestLabelOrdering(t *testing.T) {
	m := New()
	m.RegisterCounter("req_total", "requests", []string{"method", "code"})
	// Register with one label set; query with the same values in a
	// different map iteration order should hit the same series.
	m.Inc("req_total", map[string]string{"method": "get", "code": "200"})
	m.Inc("req_total", map[string]string{"code": "200", "method": "get"})

	out := m.GetMetrics()
	if !strings.Contains(out, `req_total{code="200",method="get"} 2`) {
		t.Errorf("labels not normalised to canonical order:\n%s", out)
	}
}

func TestStartStopLifecycle(t *testing.T) {
	m := New()
	if err := m.Init(&PromConfig{ListenAddr: "127.0.0.1:0", Path: "/metrics"}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	m.RegisterCounter("up", "up", nil)
	m.Inc("up", nil)
	if err := m.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	addr := m.listener.Addr().String()

	resp, err := http.Get("http://" + addr + "/metrics")
	if err != nil {
		t.Fatalf("http.Get error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "up 1") {
		t.Errorf("metrics endpoint missing 'up 1':\n%s", body)
	}
	m.Stop()
}

func TestDefaultAndInit(t *testing.T) {
	if DefaultXHTTPProm() == nil {
		t.Fatalf("DefaultXHTTPProm() nil")
	}
	Init()
	d := DefaultXHTTPProm()
	if d == nil {
		t.Fatalf("DefaultXHTTPProm() nil after Init")
	}
	if d != DefaultXHTTPProm() {
		t.Fatalf("DefaultXHTTPProm() returned different instances")
	}
}

func TestConcurrentMetrics(t *testing.T) {
	m := New()
	m.RegisterCounter("c_total", "c", []string{"k"})
	m.RegisterGauge("g", "g", nil)
	m.RegisterHistogram("h", "h", nil, []float64{0.5, 1.0})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.Inc("c_total", map[string]string{"k": itoa(i % 3)})
			m.Set("g", float64(i), nil)
			m.Observe("h", float64(i)/10.0, nil)
			_ = m.GetMetrics()
		}(i)
	}
	wg.Wait()

	out := m.GetMetrics()
	if !strings.Contains(out, "h_count") {
		t.Errorf("histogram missing after concurrent ops:\n%s", out)
	}
}

// itoa is a tiny local int->string helper to avoid pulling strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
