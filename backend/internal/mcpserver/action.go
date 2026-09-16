package mcpserver

import "github.com/evalplane/evalplane/backend/internal/agentgateway"

func agentAction(actionType, kind, namespace, name string, parameters map[string]any) agentgateway.ProposedAction {
	action := agentgateway.ProposedAction{Type: actionType, Parameters: parameters}
	if action.Parameters == nil {
		action.Parameters = map[string]any{}
	}
	if kind != "" || namespace != "" || name != "" {
		action.Target = &agentgateway.ActionTarget{Kind: kind, Namespace: namespace, Name: name}
	}
	return action
}
