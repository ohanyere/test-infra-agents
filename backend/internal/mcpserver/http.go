package mcpserver

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var toolSchemaCache = mcp.NewSchemaCache()

func (s *Service) Handler() http.Handler {
	transport := mcp.NewStreamableHTTPHandler(func(request *http.Request) *mcp.Server {
		authenticated, err := contextAuth(request.Context())
		if err != nil {
			return nil
		}
		server := mcp.NewServer(&mcp.Implementation{Name: "evalplane", Version: ToolContract}, &mcp.ServerOptions{SchemaCache: toolSchemaCache})
		server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
			return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
				if method != "tools/call" {
					return next(ctx, method, request)
				}
				name := "unknown_tool"
				if call, ok := request.(*mcp.CallToolRequest); ok && call.Params != nil && call.Params.Name != "" {
					name = call.Params.Name
				}
				s.record(ctx, authenticated.scope.RunID, "mcp_tool_called", name, nil)
				if !slices.Contains(s.effectiveTools(authenticated.scope), name) {
					s.record(ctx, authenticated.scope.RunID, "mcp_tool_denied", name, map[string]any{"status": "denied", "authorized": false, "denial_code": "unsupported_action"})
				}
				if err := s.store.Consume(ctx, authenticated.grant.ID); err != nil {
					s.record(ctx, authenticated.scope.RunID, "mcp_tool_denied", name, map[string]any{"denial_code": "tool_call_budget_exhausted"})
					return nil, err
				}
				result, callErr := next(ctx, method, request)
				if callErr != nil {
					s.record(ctx, authenticated.scope.RunID, "mcp_tool_failed", name, map[string]any{"reason_code": "protocol_or_tool_error"})
				}
				return result, callErr
			}
		})
		for _, tool := range s.effectiveTools(authenticated.scope) {
			s.addTool(server, tool)
		}
		return server
	}, &mcp.StreamableHTTPOptions{Stateless: true, PropagateRequestCancellation: true, MaxRequestBodyBytes: 64 << 10})
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		token, ok := bearerToken(request.Header.Get("Authorization"))
		if !ok {
			http.Error(writer, "MCP grant required", http.StatusUnauthorized)
			return
		}
		grant, scope, err := s.resolve(request.Context(), token, false)
		if err != nil {
			http.Error(writer, "MCP grant invalid or inactive", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(request.Context(), authContextKey{}, authenticated{grant: grant, scope: scope})
		transport.ServeHTTP(writer, request.WithContext(ctx))
	})
}

func bearerToken(header string) (string, bool) {
	parts := strings.Fields(header)
	returnToken := ""
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		returnToken = parts[1]
	}
	return returnToken, returnToken != ""
}

func (s *Service) addTool(server *mcp.Server, name string) {
	switch name {
	case ToolObserveWorkloads, ToolObservePods, ToolObserveEvents, ToolGetTelemetry:
		mcp.AddTool(server, &mcp.Tool{Name: name, Description: "Read bounded evaluation-scoped infrastructure evidence."}, func(ctx context.Context, _ *mcp.CallToolRequest, _ EmptyInput) (*mcp.CallToolResult, ToolOutput, error) {
			out, err := s.observeTool(ctx, name)
			return nil, out, err
		})
	case ToolScaleWorkload:
		mcp.AddTool(server, &mcp.Tool{Name: name, Description: "Scale a permitted run-owned Deployment."}, func(ctx context.Context, _ *mcp.CallToolRequest, in ScaleInput) (*mcp.CallToolResult, ToolOutput, error) {
			if in.Kind != "Deployment" || in.Namespace == "" || in.Name == "" || in.Replicas < 1 || in.Replicas > 20 {
				return nil, ToolOutput{Status: "denied", Code: "invalid_action_parameters"}, errors.New("invalid bounded scale arguments")
			}
			action := agentAction("scale_workload", in.Kind, in.Namespace, in.Name, map[string]any{"replicas": in.Replicas})
			out, err := s.actionTool(ctx, name, action)
			return nil, out, err
		})
	case ToolRestartPod:
		mcp.AddTool(server, &mcp.Tool{Name: name, Description: "Restart a permitted run-owned Pod."}, func(ctx context.Context, _ *mcp.CallToolRequest, in RestartInput) (*mcp.CallToolResult, ToolOutput, error) {
			if in.Kind != "Pod" || in.Namespace == "" || in.Name == "" {
				return nil, ToolOutput{Status: "denied", Code: "invalid_action_parameters"}, errors.New("invalid bounded restart arguments")
			}
			out, err := s.actionTool(ctx, name, agentAction("restart_pod", in.Kind, in.Namespace, in.Name, nil))
			return nil, out, err
		})
	case ToolNoOp:
		mcp.AddTool(server, &mcp.Tool{Name: name, Description: "Record completion without mutating infrastructure."}, func(ctx context.Context, _ *mcp.CallToolRequest, _ EmptyInput) (*mcp.CallToolResult, ToolOutput, error) {
			out, err := s.actionTool(ctx, name, agentAction("no_op", "", "", "", nil))
			return nil, out, err
		})
	}
}
