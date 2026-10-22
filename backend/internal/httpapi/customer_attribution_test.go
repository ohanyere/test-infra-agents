package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/customerattribution"
)

type receiptGatewayStub struct {
	calls int
	token string
}

func (*receiptGatewayStub) RegisterIssuer(context.Context, auth.Principal, string, string, string, string, string) (customerattribution.Issuer, error) {
	return customerattribution.Issuer{}, nil
}
func (*receiptGatewayStub) ListIssuers(context.Context, auth.Principal, string, string) ([]customerattribution.Issuer, error) {
	return nil, nil
}
func (*receiptGatewayStub) RevokeIssuer(context.Context, auth.Principal, string, string, string) error {
	return nil
}
func (*receiptGatewayStub) IngestReceipt(context.Context, auth.Principal, string, string, customerattribution.SignedReceipt) (customerattribution.StoredDecision, error) {
	return customerattribution.StoredDecision{}, nil
}
func (*receiptGatewayStub) ListDecisions(context.Context, auth.Principal, string, string, string) ([]customerattribution.StoredDecision, error) {
	return nil, nil
}
func (s *receiptGatewayStub) IngestGatewayReceipt(_ context.Context, token string, _ customerattribution.SignedReceipt) (customerattribution.StoredDecision, error) {
	s.calls++
	s.token = token
	return customerattribution.StoredDecision{ID: "accepted"}, nil
}

func TestGatewayReceiptHTTPRequiresBearerCredential(t *testing.T) {
	stub := &receiptGatewayStub{}
	h := customerAttributionHandlers{service: stub, logger: slog.Default()}
	body, _ := json.Marshal(customerattribution.SignedReceipt{Payload: customerattribution.ReceiptPayload{AuditID: "test"}, Signature: "test"})
	request := httptest.NewRequest(http.MethodPost, "/v1/execution-issuers/receipts", strings.NewReader(string(body)))
	response := httptest.NewRecorder()
	h.ingestGatewayReceipt(response, request)
	if response.Code != http.StatusUnauthorized || stub.calls != 0 {
		t.Fatalf("unauthenticated receipt accepted: %d calls=%d", response.Code, stub.calls)
	}
	request = httptest.NewRequest(http.MethodPost, "/v1/execution-issuers/receipts", strings.NewReader(string(body)))
	request.Header.Set("Authorization", "Bearer issuer-token")
	response = httptest.NewRecorder()
	h.ingestGatewayReceipt(response, request)
	if response.Code != http.StatusCreated || stub.calls != 1 || stub.token != "issuer-token" {
		t.Fatalf("credential was not passed to verifier: %d calls=%d", response.Code, stub.calls)
	}
}
