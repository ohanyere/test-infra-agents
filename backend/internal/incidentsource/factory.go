package incidentsource

import (
	"encoding/json"
	"strings"
)

type defaultFactory struct{}

func NewFactory() Factory { return defaultFactory{} }

func (defaultFactory) Connector(provider string, config json.RawMessage, credentials Credentials) (Connector, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case ProviderPagerDuty:
		if strings.TrimSpace(credentials.APIToken) == "" {
			return nil, validation("PagerDuty requires only api_token")
		}
		return NewPagerDutyConnector(credentials.APIToken)
	case ProviderDatadog:
		var parsed struct {
			Site string `json:"site"`
		}
		_ = json.Unmarshal(config, &parsed)
		if strings.TrimSpace(parsed.Site) == "" {
			return nil, validation("site is required for Datadog")
		}
		return NewDatadogConnector(parsed.Site, credentials.APIKey, credentials.ApplicationKey)
	default:
		return nil, validation("provider must be pagerduty or datadog")
	}
}
