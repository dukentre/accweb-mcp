package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/assetto-corsa-web/accweb/internal/pkg/cfg"
	"github.com/assetto-corsa-web/accweb/internal/pkg/server_manager"
	"github.com/gin-gonic/gin"
)

func TestSchemaObjectOmitsEmptyRequired(t *testing.T) {
	schema := schemaObject(map[string]any{})

	if _, ok := schema["required"]; ok {
		t.Fatalf("schemaObject without required fields must omit required, got %#v", schema["required"])
	}

	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"required":null`) {
		t.Fatalf("schema must not serialize required:null: %s", data)
	}
}

func TestSchemaObjectIncludesRequiredArray(t *testing.T) {
	schema := schemaObject(map[string]any{
		"instanceId": schemaString("ACCWeb instance id"),
	}, "instanceId")

	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("required must be []string, got %T", schema["required"])
	}
	if len(required) != 1 || required[0] != "instanceId" {
		t.Fatalf("unexpected required fields: %#v", required)
	}
}

func TestMCPToolsListInputSchemasAreValidObjects(t *testing.T) {
	var h Handler
	result := h.mcpToolsList()

	tools, ok := result["tools"].([]mcpTool)
	if !ok {
		t.Fatalf("tools/list result must contain []mcpTool, got %T", result["tools"])
	}
	if len(tools) == 0 {
		t.Fatal("tools/list returned no tools")
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"required":null`) {
		t.Fatalf("tools/list must not serialize required:null: %s", data)
	}

	for _, tool := range tools {
		if got := tool.InputSchema["type"]; got != "object" {
			t.Fatalf("%s inputSchema.type must be object, got %#v", tool.Name, got)
		}
		assertNoNilRequired(t, tool.Name+".inputSchema", tool.InputSchema)
	}
}

func TestMCPToolsListAnnotatesReadOnlyTools(t *testing.T) {
	var h Handler
	result := h.mcpToolsList()

	tools, ok := result["tools"].([]mcpTool)
	if !ok {
		t.Fatalf("tools/list result must contain []mcpTool, got %T", result["tools"])
	}

	expectedReadOnly := map[string]bool{
		"list_instances":      true,
		"get_instance_config": true,
	}

	for _, tool := range tools {
		readOnly, ok := tool.Annotations["readOnlyHint"].(bool)
		if !ok {
			t.Fatalf("%s must include annotations.readOnlyHint", tool.Name)
		}
		if readOnly != expectedReadOnly[tool.Name] {
			t.Fatalf("%s readOnlyHint = %t, want %t", tool.Name, readOnly, expectedReadOnly[tool.Name])
		}

		openWorld, ok := tool.Annotations["openWorldHint"].(bool)
		if !ok {
			t.Fatalf("%s must include annotations.openWorldHint", tool.Name)
		}
		if openWorld {
			t.Fatalf("%s openWorldHint must be false because ACCWeb tools operate on local server instances", tool.Name)
		}
	}
}

func TestMCPRejectsUnsupportedProtocolVersionHeader(t *testing.T) {
	router := newMCPTestRouter()
	rec := performMCPRequest(router, http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`, map[string]string{
		"MCP-Protocol-Version": "2024-11-05",
	})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unsupported MCP-Protocol-Version must return 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("MCP-Protocol-Version"); got != mcpProtocolVersion {
		t.Fatalf("response must include supported MCP-Protocol-Version, got %q", got)
	}
}

func TestMCPHTTPToolsListDoesNotEmitNullRequired(t *testing.T) {
	router := newMCPTestRouter()
	rec := performMCPRequest(router, http.MethodPost, `{"jsonrpc":"2.0","id":"tools","method":"tools/list"}`, map[string]string{
		"MCP-Protocol-Version": mcpProtocolVersion,
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("tools/list must return 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("MCP-Protocol-Version"); got != mcpProtocolVersion {
		t.Fatalf("response must include MCP-Protocol-Version, got %q", got)
	}
	if strings.Contains(rec.Body.String(), `"required":null`) {
		t.Fatalf("tools/list HTTP response must not include required:null: %s", rec.Body.String())
	}

	var response mcpResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != nil {
		t.Fatalf("tools/list returned JSON-RPC error: %#v", response.Error)
	}
	if response.Result == nil {
		t.Fatal("tools/list result is missing")
	}
}

func TestMCPGetRequiresAuthBeforeMethodNotAllowed(t *testing.T) {
	router := newMCPTestRouter()
	req := httptest.NewRequest(http.MethodGet, mcpEndpointPath, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET /mcp must return 401, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = performMCPRequest(router, http.MethodGet, "", map[string]string{})
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("authenticated GET /mcp without SSE support must return 405, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMCPInitializedNotificationReturnsAccepted(t *testing.T) {
	router := newMCPTestRouter()
	rec := performMCPRequest(router, http.MethodPost, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, map[string]string{
		"MCP-Protocol-Version": mcpProtocolVersion,
	})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("notifications/initialized must return 202, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("notification response body must be empty, got %q", rec.Body.String())
	}
}

func TestMCPToolExecutionErrorsUseToolResultShape(t *testing.T) {
	config := &cfg.Config{}
	h := Handler{config: config, sm: server_manager.New(config)}

	result, err := h.mcpToolsCall(json.RawMessage(`{"name":"get_instance_config","arguments":{"instanceId":"missing"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := result["isError"]; got != true {
		t.Fatalf("tool execution error must set isError=true, got %#v", got)
	}
	if _, ok := result["content"].([]map[string]any); !ok {
		t.Fatalf("tool execution error must include text content, got %#v", result["content"])
	}
}

func assertNoNilRequired(t *testing.T, path string, value any) {
	t.Helper()

	switch v := value.(type) {
	case map[string]any:
		if required, ok := v["required"]; ok {
			switch required := required.(type) {
			case []string:
				if len(required) == 0 {
					t.Fatalf("%s.required must be omitted instead of an empty array", path)
				}
			case []any:
				if len(required) == 0 {
					t.Fatalf("%s.required must be omitted instead of an empty array", path)
				}
				for _, item := range required {
					if _, ok := item.(string); !ok {
						t.Fatalf("%s.required must contain strings, got %#v", path, item)
					}
				}
			default:
				t.Fatalf("%s.required must be a string array when present, got %T", path, required)
			}
		}
		for key, child := range v {
			assertNoNilRequired(t, path+"."+key, child)
		}
	case []any:
		for i, child := range v {
			assertNoNilRequired(t, fmt.Sprintf("%s[%d]", path, i), child)
		}
	}
}

func newMCPTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	config := &cfg.Config{MCP: cfg.MCP{Token: "test-token"}}
	handler := Handler{config: config, sm: server_manager.New(config)}

	router := gin.New()
	router.GET(mcpEndpointPath, handler.HandleMCP)
	router.POST(mcpEndpointPath, handler.HandleMCP)
	router.DELETE(mcpEndpointPath, handler.HandleMCP)
	return router
}

func performMCPRequest(router http.Handler, method, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, mcpEndpointPath, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		req.Header.Set(name, value)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
