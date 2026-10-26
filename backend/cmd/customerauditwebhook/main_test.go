package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/customercollector"
	"github.com/evalplane/evalplane/backend/internal/kubeaudit"
)

type recordingIntake struct{ count int }

func (r *recordingIntake) IngestAuthenticatedAuditEvent(_ context.Context, _ *x509.Certificate, _ []byte) (customercollector.AuthenticatedAuditResult, error) {
	r.count++
	return customercollector.AuthenticatedAuditResult{}, nil
}

func TestWebhookRequiresDirectTLSClientCertificate(t *testing.T) {
	intake := &recordingIntake{}
	handler := newHandler(intake)
	event := kubeaudit.Event{APIVersion: kubeaudit.AuditAPIVersion, Kind: "Event", AuditID: "audit-id", Stage: "ResponseComplete", Verb: "patch", RequestAt: time.Now(), StageAt: time.Now(), ObjectRef: kubeaudit.ObjectRef{APIGroup: "apps", APIVersion: "v1", Resource: "deployments", Namespace: "checkout", Name: "checkout"}, Response: kubeaudit.Response{Code: 200}}
	content, _ := json.Marshal(struct {
		APIVersion string            `json:"apiVersion"`
		Kind       string            `json:"kind"`
		Items      []kubeaudit.Event `json:"items"`
	}{kubeaudit.AuditAPIVersion, "EventList", []kubeaudit.Event{event}})
	request := httptest.NewRequest(http.MethodPost, "/audit", nil)
	request.Body = http.NoBody
	request.Header.Set("X-Forwarded-Client-Cert", "pretend-apiserver")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || intake.count != 0 {
		t.Fatalf("forwarded certificate accepted: %d %d", response.Code, intake.count)
	}
	request = httptest.NewRequest(http.MethodPost, "/audit", bytes.NewReader(content))
	request.TLS = &tls.ConnectionState{HandshakeComplete: true, PeerCertificates: []*x509.Certificate{{}}}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || intake.count != 1 {
		t.Fatalf("direct peer certificate was not passed to intake: %d %d", response.Code, intake.count)
	}
}
