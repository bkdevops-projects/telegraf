package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/influxdata/telegraf/agent"
	"github.com/influxdata/telegraf/internal/bkci"
	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

var version = "1.40.3-bkci.1"

func main() {
	if err := run(); err != nil {
		log.Printf("E! collector: %v", err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	showVersion := flag.Bool("version", false, "print collector version")
	diagnostic := flag.Bool("test", false, "print metrics, do not report")
	duration := flag.Duration("d", time.Second, "CPU diagnostic warmup")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return nil
	}
	if *duration < 0 || *duration > 30*time.Second {
		return errors.New("diagnostic warmup must be within 0..30s")
	}
	runtime.GOMAXPROCS(1)
	debug.SetMemoryLimit(128 << 20)
	debug.SetGCPercent(50)
	reader := bufio.NewReaderSize(os.Stdin, 64<<10)
	line, err := reader.ReadSlice('\n')
	if err != nil {
		return fmt.Errorf("read bootstrap: %w", err)
	}
	var b bkci.Bootstrap
	if err := json.Unmarshal(line, &b); err != nil {
		return err
	}
	if b.WorkDir == "" {
		return errors.New("work_dir is required")
	}
	logDir := filepath.Join(b.WorkDir, "logs")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return err
	}
	logName := "collector.log"
	if *diagnostic {
		logName = "collector-diagnostic.log"
	}
	rolling := &lumberjack.Logger{Filename: filepath.Join(logDir, logName), MaxSize: 10, MaxBackups: 3, MaxAge: 7}
	defer rolling.Close()
	log.SetOutput(rolling)
	logrus.SetOutput(rolling)
	logrus.SetLevel(logrus.WarnLevel)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go watchMemory(ctx)
	finished := make(chan struct{})
	defer close(finished)
	go func() { _, _ = io.Copy(io.Discard, reader); cancel() }()
	// Any plugin blocked in an OS call cannot hold the subprocess forever after
	// Agent exit. The parent independently applies a 10-second shutdown deadline.
	go func() {
		<-ctx.Done()
		select {
		case <-finished:
			return
		case <-time.After(8 * time.Second):
			terminateTree()
			os.Exit(1)
		}
	}()
	cfg, err := b.Config(*diagnostic)
	if err != nil {
		return err
	}
	a := agent.NewAgent(cfg)
	if *diagnostic {
		warmup(cfg, *duration)
		return a.Test(ctx, 0)
	}
	a.OnReady = func() { fmt.Fprintln(os.Stdout, "{\"ready\":true}") }
	err = a.Run(ctx)
	if ctx.Err() != nil {
		terminateTree()
	}
	return err
}
