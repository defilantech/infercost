package scraper

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// MetricSample is a single scraped metric value with its labels.
type MetricSample struct {
	Name   string
	Labels map[string]string
	Value  float64
}

// Client scrapes Prometheus-format /metrics endpoints.
type Client struct {
	httpClient *http.Client
}

// NewClient creates a scraper client with the given timeout.
func NewClient(timeout time.Duration) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
			},
		},
	}
}

// Scrape fetches and parses all metrics from a Prometheus /metrics endpoint.
func (c *Client) Scrape(ctx context.Context, url string) ([]MetricSample, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("scraping %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scraping %s: status %d", url, resp.StatusCode)
	}

	return parseMetrics(resp.Body)
}

func parseMetrics(r io.Reader) ([]MetricSample, error) {
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(r)
	if err != nil {
		return nil, fmt.Errorf("parsing metrics: %w", err)
	}

	var samples []MetricSample
	for name, family := range families {
		for _, m := range family.GetMetric() {
			labels := make(map[string]string)
			for _, lp := range m.GetLabel() {
				labels[lp.GetName()] = lp.GetValue()
			}

			var value float64
			switch family.GetType() {
			case dto.MetricType_GAUGE:
				value = m.GetGauge().GetValue()
			case dto.MetricType_COUNTER:
				value = m.GetCounter().GetValue()
			case dto.MetricType_UNTYPED:
				value = m.GetUntyped().GetValue()
			default:
				continue
			}

			samples = append(samples, MetricSample{
				Name:   name,
				Labels: labels,
				Value:  value,
			})
		}
	}
	return samples, nil
}

// FilterByName returns only samples matching the given metric name.
func FilterByName(samples []MetricSample, name string) []MetricSample {
	var filtered []MetricSample
	for _, s := range samples {
		if s.Name == name {
			filtered = append(filtered, s)
		}
	}
	return filtered
}
