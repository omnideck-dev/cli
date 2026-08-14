package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type podmanServiceClient struct {
	client  *http.Client
	baseURL string
}

func (c *podmanServiceClient) get(path string, target any) error {
	request, err := http.NewRequestWithContext(processCtx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("creating Podman service request: %w", err)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("contacting Podman service: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, maxCommandOutput))
		detailText := strings.TrimSpace(string(detail))
		if detailText == "" {
			detailText = response.Status
		}
		return fmt.Errorf("Podman service returned %s: %s", response.Status, detailText)
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return fmt.Errorf("decoding Podman service response: %w", err)
	}
	return nil
}

type podmanServiceInspect struct {
	Created      string `json:"Created"`
	RestartCount int    `json:"RestartCount"`
	State        struct {
		Status    string `json:"Status"`
		StartedAt string `json:"StartedAt"`
		Health    *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
}

func (c *podmanServiceClient) containerInspect(name string) (InspectData, error) {
	var response podmanServiceInspect
	path := "/containers/" + url.PathEscape(name) + "/json"
	if err := c.get(path, &response); err != nil {
		return InspectData{}, fmt.Errorf("podman inspect: %w", err)
	}
	inspect := InspectData{
		Status:       response.State.Status,
		RestartCount: response.RestartCount,
	}
	inspect.CreatedAt, _ = time.Parse(time.RFC3339Nano, response.Created)
	inspect.StartedAt, _ = time.Parse(time.RFC3339Nano, response.State.StartedAt)
	if response.State.Health != nil && response.State.Health.Status != "none" {
		inspect.HealthStatus = response.State.Health.Status
	}
	return inspect, nil
}

type podmanServiceStatsReport struct {
	Stats []struct {
		CPU      float64 `json:"CPU"`
		MemUsage uint64  `json:"MemUsage"`
		MemLimit uint64  `json:"MemLimit"`
		MemPerc  float64 `json:"MemPerc"`
	} `json:"Stats"`
}

func (c *podmanServiceClient) containerStats(name string) (cpu string, cpuPct float64, ram, ramTotal string, ramPct float64, err error) {
	query := url.Values{
		"containers": {name},
		"stream":     {"false"},
	}
	var response podmanServiceStatsReport
	if err := c.get("/v1.0.0/libpod/containers/stats?"+query.Encode(), &response); err != nil {
		return "", 0, "", "", 0, fmt.Errorf("podman stats: %w", err)
	}
	if len(response.Stats) == 0 {
		return "", 0, "", "", 0, fmt.Errorf("podman stats: Podman service returned no container statistics")
	}
	stats := response.Stats[0]
	return formatServicePercent(stats.CPU), stats.CPU / 100,
		formatServiceBytes(stats.MemUsage), formatServiceBytes(stats.MemLimit), stats.MemPerc / 100, nil
}

func formatServicePercent(value float64) string {
	return fmt.Sprintf("%.2f%%", value)
}

func formatServiceBytes(value uint64) string {
	const unit = uint64(1024)
	if value < unit {
		return fmt.Sprintf("%dB", value)
	}
	units := [...]string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	amount := float64(value)
	for _, suffix := range units {
		amount /= float64(unit)
		if amount < float64(unit) || suffix == units[len(units)-1] {
			formatted := fmt.Sprintf("%.1f", amount)
			formatted = strings.TrimSuffix(formatted, ".0")
			return formatted + suffix
		}
	}
	return fmt.Sprintf("%dB", value)
}
