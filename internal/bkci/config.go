// Package bkci assembles the standalone collector using native Telegraf pipes.
package bkci

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/influxdata/telegraf/config"
	compat "github.com/influxdata/telegraf/internal/bkci/compat"
	_ "github.com/influxdata/telegraf/internal/bkci/external"
	_ "github.com/influxdata/telegraf/plugins/outputs/http"
	_ "github.com/influxdata/telegraf/plugins/parsers/influx"
	_ "github.com/influxdata/telegraf/plugins/parsers/json"
	_ "github.com/influxdata/telegraf/plugins/parsers/prometheus"
	_ "github.com/influxdata/telegraf/plugins/serializers/influx"
	_ "github.com/influxdata/telegraf/plugins/serializers/json"
)

const metricsPath = "/ms/environment/api/buildAgent/agent/thirdPartyAgent/agents/metrics"
const metrixPath = "/ms/environment/api/buildAgent/agent/thirdPartyAgent/agents/metrix"
const snapshotFilter = `name == 'mem' || name == 'disk' || ((name == 'cpu_detail' || name == 'cpu' || name == 'win_cpu') && (('cpu' in tags && tags['cpu'] == 'cpu-total') || ('instance' in tags && tags['instance'] in ['_Total', 'cpu-total'])))`

type Bootstrap struct {
	Schema         int               `json:"schema"`
	WorkDir        string            `json:"work_dir"`
	Gateway        string            `json:"gateway"`
	ProjectID      string            `json:"project_id"`
	AgentID        string            `json:"agent_id"`
	HostName       string            `json:"host_name"`
	HostIP         string            `json:"host_ip"`
	CPUProductInfo string            `json:"cpu_product_info"`
	GPUProductInfo string            `json:"gpu_product_info"`
	Headers        map[string]string `json:"headers"`
	ConfigFile     string            `json:"config_file"`
	CAFile         string            `json:"ca_file"`
}

func quote(s string) string { b, _ := json.Marshal(s); return string(b) }
func (b Bootstrap) Config(diagnostic bool) (*config.Config, error) {
	if b.Schema != 1 {
		return nil, fmt.Errorf("unsupported bootstrap schema %d", b.Schema)
	}
	cfg := config.NewConfig()
	cfg.Agent.OmitHostname = true
	var text strings.Builder
	text.WriteString(`[agent]
  interval = "1m"
  round_interval = false
  flush_interval = "1m"
  flush_jitter = "5s"
  collection_jitter = "1s"
  precision = "1ns"
  metric_batch_size = 256
  metric_buffer_limit = 1024
  omit_hostname = true
  snmp_translator = "gosmi"
  skip_processors_after_aggregators = true
[global_tags]
`)
	for k, v := range map[string]string{"projectId": b.ProjectID, "agentId": b.AgentID, "hostName": b.HostName, "hostIp": b.HostIP, "cpuProductInfo": b.CPUProductInfo, "gpuProductInfo": b.GPUProductInfo} {
		if v != "" {
			fmt.Fprintf(&text, "  %s = %s\n", quote(k), quote(v))
		}
	}
	for _, source := range compat.Sources() {
		fmt.Fprintf(&text, "\n[[inputs.bkci_compat]]\n  alias = %s\n  source = %s\n", quote(source), quote(source))
	}
	if len(compat.Sources()) > 0 && !diagnostic {
		fmt.Fprintf(&text, "\n[[processors.bkci_debug]]\n  work_dir = %s\n", quote(b.WorkDir))
	}
	if !diagnostic {
		gw := strings.TrimRight(strings.TrimSpace(b.Gateway), "/")
		if gw == "" {
			return nil, fmt.Errorf("gateway is required")
		}
		if !strings.Contains(gw, "://") {
			gw = "http://" + gw
		}
		if strings.HasPrefix(b.ProjectID, "git_") {
			b.output(&text, gw+metrixPath, "influx", 30*time.Second, "")
			b.output(&text, gw+metricsPath, "json", 5*time.Second, snapshotFilter)
		} else {
			b.output(&text, gw+metricsPath, "json", 30*time.Second, "")
		}
	}
	if err := cfg.LoadConfigData([]byte(text.String()), "bkci-defaults"); err != nil {
		return nil, err
	}
	filename := b.ConfigFile
	if filename == "" {
		filename = filepath.Join(b.WorkDir, "collector.conf")
	}
	if !filepath.IsAbs(filename) {
		filename = filepath.Join(b.WorkDir, filename)
	}
	data, err := readConfigFile(filename)
	if err != nil && (!os.IsNotExist(err) || b.ConfigFile != "") {
		return nil, err
	}
	if err == nil {
		if len(data) > 1<<20 {
			return nil, fmt.Errorf("collector.conf exceeds 1 MiB")
		}
		var user map[string]any
		if _, err := toml.Decode(string(data), &user); err != nil {
			return nil, err
		}
		for key := range user {
			if key != "inputs" {
				return nil, fmt.Errorf("extension config accepts only inputs, found %q", key)
			}
		}
		before := len(cfg.Inputs)
		if err := cfg.LoadConfigData(data, filename); err != nil {
			return nil, err
		}
		if len(cfg.Inputs)-before > 16 {
			return nil, fmt.Errorf("at most 16 extension inputs are supported")
		}
		for _, i := range cfg.Inputs[before:] {
			if i.Config.Name != "bkci_external" {
				return nil, fmt.Errorf("extension input %q is not enabled", i.Config.Name)
			}
			if i.Config.Interval != 0 && i.Config.Interval < time.Second*10 {
				return nil, fmt.Errorf("extension interval must be at least 10s")
			}
			if (i.Config.NameOverride != "" && i.Config.NameOverride != "env") || i.Config.MeasurementPrefix != "" || i.Config.MeasurementSuffix != "" {
				return nil, fmt.Errorf("extension name_override must be env")
			}
		}
	}
	if err := cfg.LoadAll(); err != nil {
		return nil, err
	}
	return cfg, nil
}
func (b Bootstrap) output(w *strings.Builder, address, format string, timeout time.Duration, filter string) {
	fmt.Fprintf(w, "\n[[outputs.http]]\n  url = %s\n  method = \"POST\"\n  timeout = %s\n  use_system_proxy = true\n  use_batch_format = true\n  data_format = %s\n", quote(address), quote(timeout.String()), quote(format))
	if format == "json" {
		w.WriteString("  json_timestamp_units = \"1s\"\n")
	}
	if format == "influx" {
		w.WriteString("  influx_uint_support = false\n")
	}
	if filter != "" {
		fmt.Fprintf(w, "  metricpass = %s\n", quote(filter))
	}
	if b.CAFile != "" {
		fmt.Fprintf(w, "  tls_ca = %s\n", quote(b.CAFile))
	}
	w.WriteString("  [outputs.http.headers]\n")
	for k, v := range b.Headers {
		if !strings.EqualFold(k, "Content-Type") {
			fmt.Fprintf(w, "    %s = %s\n", quote(k), quote(v))
		}
	}
	contentType := "text/plain; charset=utf-8"
	if format == "json" {
		contentType = "application/json; charset=utf-8"
	}
	fmt.Fprintf(w, "    \"Content-Type\" = %s\n", quote(contentType))
}
