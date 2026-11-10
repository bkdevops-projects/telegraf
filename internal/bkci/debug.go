//go:build !loong64

package bkci

import (
	"github.com/influxdata/telegraf"
	compat "github.com/influxdata/telegraf/internal/bkci/compat"
	"github.com/influxdata/telegraf/plugins/processors"
)

type debugDump struct {
	WorkDir string `toml:"work_dir"`
	dumper  *compat.Dumper
}

func (*debugDump) SampleConfig() string { return "  work_dir = \".\"\n" }
func (d *debugDump) Init() error        { d.dumper = compat.NewMonitorDumper(d.WorkDir); return nil }
func (d *debugDump) Apply(metrics ...telegraf.Metric) []telegraf.Metric {
	if d.dumper == nil {
		return metrics
	}
	converted := make([]compat.Metric, 0, len(metrics))
	for _, m := range metrics {
		converted = append(converted, compat.Metric{Name: m.Name(), Tags: m.Tags(), Fields: m.Fields(), Timestamp: m.Time()})
	}
	d.dumper.Dump(converted)
	return metrics
}
func init() { processors.Add("bkci_debug", func() telegraf.Processor { return &debugDump{} }) }
