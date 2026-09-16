package agent

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/identity"
)

func validCreate() CreateInput {
	return CreateInput{Name: "Agent", Slug: "agent", Visibility: "workspace", InitialVersion: VersionInput{VersionLabel: "0.1.0", InterfaceKind: "http", EndpointURL: "https://agent.example/v1/decide", Capabilities: []string{"kubernetes"}, Metadata: json.RawMessage(`{}`)}}
}

func TestCreateValidation(t *testing.T) {
	tests := map[string]func(*CreateInput){
		"invalid slug":           func(v *CreateInput) { v.Slug = "Bad Slug" },
		"invalid visibility":     func(v *CreateInput) { v.Visibility = "global" },
		"public creation":        func(v *CreateInput) { v.Visibility = "public" },
		"invalid interface":      func(v *CreateInput) { v.InitialVersion.InterfaceKind = "grpc" },
		"duplicate capabilities": func(v *CreateInput) { v.InitialVersion.Capabilities = []string{"kubernetes", "kubernetes"} },
		"too many capabilities": func(v *CreateInput) {
			v.InitialVersion.Capabilities = make([]string, 33)
			for i := range v.InitialVersion.Capabilities {
				v.InitialVersion.Capabilities[i] = "cap-" + strings.Repeat("a", i+1)
			}
		},
		"invalid version label": func(v *CreateInput) { v.InitialVersion.VersionLabel = "" },
		"sensitive metadata":    func(v *CreateInput) { v.InitialVersion.Metadata = json.RawMessage(`{"api_key":"no"}`) },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			value := validCreate()
			mutate(&value)
			if _, err := ValidateCreate(value); !errors.Is(err, identity.ErrValidation) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestUpdateValidationRejectsEmptyPatch(t *testing.T) {
	if _, err := ValidateUpdate(UpdateInput{}); !errors.Is(err, identity.ErrValidation) {
		t.Fatalf("error=%v", err)
	}
}

func TestCapabilitiesStoredDeterministically(t *testing.T) {
	value := validCreate()
	value.InitialVersion.Capabilities = []string{"scale_workload", "kubernetes"}
	got, err := ValidateCreate(value)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.InitialVersion.Capabilities, ",") != "kubernetes,scale_workload" {
		t.Fatalf("capabilities=%v", got.InitialVersion.Capabilities)
	}
}

func TestToolAccessModeDefaultsDirectAndMCPIsImmutableVersionInput(t *testing.T) {
	value := validCreate()
	got, err := ValidateCreate(value)
	if err != nil || got.InitialVersion.ToolAccessMode != "direct" {
		t.Fatalf("default mode=%q err=%v", got.InitialVersion.ToolAccessMode, err)
	}
	value.InitialVersion.ToolAccessMode = "mcp"
	got, err = ValidateCreate(value)
	if err != nil || got.InitialVersion.ToolAccessMode != "mcp" {
		t.Fatalf("MCP mode=%q err=%v", got.InitialVersion.ToolAccessMode, err)
	}
	value.InitialVersion.ToolAccessMode = "arbitrary"
	if _, err = ValidateCreate(value); !errors.Is(err, identity.ErrValidation) {
		t.Fatalf("invalid mode error=%v", err)
	}
}
