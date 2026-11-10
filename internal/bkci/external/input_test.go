package external

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/parsers/prometheus"
	"github.com/influxdata/telegraf/testutil"
)

func TestPrometheusNamesSurviveEnvMapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("# TYPE custom_temperature gauge\ncustom_temperature{disk=\"nvme0\"} 42.5\n"))
	}))
	defer srv.Close()
	parser := &prometheus.Parser{MetricVersion: 1}
	if err := parser.Init(); err != nil {
		t.Fatal(err)
	}
	i := &Input{URL: srv.URL, Mapping: "measurement_field"}
	i.SetParser(parser)
	if err := i.Init(); err != nil {
		t.Fatal(err)
	}
	var acc testutil.Accumulator
	if err := i.Gather(&acc); err != nil {
		t.Fatal(err)
	}
	if len(acc.Metrics) != 1 {
		t.Fatalf("metrics=%d", len(acc.Metrics))
	}
	m := acc.Metrics[0]
	if m.Measurement != "env" || m.Fields["custom_temperature"] != 42.5 || m.Tags["disk"] != "nvme0" {
		t.Fatalf("mapping=%+v", m)
	}
}
func TestOversizedHTTPResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(bytes.Repeat([]byte("x"), maxBody+1)) }))
	defer srv.Close()
	parser := &prometheus.Parser{}
	_ = parser.Init()
	i := &Input{URL: srv.URL}
	i.SetParser(parser)
	if err := i.Init(); err != nil {
		t.Fatal(err)
	}
	if err := i.Gather(&testutil.Accumulator{}); err == nil {
		t.Fatal("oversized HTTP body accepted")
	}
}
func TestHTTPTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	parser := &prometheus.Parser{}
	_ = parser.Init()
	i := &Input{URL: srv.URL, Timeout: config.Duration(30 * time.Millisecond)}
	i.SetParser(parser)
	if err := i.Init(); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := i.Gather(&testutil.Accumulator{}); err == nil {
		t.Fatal("timeout ignored")
	}
	if time.Since(started) > time.Second {
		t.Fatal("HTTP input blocked")
	}
}
func TestScriptFixture(t *testing.T) {
	if os.Getenv("BKCI_SCRIPT_TEST") == "" {
		return
	}
	for {
		time.Sleep(time.Hour)
	}
}
func TestScriptTimeout(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires delegated cgroup")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestScriptFixture$")
	start := time.Now()
	_, err := runCommand(cmd, []string{"BKCI_SCRIPT_TEST=1"})
	if err == nil {
		t.Fatal("timed out script succeeded")
	}
	if ctx.Err() != context.DeadlineExceeded {
		t.Fatalf("script ended before its timeout: %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("script timeout did not terminate process tree")
	}
}
func TestCappedOutput(t *testing.T) {
	b := &cappedBuffer{}
	if _, err := b.Write(bytes.Repeat([]byte("x"), maxBody)); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte("x")); err == nil {
		t.Fatal("unbounded script output")
	}
	if len(b.data) != maxBody {
		t.Fatal("buffer exceeded limit")
	}
}
