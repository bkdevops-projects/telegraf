// Package external is a bounded URL/script input using native Telegraf parsers.
package external

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/inputs"
)

const maxBody = 1 << 20

var slots = make(chan struct{}, 4)

type Input struct {
	URL         string            `toml:"url"`
	Command     []string          `toml:"command"`
	Environment []string          `toml:"environment"`
	Headers     map[string]string `toml:"headers"`
	Timeout     config.Duration   `toml:"timeout"`
	// measurement_field preserves Prometheus v1 metric names when all series
	// are placed under env. Other parsers preserve their existing field names.
	Mapping string `toml:"mapping"`
	Prefix  string `toml:"field_prefix"`
	parser  telegraf.Parser
	client  *http.Client
}

func (*Input) SampleConfig() string {
	return "  url = \"http://127.0.0.1:9100/metrics\"\n  data_format = \"prometheus\"\n  mapping = \"measurement_field\"\n"
}
func (i *Input) SetParser(p telegraf.Parser) { i.parser = p }
func (i *Input) Init() error {
	if (i.URL == "") == (len(i.Command) == 0) {
		return errors.New("specify exactly one URL or command argument array")
	}
	if runtime.GOOS == "darwin" && len(i.Command) > 0 {
		return errors.New("script inputs require Windows Job Object or Linux cgroup v2; disabled on macOS")
	}
	if i.Timeout == 0 {
		i.Timeout = config.Duration(3 * time.Second)
	}
	if time.Duration(i.Timeout) <= 0 || time.Duration(i.Timeout) > 30*time.Second {
		return errors.New("timeout must be within (0,30s]")
	}
	if i.Mapping != "" && i.Mapping != "fields" && i.Mapping != "measurement_field" {
		return errors.New("mapping must be fields or measurement_field")
	}
	if i.parser == nil {
		return errors.New("data_format parser required")
	}
	i.client = &http.Client{Timeout: time.Duration(i.Timeout), Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, MaxIdleConns: 1, MaxIdleConnsPerHost: 1, IdleConnTimeout: time.Minute}}
	return nil
}
func (i *Input) Gather(acc telegraf.Accumulator) error {
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	default:
		return errors.New("third-party concurrency limit reached; sample skipped")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(i.Timeout))
	defer cancel()
	var data []byte
	var err error
	if i.URL != "" {
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, i.URL, nil)
		if e != nil {
			return e
		}
		for k, v := range i.Headers {
			req.Header.Set(k, v)
		}
		resp, e := i.client.Do(req)
		if e != nil {
			return e
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("input HTTP status %d", resp.StatusCode)
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
		if len(data) > maxBody {
			return errors.New("input response exceeds 1 MiB")
		}
	} else {
		cmd := exec.CommandContext(ctx, i.Command[0], i.Command[1:]...)
		data, err = runCommand(cmd, i.Environment)
	}
	if err != nil {
		return err
	}
	metrics, err := i.parser.Parse(data)
	if err != nil {
		return err
	}
	if len(metrics) > 1024 {
		return errors.New("input exceeds 1024 metrics per sample")
	}
	for _, m := range metrics {
		fields := make(map[string]any, len(m.FieldList()))
		for _, f := range m.FieldList() {
			name := f.Key
			if i.Mapping == "measurement_field" {
				name = m.Name()
				if f.Key != "gauge" && f.Key != "counter" && f.Key != "value" && f.Key != "untyped" {
					name += "_" + f.Key
				}
			}
			name = i.Prefix + name
			if len(name) > 256 {
				return errors.New("metric field name exceeds 256 bytes")
			}
			if text, ok := f.Value.(string); ok && len(text) > 4096 {
				return errors.New("metric string field too large")
			}
			fields[name] = f.Value
		}
		if len(fields) > 64 || len(m.TagList()) > 32 {
			return errors.New("metric exceeds field/tag limit")
		}
		tags := m.Tags()
		for k, v := range tags {
			if len(k) > 256 || len(v) > 1024 {
				return errors.New("metric tag too large")
			}
		}
		acc.AddFields("env", fields, tags, m.Time())
	}
	return nil
}

// cappedBuffer returns an error immediately on overflow. exec's copy goroutine
// then finishes; CommandContext/WaitDelay bound a child that ignores a broken pipe.
type cappedBuffer struct {
	data     []byte
	overflow bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > maxBody {
		b.overflow = true
		return 0, errors.New("script output exceeds 1 MiB")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
func commandEnv(base, extra []string) []string {
	for _, entry := range extra {
		k, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		filtered := base[:0]
		for _, e := range base {
			key, _, _ := strings.Cut(e, "=")
			if !strings.EqualFold(k, key) {
				filtered = append(filtered, e)
			}
		}
		base = append(filtered, entry)
	}
	return base
}
func init() { inputs.Add("bkci_external", func() telegraf.Input { return &Input{} }) }
