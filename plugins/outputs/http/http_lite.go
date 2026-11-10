//go:build bkci_lite

// BK-CI reduced build: the native v1.40.3 HTTP output, without optional AWS/GCP
// authentication dependencies. Keep request, serializer and retry semantics in
// sync with http.go. Successful response bodies are drained with a 64 KiB cap.
//go:generate ../../../tools/config_includer/generator
//go:generate ../../../tools/readme_config_includer/generator
package http

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	common_http "github.com/influxdata/telegraf/plugins/common/http"
	"github.com/influxdata/telegraf/plugins/outputs"
)

//go:embed sample.conf
var sampleConfig string

const (
	maxErrMsgLen = 1024
	defaultURL   = "http://127.0.0.1:8080/telegraf"
)

const (
	defaultContentType    = "text/plain; charset=utf-8"
	defaultMethod         = http.MethodPost
	defaultUseBatchFormat = true
)

type HTTP struct {
	URL                     string                    `toml:"url"`
	Method                  string                    `toml:"method"`
	Username                config.Secret             `toml:"username"`
	Password                config.Secret             `toml:"password"`
	Headers                 map[string]*config.Secret `toml:"headers"`
	ContentEncoding         string                    `toml:"content_encoding"`
	UseBatchFormat          bool                      `toml:"use_batch_format"`
	NonRetryableStatusCodes []int                     `toml:"non_retryable_statuscodes"`
	common_http.HTTPClientConfig
	Log telegraf.Logger `toml:"-"`

	client     *http.Client
	serializer telegraf.Serializer
}

func (*HTTP) SampleConfig() string {
	return sampleConfig
}

func (h *HTTP) SetSerializer(serializer telegraf.Serializer) {
	h.serializer = serializer
}

func (h *HTTP) Connect() error {
	if h.Method == "" {
		h.Method = http.MethodPost
	}
	h.Method = strings.ToUpper(h.Method)
	if h.Method != http.MethodPost && h.Method != http.MethodPut && h.Method != http.MethodPatch {
		return fmt.Errorf("invalid method [%s] %s", h.URL, h.Method)
	}

	ctx := context.Background()
	client, err := h.HTTPClientConfig.CreateClient(ctx, h.Log)
	if err != nil {
		return err
	}

	h.client = client

	return nil
}

func (h *HTTP) Close() error {
	if h.client != nil {
		h.client.CloseIdleConnections()
	}

	return nil
}

func (h *HTTP) Write(metrics []telegraf.Metric) error {
	if h.UseBatchFormat {
		reqBody, err := h.serializer.SerializeBatch(metrics)
		if err != nil {
			return err
		}

		return h.writeMetric(reqBody)
	}

	for _, metric := range metrics {
		reqBody, err := h.serializer.Serialize(metric)
		if err != nil {
			return err
		}

		if err := h.writeMetric(reqBody); err != nil {
			return err
		}
	}
	return nil
}

func (h *HTTP) writeMetric(reqBody []byte) error {
	var reqBodyBuffer io.Reader = bytes.NewBuffer(reqBody)

	var err error
	if h.ContentEncoding == "gzip" {
		rc := internal.CompressWithGzip(reqBodyBuffer)
		defer rc.Close()
		reqBodyBuffer = rc
	}

	req, err := http.NewRequest(h.Method, h.URL, reqBodyBuffer)
	if err != nil {
		return err
	}

	if !h.Username.Empty() || !h.Password.Empty() {
		username, err := h.Username.Get()
		if err != nil {
			return fmt.Errorf("getting username failed: %w", err)
		}
		password, err := h.Password.Get()
		if err != nil {
			username.Destroy()
			return fmt.Errorf("getting password failed: %w", err)
		}
		req.SetBasicAuth(username.String(), password.String())
		username.Destroy()
		password.Destroy()
	}

	req.Header.Set("User-Agent", internal.ProductToken())
	req.Header.Set("Content-Type", defaultContentType)
	if h.ContentEncoding == "gzip" {
		req.Header.Set("Content-Encoding", "gzip")
	}

	for k, v := range h.Headers {
		secret, err := v.Get()
		if err != nil {
			return err
		}

		headerVal := secret.String()
		if strings.EqualFold(k, "host") {
			req.Host = headerVal
		}
		req.Header.Set(k, headerVal)

		secret.Destroy()
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errorLine := ""
		scanner := bufio.NewScanner(io.LimitReader(resp.Body, maxErrMsgLen))
		if scanner.Scan() {
			errorLine = scanner.Text()
		}

		if slices.Contains(h.NonRetryableStatusCodes, resp.StatusCode) {
			h.Log.Errorf("Received non-retryable status %v. Metrics are lost. body: %s", resp.StatusCode, errorLine)
			return nil
		}

		return fmt.Errorf("when writing to [%s] received status code: %d. body: %s", h.URL, resp.StatusCode, errorLine)
	}

	_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return fmt.Errorf("when writing to [%s] received error: %w", h.URL, err)
	}

	return nil
}

func init() {
	outputs.Add("http", func() telegraf.Output {
		return &HTTP{
			Method:         defaultMethod,
			URL:            defaultURL,
			UseBatchFormat: defaultUseBatchFormat,
		}
	})
}
