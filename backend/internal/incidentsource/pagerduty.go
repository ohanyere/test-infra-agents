package incidentsource

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const pagerDutyBaseURL = "https://api.pagerduty.com"
const pagerDutyAPIVersion = "application/vnd.pagerduty+json;version=2"

type pagerDutyConnector struct {
	token      string
	baseURL    string
	httpClient *http.Client
}

func NewPagerDutyConnector(apiToken string) (*pagerDutyConnector, error) {
	apiToken = strings.TrimSpace(apiToken)
	if apiToken == "" || len(apiToken) > 8192 {
		return nil, validation("PagerDuty requires only api_token")
	}
	return &pagerDutyConnector{token: apiToken, baseURL: pagerDutyBaseURL, httpClient: defaultHTTPClient()}, nil
}

func newPagerDutyConnectorForTest(apiToken, baseURL string, client *http.Client) *pagerDutyConnector {
	if client == nil {
		client = defaultHTTPClient()
	}
	return &pagerDutyConnector{token: apiToken, baseURL: strings.TrimRight(baseURL, "/"), httpClient: client}
}

type pdListResponse struct {
	Incidents []pdIncident `json:"incidents"`
	Limit     int          `json:"limit"`
	Offset    int          `json:"offset"`
	More      bool         `json:"more"`
	Total     *int         `json:"total"`
}

type pdIncident struct {
	ID             string  `json:"id"`
	IncidentNumber int     `json:"incident_number"`
	Title          string  `json:"title"`
	Description    any     `json:"description"`
	Status         string  `json:"status"`
	Urgency        string  `json:"urgency"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	ResolvedAt     string  `json:"resolved_at"`
	HTMLURL        string  `json:"html_url"`
	Service        *pdRef  `json:"service"`
	Teams          []pdRef `json:"teams"`
	Priority       *pdRef  `json:"priority"`
}

type pdRef struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

type pdGetResponse struct {
	Incident pdIncident `json:"incident"`
}

type pdLogListResponse struct {
	LogEntries []pdLogEntry `json:"log_entries"`
	More       bool         `json:"more"`
}

type pdLogEntry struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	CreatedAt string `json:"created_at"`
	Summary   string `json:"summary"`
	Details   string `json:"details"`
}

func (c *pagerDutyConnector) newRequest(ctx context.Context, method, path string, query url.Values) (*http.Request, error) {
	raw := c.baseURL + path
	if len(query) > 0 {
		raw += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Token token="+c.token)
	req.Header.Set("Accept", pagerDutyAPIVersion)
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (c *pagerDutyConnector) TestConnection(ctx context.Context) error {
	query := url.Values{}
	query.Set("limit", "1")
	query.Set("offset", "0")
	req, err := c.newRequest(ctx, http.MethodGet, "/incidents", query)
	if err != nil {
		return &ProviderError{Code: CodeProviderUnreachable, Status: StatusUnreachable}
	}
	var out pdListResponse
	status, err := httpGetJSON(ctx, c.httpClient, req, &out)
	if err != nil {
		return err
	}
	if statusErr := mapPagerDutyStatus(status); statusErr != nil {
		return statusErr
	}
	return nil
}

func (c *pagerDutyConnector) ListIncidents(ctx context.Context, filter ListFilter) (IncidentPage, error) {
	filter = boundedFilter(filter)
	query := url.Values{}
	query.Set("limit", fmt.Sprint(filter.Limit))
	query.Set("offset", fmt.Sprint(filter.Offset))
	req, err := c.newRequest(ctx, http.MethodGet, "/incidents", query)
	if err != nil {
		return IncidentPage{}, &ProviderError{Code: CodeProviderUnreachable, Status: StatusUnreachable}
	}
	var out pdListResponse
	status, err := httpGetJSON(ctx, c.httpClient, req, &out)
	if err != nil {
		return IncidentPage{}, err
	}
	if statusErr := mapPagerDutyStatus(status); statusErr != nil {
		return IncidentPage{}, statusErr
	}
	items := make([]NormalizedIncident, 0, len(out.Incidents))
	for _, incident := range out.Incidents {
		mapped := mapPagerDutyIncident(incident, nil)
		if filter.Query != "" && !strings.Contains(strings.ToLower(mapped.Title+" "+mapped.Summary), strings.ToLower(filter.Query)) {
			continue
		}
		items = append(items, mapped)
	}
	page := IncidentPage{Items: items, Limit: filter.Limit, Offset: filter.Offset}
	if out.More || len(out.Incidents) == filter.Limit {
		next := filter.Offset + len(out.Incidents)
		page.NextOffset = &next
	}
	return page, nil
}

func (c *pagerDutyConnector) GetIncident(ctx context.Context, externalID string) (NormalizedIncident, error) {
	if err := validateExternalID(externalID); err != nil {
		return NormalizedIncident{}, err
	}
	path := "/incidents/" + url.PathEscape(externalID)
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return NormalizedIncident{}, &ProviderError{Code: CodeProviderUnreachable, Status: StatusUnreachable}
	}
	var out pdGetResponse
	status, err := httpGetJSON(ctx, c.httpClient, req, &out)
	if err != nil {
		return NormalizedIncident{}, err
	}
	if status == http.StatusNotFound {
		return NormalizedIncident{}, &ProviderError{Code: CodeProviderNotFound, Status: StatusProviderError}
	}
	if statusErr := mapPagerDutyStatus(status); statusErr != nil {
		return NormalizedIncident{}, statusErr
	}
	timeline := c.fetchLogEntries(ctx, externalID)
	return mapPagerDutyIncident(out.Incident, timeline), nil
}

func (c *pagerDutyConnector) fetchLogEntries(ctx context.Context, incidentID string) []TimelineEntry {
	query := url.Values{}
	query.Set("limit", "100")
	query.Set("offset", "0")
	req, err := c.newRequest(ctx, http.MethodGet, "/incidents/"+url.PathEscape(incidentID)+"/log_entries", query)
	if err != nil {
		return nil
	}
	var out pdLogListResponse
	status, err := httpGetJSON(ctx, c.httpClient, req, &out)
	if err != nil || status < 200 || status >= 300 {
		return nil
	}
	entries := make([]TimelineEntry, 0, len(out.LogEntries))
	for _, entry := range out.LogEntries {
		var timestamp *time.Time
		if parsed, parseErr := time.Parse(time.RFC3339, entry.CreatedAt); parseErr == nil {
			timestamp = &parsed
		}
		details := entry.Details
		if len(details) > 2000 {
			details = details[:2000]
		}
		summary := entry.Summary
		if len(summary) > 2000 {
			summary = summary[:2000]
		}
		entries = append(entries, TimelineEntry{Timestamp: timestamp, Type: entry.Type, Summary: summary, Details: details})
		if len(entries) >= 100 {
			break
		}
	}
	return entries
}

func mapPagerDutyIncident(incident pdIncident, timeline []TimelineEntry) NormalizedIncident {
	description := ""
	switch raw := incident.Description.(type) {
	case string:
		description = raw
	case map[string]any:
		if summary, ok := raw["summary"].(string); ok {
			description = summary
		} else {
			encoded, _ := json.Marshal(raw)
			description = string(encoded)
		}
	}
	if len(description) > 8000 {
		description = description[:8000]
	}
	title := incident.Title
	if len(title) > 500 {
		title = title[:500]
	}
	services := []string{}
	if incident.Service != nil && incident.Service.Summary != "" {
		services = append(services, incident.Service.Summary)
	}
	teams := []string{}
	for _, team := range incident.Teams {
		if team.Summary != "" {
			teams = append(teams, team.Summary)
		}
	}
	severity := incident.Urgency
	if incident.Priority != nil && incident.Priority.Summary != "" {
		severity = incident.Priority.Summary
	}
	metadata := map[string]string{}
	if incident.IncidentNumber != 0 {
		metadata["incident_number"] = fmt.Sprint(incident.IncidentNumber)
	}
	if incident.Urgency != "" {
		metadata["urgency"] = incident.Urgency
	}
	out := NormalizedIncident{
		Provider:        ProviderPagerDuty,
		ExternalID:      incident.ID,
		Title:           title,
		Summary:         description,
		Status:          incident.Status,
		Severity:        severity,
		Services:        services,
		Teams:           teams,
		Timeline:        timeline,
		Metadata:        metadata,
		SourceReference: incident.HTMLURL,
	}
	if parsed, err := time.Parse(time.RFC3339, incident.CreatedAt); err == nil {
		out.StartedAt = &parsed
	}
	if parsed, err := time.Parse(time.RFC3339, incident.ResolvedAt); err == nil {
		out.ResolvedAt = &parsed
	}
	if parsed, err := time.Parse(time.RFC3339, incident.UpdatedAt); err == nil {
		out.UpdatedAt = &parsed
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
	return out
}

func mapPagerDutyStatus(status int) error {
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusUnauthorized:
		return &ProviderError{Code: CodePagerDutyAuthenticationFailed, Status: StatusAuthenticationFailed}
	case status == http.StatusForbidden:
		return &ProviderError{Code: CodePagerDutyPermissionRequired, Status: StatusPermissionDenied}
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
