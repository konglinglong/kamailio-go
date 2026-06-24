// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * xhttp_prom module - Prometheus metrics endpoint.
 * Port of the kamailio xhttp_prom module (src/modules/xhttp_prom).
 *
 * The xhttp_prom module exposes Kamailio's internal counters, gauges and
 * histograms over HTTP in the Prometheus text exposition format. This Go
 * counterpart maintains an in-process metric registry, lets the script
 * register counter/gauge/histogram metrics and update them, and serves the
 * accumulated metrics on a configurable HTTP path.
 *
 * It is safe for concurrent use: the registry is guarded by a mutex.
 */

package xhttp_prom

import (
	"io"
	"math"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// PromConfig holds the configuration for an XHTTPPromModule, mirroring the
// modparams of the C xhttp_prom module (http_listen, prom_path).
type PromConfig struct {
	ListenAddr string
	Path       string
}

// PromMetric is the script-facing description of a single metric sample.
type PromMetric struct {
	Name   string
	Help   string
	Type   string
	Value  float64
	Labels map[string]string
}

const (
	metricCounter   = "counter"
	metricGauge     = "gauge"
	metricHistogram = "histogram"
)

// metricDef describes a registered metric.
type metricDef struct {
	name    string
	help    string
	mtype   string
	labels  []string // sorted alphabetically
	buckets []float64 // sorted ascending (histogram only)
	// counter/gauge: label-signature -> value
	values map[string]float64
	// histogram: label-signature -> sample
	hists map[string]*histSample
}

// histSample holds the per-label-set state of a histogram.
type histSample struct {
	buckets []uint64 // len = len(buckets)+1, last is +Inf
	sum     float64
	count   uint64
}

// XHTTPPromModule maintains a Prometheus metric registry and serves it
// over HTTP. It is the Go counterpart of the C xhttp_prom module.
type XHTTPPromModule struct {
	mu       sync.Mutex
	cfg      *PromConfig
	metrics  map[string]*metricDef
	listener net.Listener
	server   *http.Server
	running  bool
}

// New creates an XHTTPPromModule with empty metric storage.
func New() *XHTTPPromModule {
	return &XHTTPPromModule{metrics: make(map[string]*metricDef)}
}

// Init configures the module from cfg. A nil cfg applies empty defaults.
//
//	C: mod_init()
func (m *XHTTPPromModule) Init(cfg *PromConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg == nil {
		cfg = &PromConfig{}
	}
	m.cfg = cfg
	if m.metrics == nil {
		m.metrics = make(map[string]*metricDef)
	}
	return nil
}

// sortedLabels returns a copy of labels sorted alphabetically.
func sortedLabels(labels []string) []string {
	out := make([]string, len(labels))
	copy(out, labels)
	sort.Strings(out)
	return out
}

// register creates (or replaces) a metric definition.
func (m *XHTTPPromModule) register(name, help, mtype string, labels []string, buckets []float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.metrics == nil {
		m.metrics = make(map[string]*metricDef)
	}
	def := &metricDef{
		name:    name,
		help:    help,
		mtype:   mtype,
		labels:  sortedLabels(labels),
		buckets: append([]float64(nil), buckets...),
	}
	sort.Float64s(def.buckets)
	switch mtype {
	case metricCounter, metricGauge:
		def.values = make(map[string]float64)
	case metricHistogram:
		def.hists = make(map[string]*histSample)
	}
	m.metrics[name] = def
}

// RegisterCounter registers a counter metric.
func (m *XHTTPPromModule) RegisterCounter(name, help string, labels []string) {
	m.register(name, help, metricCounter, labels, nil)
}

// RegisterGauge registers a gauge metric.
func (m *XHTTPPromModule) RegisterGauge(name, help string, labels []string) {
	m.register(name, help, metricGauge, labels, nil)
}

// RegisterHistogram registers a histogram metric with the given buckets.
func (m *XHTTPPromModule) RegisterHistogram(name, help string, labels []string, buckets []float64) {
	m.register(name, help, metricHistogram, labels, buckets)
}

// signature builds the canonical label-value signature for a metric given
// a (possibly unordered) labels map. Values are extracted in the metric's
// sorted label order, so the same label set always yields the same key.
func (d *metricDef) signature(labels map[string]string) string {
	if len(d.labels) == 0 {
		return ""
	}
	var b strings.Builder
	for i, k := range d.labels {
		if i > 0 {
			b.WriteByte(0)
		}
		b.WriteString(labels[k])
	}
	return b.String()
}

// labelString renders the Prometheus label block for a sample, with labels
// in the metric's sorted order. Returns "" when there are no labels.
func (d *metricDef) labelString(labels map[string]string) string {
	if len(d.labels) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range d.labels {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteString(`="`)
		b.WriteString(labels[k])
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}

// Inc increments the counter identified by name by 1. Unknown metrics or
// non-counter metrics are silently ignored.
func (m *XHTTPPromModule) Inc(name string, labels map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.metrics[name]
	if !ok || d.mtype != metricCounter {
		return
	}
	sig := d.signature(labels)
	d.values[sig] = d.values[sig] + 1
}

// Set assigns value to the gauge identified by name. Unknown metrics or
// non-gauge metrics are silently ignored.
func (m *XHTTPPromModule) Set(name string, value float64, labels map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.metrics[name]
	if !ok || d.mtype != metricGauge {
		return
	}
	sig := d.signature(labels)
	d.values[sig] = value
}

// Observe records value into the histogram identified by name. Unknown
// metrics or non-histogram metrics are silently ignored.
func (m *XHTTPPromModule) Observe(name string, value float64, labels map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.metrics[name]
	if !ok || d.mtype != metricHistogram {
		return
	}
	sig := d.signature(labels)
	s := d.hists[sig]
	if s == nil {
		s = &histSample{buckets: make([]uint64, len(d.buckets)+1)}
		d.hists[sig] = s
	}
	for i, b := range d.buckets {
		if value <= b {
			s.buckets[i]++
		}
	}
	s.buckets[len(d.buckets)]++ // +Inf bucket
	s.sum += value
	s.count++
}

// formatFloat renders a float in Prometheus's minimal representation.
func formatFloat(v float64) string {
	if math.IsInf(v, 1) {
		return "+Inf"
	}
	if math.IsInf(v, -1) {
		return "-Inf"
	}
	if math.IsNaN(v) {
		return "NaN"
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// GetMetrics returns all registered metrics in the Prometheus text
// exposition format, sorted by metric name and label signature.
func (m *XHTTPPromModule) GetMetrics() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	names := make([]string, 0, len(m.metrics))
	for n := range m.metrics {
		names = append(names, n)
	}
	sort.Strings(names)

	var b strings.Builder
	for _, n := range names {
		d := m.metrics[n]
		b.WriteString("# HELP ")
		b.WriteString(d.name)
		b.WriteByte(' ')
		b.WriteString(d.help)
		b.WriteByte('\n')
		b.WriteString("# TYPE ")
		b.WriteString(d.name)
		b.WriteByte(' ')
		b.WriteString(d.mtype)
		b.WriteByte('\n')

		switch d.mtype {
		case metricCounter, metricGauge:
			sigs := make([]string, 0, len(d.values))
			for sig := range d.values {
				sigs = append(sigs, sig)
			}
			sort.Strings(sigs)
			for _, sig := range sigs {
				labels := d.labelsForSig(sig)
				b.WriteString(d.name)
				b.WriteString(d.labelString(labels))
				b.WriteByte(' ')
				b.WriteString(formatFloat(d.values[sig]))
				b.WriteByte('\n')
			}
		case metricHistogram:
			sigs := make([]string, 0, len(d.hists))
			for sig := range d.hists {
				sigs = append(sigs, sig)
			}
			sort.Strings(sigs)
			for _, sig := range sigs {
				labels := d.labelsForSig(sig)
				s := d.hists[sig]
				for i, bound := range d.buckets {
					b.WriteString(d.name)
					b.WriteString("_bucket")
					b.WriteString(d.bucketLabelString(labels, formatFloat(bound)))
					b.WriteByte(' ')
					b.WriteString(strconv.FormatUint(s.buckets[i], 10))
					b.WriteByte('\n')
				}
				b.WriteString(d.name)
				b.WriteString("_bucket")
				b.WriteString(d.bucketLabelString(labels, "+Inf"))
				b.WriteByte(' ')
				b.WriteString(strconv.FormatUint(s.buckets[len(d.buckets)], 10))
				b.WriteByte('\n')
				b.WriteString(d.name)
				b.WriteString("_sum")
				b.WriteString(d.labelString(labels))
				b.WriteByte(' ')
				b.WriteString(formatFloat(s.sum))
				b.WriteByte('\n')
				b.WriteString(d.name)
				b.WriteString("_count")
				b.WriteString(d.labelString(labels))
				b.WriteByte(' ')
				b.WriteString(strconv.FormatUint(s.count, 10))
				b.WriteByte('\n')
			}
		}
	}
	return b.String()
}

// labelsForSig reconstructs a labels map from a signature. Because the
// signature only encodes values (in sorted label order), the returned map
// maps each sorted label name to its value. This is sufficient for
// rendering since labelString iterates the same sorted order.
func (d *metricDef) labelsForSig(sig string) map[string]string {
	if len(d.labels) == 0 {
		return nil
	}
	parts := strings.Split(sig, "\x00")
	out := make(map[string]string, len(d.labels))
	for i, k := range d.labels {
		if i < len(parts) {
			out[k] = parts[i]
		}
	}
	return out
}

// bucketLabelString renders the label block for a histogram bucket line,
// appending the le="..." label after the metric's own labels.
func (d *metricDef) bucketLabelString(labels map[string]string, le string) string {
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range d.labels {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteString(`="`)
		b.WriteString(labels[k])
		b.WriteByte('"')
	}
	if len(d.labels) > 0 {
		b.WriteByte(',')
	}
	b.WriteString(`le="`)
	b.WriteString(le)
	b.WriteString(`"}`)
	return b.String()
}

// ServeHTTP implements http.Handler, serving the metrics at the configured
// path.
func (m *XHTTPPromModule) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	io.WriteString(w, m.GetMetrics())
}

// Start launches the HTTP server on the configured ListenAddr, serving
// metrics at the configured Path. Calling Start while already running
// stops the previous server first.
func (m *XHTTPPromModule) Start() error {
	m.Stop()
	m.mu.Lock()
	if m.cfg == nil {
		m.cfg = &PromConfig{}
	}
	addr := m.cfg.ListenAddr
	if addr == "" {
		addr = "0.0.0.0:9091"
	}
	path := m.cfg.Path
	if path == "" {
		path = "/metrics"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	mux := http.NewServeMux()
	mux.Handle(path, m)
	m.listener = ln
	m.server = &http.Server{Handler: mux}
	m.running = true
	ln = m.listener
	srv := m.server
	m.mu.Unlock()
	go srv.Serve(ln)
	return nil
}

// Stop shuts down the running HTTP server. It is idempotent.
func (m *XHTTPPromModule) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	ln := m.listener
	srv := m.server
	m.listener = nil
	m.server = nil
	m.mu.Unlock()
	if srv != nil {
		srv.Close()
	}
	if ln != nil {
		ln.Close()
	}
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *XHTTPPromModule
)

// DefaultXHTTPProm returns the process-wide XHTTPPromModule, creating it on
// first use.
func DefaultXHTTPProm() *XHTTPPromModule {
	defaultMu.RLock()
	mod := defaultM
	defaultMu.RUnlock()
	if mod != nil {
		return mod
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM == nil {
		defaultM = New()
	}
	return defaultM
}

// Init (re)initialises the process-wide XHTTPPromModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM != nil {
		defaultM.Stop()
	}
	defaultM = New()
}
