//go:build !loong64

package monitor

import (
	"context"
	"fmt"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

// Adapter leaves the validated collection algorithms unchanged. Telegraf owns
// scheduling, non-reentrancy, buffering, serialization and delivery.
type Adapter struct {
	Source  string        `toml:"source"`
	Timeout time.Duration `toml:"-"`
	Warmup  time.Duration `toml:"-"`
	input   Input
}

func (*Adapter) SampleConfig() string { return "  source = \"cpu_detail\"\n" }

func (a *Adapter) Init() error {
	if a.input != nil {
		return nil
	}
	for _, input := range newDefaultInputs() {
		name := input.Name()
		if _, ok := input.(*Net); ok {
			name = "net"
		}
		if name == a.Source {
			a.input = input
			break
		}
	}
	// Linux has a second input with Name()=="net".
	if a.Source == "netproto" && platformExtraInputsFn != nil {
		for _, input := range platformExtraInputsFn() {
			if input.Name() == "net" {
				a.input = input
				break
			}
		}
	}
	if a.input == nil {
		return fmt.Errorf("unsupported compatibility source %q", a.Source)
	}
	return nil
}

func (a *Adapter) Gather(acc telegraf.Accumulator) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("compatibility input %s panic: %v", a.Source, r)
		}
	}()
	timeout := a.Timeout
	if timeout == 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout+a.Warmup)
	defer cancel()
	if a.Warmup > 0 {
		if _, err := a.input.Gather(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(a.Warmup):
		}
		a.Warmup = 0
	}
	metrics, err := a.input.Gather(ctx)
	if err != nil {
		return err
	}
	// LHM and third-party env metrics must never pass through system rename rules.
	if a.Source != "hwtemp" {
		metrics = Rename(metrics)
	}
	for _, m := range metrics {
		acc.AddFields(m.Name, m.Fields, m.Tags, m.Timestamp)
	}
	return nil
}

func Sources() []string {
	names := make([]string, 0, 12)
	for _, input := range newDefaultInputs() {
		name := input.Name()
		if name == "net" {
			found := false
			for _, n := range names {
				if n == "net" {
					found = true
				}
			}
			if found {
				name = "netproto"
			}
		}
		names = append(names, name)
	}
	return names
}

func init() { inputs.Add("bkci_compat", func() telegraf.Input { return &Adapter{} }) }
