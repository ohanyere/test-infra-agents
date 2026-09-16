package incidentsource

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

const providerTimeout = 10 * time.Second

func httpGetJSON(ctx context.Context, client *http.Client, req *http.Request, out any) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || strings.Contains(strings.ToLower(err.Error()), "timeout") {
			return 0, &ProviderError{Code: CodeProviderTimeout, Status: StatusTimeout}
		}
		return 0, &ProviderError{Code: CodeProviderUnreachable, Status: StatusUnreachable}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return resp.StatusCode, &ProviderError{Code: CodeProviderMalformedResponse, Status: StatusProviderError}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, nil
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(out); err != nil {
		return resp.StatusCode, &ProviderError{Code: CodeProviderMalformedResponse, Status: StatusProviderError}
	}
	return resp.StatusCode, nil
}

func defaultHTTPClient() *http.Client {
	return &http.Client{Timeout: providerTimeout + 2*time.Second}
}
