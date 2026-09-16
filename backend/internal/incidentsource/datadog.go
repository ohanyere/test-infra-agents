package incidentsource

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var datadogSiteBases = map[string]string{
	"DATADOGHQ.COM":     "https://api.datadoghq.com",
	"US3.DATADOGHQ.COM": "https://api.us3.datadoghq.com",
	"US5.DATADOGHQ.COM": "https://api.us5.datadoghq.com",
	"DATADOGHQ.EU":      "https://api.datadoghq.eu",
	"AP1.DATADOGHQ.COM": "https://api.ap1.datadoghq.com",
	"AP2.DATADOGHQ.COM": "https://api.ap2.datadoghq.com",
}

func SupportedDatadogSites() []string {
	return []string{"DATADOGHQ.COM", "US3.DATADOGHQ.COM", "US5.DATADOGHQ.COM", "DATADOGHQ.EU", "AP1.DATADOGHQ.COM", "AP2.DATADOGHQ.COM"}
}

func datadogBaseForSite(site string) (string, error) {
	normalized := strings.ToUpper(strings.TrimSpace(site))
	base, ok := datadogSiteBases[normalized]
	if !ok {
		return "", &ProviderError{Code: CodeDatadogInvalidSite, Status: StatusConfigurationError}
	}
	return base, nil
}

type datadogConnector struct {
	apiKey         string
	applicationKey string
	baseURL        string
	site           string
	httpClient     *http.Client
}

func NewDatadogConnector(site, apiKey, applicationKey string) (*datadogConnector, error) {
	site = strings.ToUpper(strings.TrimSpace(site))
	base, err := datadogBaseForSite(site)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(apiKey) == "" || strings.TrimSpace(applicationKey) == "" {
		return nil, validation("Datadog requires api_key and application_key")
	}
	return &datadogConnector{apiKey: apiKey, applicationKey: applicationKey, baseURL: base, site: site, httpClient: defaultHTTPClient()}, nil
}

func newDatadogConnectorForTest(site, apiKey, applicationKey, baseURL string, client *http.Client) (*datadogConnector, error) {
	if _, err := datadogBaseForSite(site); err != nil {
		return nil, err
	}
	if client == nil {
		client = defaultHTTPClient()
	}
	return &datadogConnector{apiKey: apiKey, applicationKey: applicationKey, baseURL: strings.TrimRight(baseURL, "/"), site: strings.ToUpper(strings.TrimSpace(site)), httpClient: client}, nil
}

type ddListResponse struct {
	Data []ddIncident `json:"data"`
	Meta *ddMeta      `json:"meta"`
}

type ddMeta struct {
	Page *ddPage `json:"page"`
}

type ddPage struct {
	TotalCount int `json:"total_count"`
}

type ddIncident struct {
	ID         string       `json:"id"`
	Type       string       `json:"type"`
	Attributes ddAttributes `json:"attributes"`
}

type ddAttributes struct {
	Title                 string         `json:"title"`
	CustomerImpactSummary string         `json:"customer_impact_summary"`
	State                 string         `json:"state"`
	Severity              string         `json:"severity"`
	Created               string         `json:"created"`
	Modified              string         `json:"modified"`
	Resolved              string         `json:"resolved"`
	Tags                  []string       `json:"tags"`
	Fields                map[string]any `json:"fields"`
	PublicID              int            `json:"public_id"`
	Detector              *ddDetector    `json:"detector"`
}

type ddDetector struct {
	Name string `json:"name"`
}

type ddGetResponse struct {
	Data     ddIncident   `json:"data"`
	Included []ddIncluded `json:"included"`
}

type ddIncluded struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Attributes map[string]any `json:"attributes"`
}

type ddUpdatesResponse struct {
	Data []ddUpdate `json:"data"`
}

type ddUpdate struct {
	ID         string             `json:"id"`
	Attributes ddUpdateAttributes `json:"attributes"`
}

type ddUpdateAttributes struct {
	Created    string `json:"created"`
	Modified   string `json:"modified"`
	Content    string `json:"content"`
	UpdateType string `json:"update_type"`
}

func (c *datadogConnector) newRequest(ctx context.Context, method, path string, query url.Values) (*http.Request, error) {
	raw := c.baseURL + path
	if len(query) > 0 {
		raw += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("DD-API-KEY", c.apiKey)
	req.Header.Set("DD-APPLICATION-KEY", c.applicationKey)
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *datadogConnector) TestConnection(ctx context.Context) error {
	query := url.Values{}
	query.Set("include_resolved", "false")
	query.Set("page[size]", "1")
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v2/incidents", query)
	if err != nil {
		return &ProviderError{Code: CodeProviderUnreachable, Status: StatusUnreachable}
	}
	var out ddListResponse
	status, err := httpGetJSON(ctx, c.httpClient, req, &out)
	if err != nil {
		return err
	}
	return mapDatadogStatus(status)
}

func (c *datadogConnector) ListIncidents(ctx context.Context, filter ListFilter) (IncidentPage, error) {
	filter = boundedFilter(filter)
	pageNumber := filter.Offset / filter.Limit
	query := url.Values{}
	query.Set("page[size]", fmt.Sprint(filter.Limit))
	query.Set("page[number]", fmt.Sprint(pageNumber))
	query.Set("include_resolved", "true")
	query.Set("sort", "-created")
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v2/incidents", query)
	if err != nil {
		return IncidentPage{}, &ProviderError{Code: CodeProviderUnreachable, Status: StatusUnreachable}
	}
	var out ddListResponse
	status, err := httpGetJSON(ctx, c.httpClient, req, &out)
	if err != nil {
		return IncidentPage{}, err
	}
	if statusErr := mapDatadogStatus(status); statusErr != nil {
		return IncidentPage{}, statusErr
	}
	items := make([]NormalizedIncident, 0, len(out.Data))
	for _, incident := range out.Data {
		mapped := mapDatadogIncident(incident, nil)
		if filter.Query != "" && !strings.Contains(strings.ToLower(mapped.Title+" "+mapped.Summary), strings.ToLower(filter.Query)) {
			continue
		}
		items = append(items, mapped)
	}
	page := IncidentPage{Items: items, Limit: filter.Limit, Offset: filter.Offset}
	total := -1
	if out.Meta != nil && out.Meta.Page != nil {
		total = out.Meta.Page.TotalCount
	}
	if total < 0 {
		if len(out.Data) == filter.Limit {
			next := filter.Offset + len(out.Data)
			page.NextOffset = &next
		}
	} else if filter.Offset+len(out.Data) < total {
		next := filter.Offset + len(out.Data)
		page.NextOffset = &next
	}
	return page, nil
}

func (c *datadogConnector) GetIncident(ctx context.Context, externalID string) (NormalizedIncident, error) {
	if err := validateExternalID(externalID); err != nil {
		return NormalizedIncident{}, err
	}
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v2/incidents/"+url.PathEscape(externalID), nil)
	if err != nil {
		return NormalizedIncident{}, &ProviderError{Code: CodeProviderUnreachable, Status: StatusUnreachable}
	}
	var out ddGetResponse
	status, err := httpGetJSON(ctx, c.httpClient, req, &out)
	if err != nil {
		return NormalizedIncident{}, err
	}
	if status == http.StatusNotFound {
		return NormalizedIncident{}, &ProviderError{Code: CodeProviderNotFound, Status: StatusProviderError}
	}
	if statusErr := mapDatadogStatus(status); statusErr != nil {
		return NormalizedIncident{}, statusErr
	}
	timeline := c.fetchUpdates(ctx, externalID)
	return mapDatadogIncident(out.Data, timeline), nil
}

func (c *datadogConnector) fetchUpdates(ctx context.Context, incidentID string) []TimelineEntry {
	query := url.Values{}
	query.Set("page[size]", "50")
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v2/incidents/"+url.PathEscape(incidentID)+"/updates", query)
	if err != nil {
		return nil
	}
	var out ddUpdatesResponse
	status, err := httpGetJSON(ctx, c.httpClient, req, &out)
	if err != nil || status < 200 || status >= 300 {
		return nil
	}
	entries := make([]TimelineEntry, 0, len(out.Data))
	for _, update := range out.Data {
		var timestamp *time.Time
		for _, raw := range []string{update.Attributes.Created, update.Attributes.Modified} {
			if parsed, parseErr := time.Parse(time.RFC3339, raw); parseErr == nil {
				timestamp = &parsed
				break
			}
		}
		content := update.Attributes.Content
		if len(content) > 2000 {
			content = content[:2000]
		}
		entries = append(entries, TimelineEntry{Timestamp: timestamp, Type: update.Attributes.UpdateType, Summary: content})
		if len(entries) >= 50 {
			break
		}
	}
	return entries
}

func mapDatadogIncident(incident ddIncident, timeline []TimelineEntry) NormalizedIncident {
	title := incident.Attributes.Title
	if len(title) > 500 {
		title = title[:500]
	}
	summary := incident.Attributes.CustomerImpactSummary
	if len(summary) > 8000 {
		summary = summary[:8000]
	}
	severity := incident.Attributes.Severity
	if severity == "" {
		if raw, ok := incident.Attributes.Fields["severity"]; ok {
			severity = fmt.Sprint(raw)
		} else if raw, ok := incident.Attributes.Fields["priority"]; ok {
			severity = fmt.Sprint(raw)
		}
	}
	services := []string{}
	if incident.Attributes.Detector != nil && incident.Attributes.Detector.Name != "" {
		services = append(services, incident.Attributes.Detector.Name)
	}
	tags := []string{}
	for _, tag := range incident.Attributes.Tags {
		if tag != "" && len(tags) < 50 {
			tags = append(tags, tag)
		}
	}
	metadata := map[string]string{}
	if incident.Attributes.PublicID != 0 {
		metadata["public_id"] = fmt.Sprint(incident.Attributes.PublicID)
	}
	out := NormalizedIncident{
		Provider:   ProviderDatadog,
		ExternalID: incident.ID,
		Title:      title,
		Summary:    summary,
		Status:     incident.Attributes.State,
		Severity:   severity,
		Services:   services,
		Teams:      []string{},
		Timeline:   timeline,
		Metadata:   metadata,
	}
	if parsed := parseDatadogTime(incident.Attributes.Created); parsed != nil {
		out.StartedAt = parsed
	}
	if parsed := parseDatadogTime(incident.Attributes.Resolved); parsed != nil {
		out.ResolvedAt = parsed
	}
	if parsed := parseDatadogTime(incident.Attributes.Modified); parsed != nil {
		out.UpdatedAt = parsed
	}
	if out.Services == nil {
		out.Services = []string{}
	}
	if out.Teams == nil {
		out.Teams = []string{}
	}
	if out.Timeline == nil {
		out.Timeline = []TimelineEntry{}
	}
	if out.Metadata == nil {
		out.Metadata = map[string]string{}
	}
	for _, tag := range tags {
		out.Metadata["tag:"+truncateKey(tag)] = tag
		if len(out.Metadata) >= 60 {
			break
		}
	}
	return out
}

func parseDatadogTime(raw string) *time.Time {
	if raw == "" {
		return nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return &parsed
	}
	return nil
}

func truncateKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) > 64 {
		value = value[:64]
	}
	return value
}

func mapDatadogStatus(status int) error {
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusUnauthorized:
		return &ProviderError{Code: CodeDatadogAuthenticationFailed, Status: StatusAuthenticationFailed}
	case status == http.StatusForbidden:
		return &ProviderError{Code: CodeDatadogPermissionRequired, Status: StatusPermissionDenied}
	case status == http.StatusTooManyRequests:
		return &ProviderError{Code: CodeProviderRateLimited, Status: StatusRateLimited}
	case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
		return &ProviderError{Code: CodeProviderTimeout, Status: StatusTimeout}
	case status >= 500:
		return &ProviderError{Code: CodeProviderError, Status: StatusProviderError}
	default:
		return &ProviderError{Code: CodeProviderError, Status: StatusProviderError}
	}
}
