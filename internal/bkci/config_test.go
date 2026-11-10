package bkci

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/models"
)

func TestNativeHTTPWireContract(t *testing.T) {
	for _, project := range []string{"bkci-demo", "git_stream"} {
		t.Run(project, func(t *testing.T) {
			received := make(map[string][]byte)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-DEVOPS-AGENT-ID") != "a1" {
					t.Error("identity header missing")
				}
				received[r.URL.Path], _ = io.ReadAll(r.Body)
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()
			b := Bootstrap{Schema: 1, WorkDir: t.TempDir(), Gateway: srv.URL, ProjectID: project, AgentID: "a1", HostName: "host", Headers: map[string]string{"X-DEVOPS-AGENT-ID": "a1"}}
			cfg, err := b.Config(false)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Tags["agentSecret"] != "" || cfg.Tags["host"] != "" {
				t.Fatal("legacy unwanted tags restored")
			}
			fields := map[string]any{"pct_used": 42.5, "total": uint64(128)}
			m := metric.New("mem", cfg.Tags, fields, time.Unix(1234567890, 123))
			for _, o := range cfg.Outputs {
				if err := o.Output.Connect(); err != nil {
					t.Fatal(err)
				}
				if err := o.Output.Write([]telegraf.Metric{m}); err != nil {
					t.Fatal(err)
				}
				_ = o.Output.Close()
			}
			var envelope struct {
				Metrics []struct {
					Name      string
					Timestamp int64
					Fields    map[string]any
					Tags      map[string]string
				}
			}
			if err := json.Unmarshal(received[metricsPath], &envelope); err != nil {
				t.Fatal(err)
			}
			if len(envelope.Metrics) != 1 || envelope.Metrics[0].Name != "mem" || envelope.Metrics[0].Timestamp != 1234567890 || envelope.Metrics[0].Fields["pct_used"] != 42.5 {
				t.Fatalf("wire mismatch: %+v", envelope)
			}
			if project == "git_stream" && len(received[metrixPath]) == 0 {
				t.Fatal("Stream main output missing")
			}
		})
	}
}
func TestSnapshotFilter(t *testing.T) {
	f := models.Filter{MetricPass: snapshotFilter}
	if err := f.Compile(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		tags map[string]string
		want bool
	}{
		{"mem", nil, true}, {"disk", nil, true}, {"cpu_detail", map[string]string{"cpu": "cpu-total"}, true},
		{"win_cpu", map[string]string{"instance": "_Total"}, true}, {"cpu_detail", map[string]string{"cpu": "cpu0"}, false},
		{"env", nil, false}, {"cpu_detail", nil, false},
	} {
		got, err := f.Select(metric.New(tc.name, tc.tags, map[string]any{"value": 1}, time.Now()))
		if err != nil || got != tc.want {
			t.Errorf("%s %v: %v %v", tc.name, tc.tags, got, err)
		}
	}
}
func TestExtensionConfigBounds(t *testing.T) {
	for _, text := range []string{"[agent]\ninterval=\"1ms\"\n", "[[inputs.bkci_external]]\nurl=\"http://127.0.0.1/metrics\"\ninterval=\"1ms\"\n", "[[inputs.bkci_external]]\nurl=\"http://127.0.0.1/metrics\"\nname_override=\"other\"\n"} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "collector.conf"), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := (Bootstrap{Schema: 1, WorkDir: dir}).Config(true); err == nil {
			t.Fatal("invalid extension config accepted")
		}
	}
}
