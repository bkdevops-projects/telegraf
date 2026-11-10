//go:build loong64

package main

import (
	"github.com/influxdata/telegraf/config"
	"time"
)

func warmup(*config.Config, time.Duration) {}
