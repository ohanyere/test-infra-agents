package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	RateWindow = 30 * time.Second
	maxBody    = int64(1 << 20)
)

var ErrUnavailable = errors.New("telemetry unavailable")

type QueryClient struct {
	baseURL string
	client  *http.Client
	timeout time.Duration
}

func NewQueryClient(baseURL string, client *http.Client, timeout time.Duration) (*QueryClient, error) {
	u, e := url.Parse(baseURL)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("invalid Prometheus base URL")
	}
	if client == nil {
		client = &http.Client{}
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &QueryClient{baseURL: u.String(), client: client, timeout: timeout}, nil
}

var queries = map[string]string{
	"request":      fmt.Sprintf(`sum(rate(http_requests_total{route=~"root|work"}[%s]))`, RateWindow),
	"error":        fmt.Sprintf(`100 * (sum(rate(http_requests_total{route=~"root|work",status_class="5xx"}[%s])) or vector(0)) / sum(rate(http_requests_total{route=~"root|work"}[%s]))`, RateWindow, RateWindow),
	"latency":      fmt.Sprintf(`1000 * histogram_quantile(0.95, sum by (le) (rate(http_request_duration_seconds_bucket{route=~"root|work"}[%s])))`, RateWindow),
	"availability": fmt.Sprintf(`100 * (sum(rate(http_requests_total{route=~"root|work",status_class!="5xx"}[%s])) or vector(0)) / sum(rate(http_requests_total{route=~"root|work"}[%s]))`, RateWindow, RateWindow),
}

func (c *QueryClient) Snapshot(ctx context.Context, _ EnvironmentContext) (Snapshot, error) {
	out := Snapshot{Timestamp: time.Now().UTC()}
	var e error
	if out.RequestRateRPS, e = c.query(ctx, queries["request"]); e != nil {
		return out, e
	}
	if out.ErrorRatePct, e = c.query(ctx, queries["error"]); e != nil {
		return out, e
	}
	if out.P95LatencyMS, e = c.query(ctx, queries["latency"]); e != nil {
		return out, e
	}
	if out.AvailabilityPct, e = c.query(ctx, queries["availability"]); e != nil {
		return out, e
	}
	return out, nil
}
func (c *QueryClient) query(ctx context.Context, q string) (*float64, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	u, e := url.Parse(c.baseURL + "/api/v1/query")
	if e != nil {
		return nil, ErrUnavailable
	}
	values := u.Query()
	values.Set("query", q)
	u.RawQuery = values.Encode()
	req, e := http.NewRequestWithContext(callCtx, http.MethodGet, u.String(), nil)
	if e != nil {
		return nil, ErrUnavailable
	}
	resp, e := c.client.Do(req)
	if e != nil {
		return nil, fmt.Errorf("%w: query request failed", ErrUnavailable)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: Prometheus HTTP status %d", ErrUnavailable, resp.StatusCode)
	}
	body, e := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if e != nil || int64(len(body)) > maxBody {
		return nil, fmt.Errorf("%w: invalid response body", ErrUnavailable)
	}
	var envelope struct {
		Status string `json:"status"`
		Error  string `json:"error"`
		Data   struct {
			ResultType string          `json:"resultType"`
			Result     json.RawMessage `json:"result"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&envelope); e != nil {
		return nil, fmt.Errorf("%w: malformed response", ErrUnavailable)
	}
	if envelope.Status != "success" {
		return nil, fmt.Errorf("%w: Prometheus query failed", ErrUnavailable)
	}
	raw := ""
	switch envelope.Data.ResultType {
	case "vector":
		var v []struct {
			Value []json.RawMessage `json:"value"`
		}
		if e = json.Unmarshal(envelope.Data.Result, &v); e != nil {
			return nil, ErrUnavailable
		}
		if len(v) == 0 {
			return nil, nil
		}
		if len(v) != 1 || len(v[0].Value) != 2 {
			return nil, ErrUnavailable
		}
		e = json.Unmarshal(v[0].Value[1], &raw)
	case "scalar":
		var v []json.RawMessage
		if e = json.Unmarshal(envelope.Data.Result, &v); e != nil || len(v) != 2 {
			return nil, ErrUnavailable
		}
		e = json.Unmarshal(v[1], &raw)
	default:
		return nil, ErrUnavailable
	}
	if e != nil {
		return nil, ErrUnavailable
	}
	number, e := strconv.ParseFloat(raw, 64)
	if e != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return nil, nil
	}
	return &number, nil
}
