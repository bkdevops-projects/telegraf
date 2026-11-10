//go:build !loong64

package main

import (
	"github.com/influxdata/telegraf/config"
	compat "github.com/influxdata/telegraf/internal/bkci/compat"
	"time"
)

func warmup(cfg *config.Config, d time.Duration) {
	for _, i := range cfg.Inputs {
		if a, ok := i.Input.(*compat.Adapter); ok && a.Source == "cpu_detail" {
			a.Warmup = d
		}
	}
}
