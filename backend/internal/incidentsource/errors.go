package incidentsource

import "errors"

const (
	CodePagerDutyAuthenticationFailed = "PAGERDUTY_AUTHENTICATION_FAILED"
	CodePagerDutyPermissionRequired   = "PAGERDUTY_INCIDENT_READ_PERMISSION_REQUIRED"
	CodeDatadogAuthenticationFailed   = "DATADOG_AUTHENTICATION_FAILED"
	CodeDatadogPermissionRequired     = "DATADOG_INCIDENT_READ_PERMISSION_REQUIRED"
	CodeDatadogInvalidSite            = "DATADOG_INVALID_SITE"
	CodeProviderRateLimited           = "PROVIDER_RATE_LIMITED"
	CodeProviderNotFound              = "PROVIDER_INCIDENT_NOT_FOUND"
	CodeProviderTimeout               = "PROVIDER_TIMEOUT"
	CodeProviderUnreachable           = "PROVIDER_UNREACHABLE"
	CodeProviderMalformedResponse     = "PROVIDER_MALFORMED_RESPONSE"
	CodeProviderError                 = "PROVIDER_ERROR"
)

type ProviderError struct {
	Code   string
	Status string
}

func (e *ProviderError) Error() string { return e.Code }

func IsProviderError(err error, code string) bool {
	var providerError *ProviderError
	return errors.As(err, &providerError) && providerError.Code == code
}
