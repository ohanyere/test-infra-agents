package agentgateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/evalplane/evalplane/backend/internal/evidence"
)

type EvidenceSink interface {
	AppendRunEvent(context.Context, evidence.AppendRunEventInput) (evidence.RunEvent, error)
	RecordAgentAction(context.Context, evidence.RecordAgentActionInput) (evidence.AgentAction, error)
}

type HTTPGateway struct {
	client      *http.Client
	sink        EvidenceSink
	workerOwned bool
}

func NewHTTPGateway(client *http.Client, sink EvidenceSink) *HTTPGateway {
	if client == nil {
		client = &http.Client{}
	}
	return &HTTPGateway{client: client, sink: sink}
}

// WithoutEvidence lets the orchestrator own the proposal lifecycle exactly once.
func (g *HTTPGateway) WithoutEvidence() *HTTPGateway {
	out := NewHTTPGateway(g.client, nil)
	out.workerOwned = true
	return out
}

func (g *HTTPGateway) Invoke(ctx context.Context, connection Connection, request Request) (Response, error) {
	if err := ValidateConnection(connection); err != nil {
		return Response{}, err
	}
	if request.ProtocolVersion == "" {
		request.ProtocolVersion = ProtocolV1
	}
	if request.SessionID == "" {
		request.SessionID = newSessionID()
	}
	if err := ValidateRequest(request); err != nil {
		return Response{}, err
	}
	timeout := connection.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	invokeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	body, err := json.Marshal(request)
	if err != nil {
		return Response{}, fmt.Errorf("%w: encode request", ErrInvalidRequest)
	}
	if err := g.recordObservation(invokeCtx, request); err != nil {
		return Response{}, fmt.Errorf("record agent observation: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(invokeCtx, http.MethodPost, connection.EndpointURL, bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("%w: create request", ErrInvalidConnection)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if connection.BearerToken != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+connection.BearerToken)
	}
	httpResponse, err := g.client.Do(httpRequest)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return Response{}, ErrCanceled
		}
		if errors.Is(invokeCtx.Err(), context.DeadlineExceeded) {
			return Response{}, ErrTimeout
		}
		return Response{}, errors.New("agent HTTP request failed")
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		if httpResponse.StatusCode == http.StatusUnauthorized || httpResponse.StatusCode == http.StatusForbidden {
			return Response{}, ErrAuthentication
		}
		return Response{}, fmt.Errorf("agent returned HTTP status %d", httpResponse.StatusCode)
	}
	responseBody, err := io.ReadAll(io.LimitReader(httpResponse.Body, MaxResponseBytes+1))
	if err != nil {
		return Response{}, errors.New("read agent HTTP response failed")
	}
	if int64(len(responseBody)) > MaxResponseBytes {
		return Response{}, ErrResponseTooLarge
	}
	var response Response
	decoder := json.NewDecoder(bytes.NewReader(responseBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return Response{}, fmt.Errorf("%w: malformed JSON", ErrInvalidResponse)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Response{}, fmt.Errorf("%w: malformed JSON", ErrInvalidResponse)
	}
	if g.workerOwned {
		// The worker persists and independently authorizes each bounded proposal.
		if response.ProtocolVersion != ProtocolV1 || len(response.Actions) > MaxActions || len(response.ReasoningSummary) > 2000 {
			return Response{}, ErrInvalidResponse
		}
	} else if err := ValidateResponse(response, request.AvailableActions); err != nil {
		return Response{}, err
	}
	if err := g.recordResponse(invokeCtx, request, response); err != nil {
		return Response{}, fmt.Errorf("record agent response: %w", err)
	}
	return response, nil
}

func newSessionID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return fmt.Sprintf("session-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value[:])
}

func (g *HTTPGateway) recordObservation(ctx context.Context, request Request) error {
	if g.sink == nil {
		return nil
	}
	payload, _ := json.Marshal(map[string]any{"observation": request.Observation, "session_id": request.SessionID})
	_, err := g.sink.AppendRunEvent(ctx, evidence.AppendRunEventInput{RunID: request.RunID, EventType: "observation", OccurredAt: time.Now().UTC(), Summary: "observation sent to agent", Payload: payload})
	return err
}

func (g *HTTPGateway) recordResponse(ctx context.Context, request Request, response Response) error {
	if g.sink == nil {
		return nil
	}
	summary := response.ReasoningSummary
	if summary == "" {
		summary = "agent returned a decision"
	}
	payload, _ := json.Marshal(map[string]any{"protocol_version": response.ProtocolVersion, "session_id": request.SessionID, "decision_summary": response.ReasoningSummary})
	if _, err := g.sink.AppendRunEvent(ctx, evidence.AppendRunEventInput{RunID: request.RunID, EventType: "decision", OccurredAt: time.Now().UTC(), Summary: summary, Payload: payload}); err != nil {
		return err
	}
	for _, action := range response.Actions {
		actionPayload, _ := json.Marshal(action)
		event, err := g.sink.AppendRunEvent(ctx, evidence.AppendRunEventInput{RunID: request.RunID, EventType: "action", OccurredAt: time.Now().UTC(), Summary: "agent proposed " + action.Type, Payload: actionPayload})
		if err != nil {
			return err
		}
		var targetType, targetRef *string
		if action.Target != nil {
			kind, ref := action.Target.Kind, action.Target.Namespace+"/"+action.Target.Name
			targetType, targetRef = &kind, &ref
		}
		empty := json.RawMessage(`{}`)
		if _, err := g.sink.RecordAgentAction(ctx, evidence.RecordAgentActionInput{RunID: request.RunID, RunEventID: &event.ID, ActionType: action.Type, TargetType: targetType, TargetRef: targetRef, Status: "proposed", Authorized: false, RequestPayload: actionPayload, ResultPayload: empty}); err != nil {
			return err
		}
	}
	return nil
}
