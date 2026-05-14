package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/assetto-corsa-web/accweb/internal/pkg/cfg"
	"github.com/assetto-corsa-web/accweb/internal/pkg/instance"
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
		"list_tracks":          true,
		"list_instances":       true,
		"get_instance_status":  true,
		"get_instance_weather": true,
		"get_instance_track":   true,
		"get_instance_config":  true,
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

func TestMCPToolsListProvidesOutputSchemas(t *testing.T) {
	var h Handler
	result := h.mcpToolsList()

	tools, ok := result["tools"].([]mcpTool)
	if !ok {
		t.Fatalf("tools/list result must contain []mcpTool, got %T", result["tools"])
	}

	for _, tool := range tools {
		if got := tool.OutputSchema["type"]; got != "object" {
			t.Fatalf("%s outputSchema.type must be object, got %#v", tool.Name, got)
		}
	}
}

func TestMCPInitializeDeclaresCompletionsCapability(t *testing.T) {
	var h Handler
	result := h.mcpInitialize()
	capabilities := result["capabilities"].(map[string]any)
	if _, ok := capabilities["completions"]; !ok {
		t.Fatalf("initialize must declare completions capability: %#v", capabilities)
	}
}

func TestMCPTrackCatalogResourceToolAndParameterReference(t *testing.T) {
	config := &cfg.Config{}
	h := Handler{config: config, sm: server_manager.New(config)}

	resource, err := h.mcpResourcesRead(json.RawMessage(`{"uri":"accweb://tracks"}`))
	if err != nil {
		t.Fatal(err)
	}
	resourceData, err := json.Marshal(resource)
	if err != nil {
		t.Fatal(err)
	}
	resourceText := string(resourceData)
	for _, expected := range []string{"monza", "spa", "nurburgring_24h"} {
		if !strings.Contains(resourceText, expected) {
			t.Fatalf("track catalog resource should include %s: %s", expected, resourceText)
		}
	}
	for _, carGroup := range []string{"GT3", "GT4", "TCX"} {
		if strings.Contains(resourceText, carGroup) {
			t.Fatalf("track catalog must not include car group %s: %s", carGroup, resourceText)
		}
	}

	tool, err := h.mcpToolsCall(json.RawMessage(`{"name":"list_tracks","arguments":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	content := tool["structuredContent"].(map[string]any)
	tracks := content["tracks"].([]accTrack)
	if len(tracks) != 25 {
		t.Fatalf("expected 25 ACC tracks, got %d", len(tracks))
	}
	if tracks[0].ID == "GT3" {
		t.Fatal("list_tracks returned a car group instead of track ids")
	}

	var trackDoc *accParameterDoc
	docs := accParameterDocs()
	for i, doc := range docs {
		if doc.Path == "acc.event.track" {
			trackDoc = &docs[i]
			break
		}
	}
	if trackDoc == nil {
		t.Fatal("missing acc.event.track parameter doc")
	}
	if trackDoc.AllowedValuesResource != "accweb://tracks" {
		t.Fatalf("acc.event.track allowedValuesResource = %q", trackDoc.AllowedValuesResource)
	}
}

func TestMCPTrackCompletion(t *testing.T) {
	var h Handler
	result, err := h.mcpCompletionComplete(json.RawMessage(`{"ref":{"type":"ref/prompt","name":"configure_quick_race"},"argument":{"name":"track","value":"mo"}}`))
	if err != nil {
		t.Fatal(err)
	}
	values := result["completion"].(map[string]any)["values"].([]string)
	if len(values) == 0 || values[0] != "monza" {
		t.Fatalf("expected monza completion first, got %#v", values)
	}

	result, err = h.mcpCompletionComplete(json.RawMessage(`{"ref":{"type":"ref/resource","uri":"accweb://tracks/{trackId}"},"argument":{"name":"trackId","value":"sp"}}`))
	if err != nil {
		t.Fatal(err)
	}
	values = result["completion"].(map[string]any)["values"].([]string)
	if len(values) == 0 || values[0] != "spa" {
		t.Fatalf("expected canonical spa id completion before aliases, got %#v", values)
	}

	result, err = h.mcpCompletionComplete(json.RawMessage(`{"ref":{"type":"ref/resource","uri":"accweb://tracks/{trackId}"},"argument":{"name":"trackId","value":"сп"}}`))
	if err != nil {
		t.Fatal(err)
	}
	values = result["completion"].(map[string]any)["values"].([]string)
	if len(values) == 0 || values[0] != "spa" {
		t.Fatalf("expected spa completion for Russian prefix, got %#v", values)
	}

	result, err = h.mcpCompletionComplete(json.RawMessage(`{"ref":{"type":"ref/prompt","name":"configure_quick_race"},"argument":{"name":"track","value":"GT"}}`))
	if err != nil {
		t.Fatal(err)
	}
	values = result["completion"].(map[string]any)["values"].([]string)
	if len(values) != 0 {
		t.Fatalf("short GT prefix must not match tracks by substring or return car groups: %#v", values)
	}
	for _, value := range values {
		if strings.HasPrefix(value, "GT") {
			t.Fatalf("track completion must not return car groups: %#v", values)
		}
	}
}

func TestWaitForMCPInstanceStoppedWaitsUntilProcessStateClears(t *testing.T) {
	fake := &fakeMCPRunningInstance{runningResponses: 2}

	err := waitForMCPInstanceStopped(fake, 50*time.Millisecond, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls < 3 {
		t.Fatalf("expected polling until IsRunning becomes false, got %d calls", fake.calls)
	}
}

func TestWaitForMCPInstanceStoppedTimeout(t *testing.T) {
	fake := &fakeMCPRunningInstance{runningResponses: 100}

	err := waitForMCPInstanceStopped(fake, 2*time.Millisecond, time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout while instance remains running")
	}
}

type fakeMCPRunningInstance struct {
	calls            int
	runningResponses int
}

func (f *fakeMCPRunningInstance) IsRunning() bool {
	f.calls++
	return f.calls <= f.runningResponses
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

func TestMCPDomainToolsReturnStructuredWeatherAndTrack(t *testing.T) {
	h := newMCPTestHandlerWithInstances(t, testInstanceSpec{
		id:            "1778596425",
		name:          "Dukentre SERVER",
		track:         "monza",
		ambientTemp:   26,
		cloudLevel:    0.1,
		rain:          0,
		randomness:    2,
		adminPass:     "228339227",
		serverPass:    "server-secret",
		spectatorPass: "spectator-secret",
	})

	weather, err := h.mcpToolsCall(json.RawMessage(`{"name":"get_instance_weather","arguments":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	weatherContent := weather["structuredContent"].(map[string]any)
	weatherFields := weatherContent["weather"].(map[string]any)
	if got := weatherFields["ambientTempC"]; got != 26 {
		t.Fatalf("ambientTempC = %#v, want 26", got)
	}
	if got := weatherFields["cloudLevel"]; got != 0.1 {
		t.Fatalf("cloudLevel = %#v, want 0.1", got)
	}
	if got := weatherFields["summary"]; !strings.Contains(got.(string), "26C") {
		t.Fatalf("weather summary should mention temperature, got %#v", got)
	}

	track, err := h.mcpToolsCall(json.RawMessage(`{"name":"get_instance_track","arguments":{"instanceIdOrName":"duken"}}`))
	if err != nil {
		t.Fatal(err)
	}
	trackContent := track["structuredContent"].(map[string]any)
	trackFields := trackContent["track"].(map[string]any)
	if got := trackFields["effectiveTrack"]; got != "monza" {
		t.Fatalf("effectiveTrack = %#v, want monza", got)
	}
}

func TestMCPResolverFallsBackToSingleInstanceForUserShorthand(t *testing.T) {
	h := newMCPTestHandlerWithInstances(t, testInstanceSpec{id: "1778596425", name: "Dukentre SERVER", track: "monza"})

	result, err := h.mcpToolsCall(json.RawMessage(`{"name":"get_instance_status","arguments":{"instanceIdOrName":"1"}}`))
	if err != nil {
		t.Fatal(err)
	}
	content := result["structuredContent"].(map[string]any)
	instanceRef := content["instance"].(map[string]any)
	if got := instanceRef["id"]; got != "1778596425" {
		t.Fatalf("single-instance fallback selected %#v", got)
	}
}

func TestMCPInstanceConfigRedactsSecrets(t *testing.T) {
	h := newMCPTestHandlerWithInstances(t, testInstanceSpec{
		id:            "1778596425",
		name:          "Dukentre SERVER",
		track:         "monza",
		adminPass:     "228339227",
		serverPass:    "server-secret",
		spectatorPass: "spectator-secret",
	})

	result, err := h.mcpToolsCall(json.RawMessage(`{"name":"get_instance_config","arguments":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, secret := range []string{"228339227", "server-secret", "spectator-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("get_instance_config leaked secret %q in %s", secret, text)
		}
	}
	if !strings.Contains(text, "[redacted]") {
		t.Fatalf("get_instance_config should include redaction markers: %s", text)
	}

	resource, err := h.mcpResourcesRead(json.RawMessage(`{"uri":"accweb://instances/1778596425/config"}`))
	if err != nil {
		t.Fatal(err)
	}
	resourceData, err := json.Marshal(resource)
	if err != nil {
		t.Fatal(err)
	}
	resourceText := string(resourceData)
	for _, secret := range []string{"228339227", "server-secret", "spectator-secret"} {
		if strings.Contains(resourceText, secret) {
			t.Fatalf("config resource leaked secret %q in %s", secret, resourceText)
		}
	}
}

func TestMCPInstanceNotFoundErrorIsActionable(t *testing.T) {
	h := newMCPTestHandlerWithInstances(t,
		testInstanceSpec{id: "111", name: "Alpha", track: "monza"},
		testInstanceSpec{id: "222", name: "Beta", track: "spa"},
	)

	result, err := h.mcpToolsCall(json.RawMessage(`{"name":"get_instance_weather","arguments":{"instanceIdOrName":"missing"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := result["isError"]; got != true {
		t.Fatalf("expected isError=true, got %#v", got)
	}
	content := result["structuredContent"].(map[string]any)
	if got := content["code"]; got != "instance_not_found" {
		t.Fatalf("error code = %#v, want instance_not_found", got)
	}
	if _, ok := content["availableInstances"]; !ok {
		t.Fatalf("actionable error should include availableInstances: %#v", content)
	}
}

func TestMCPResourceTemplatesListIncludesDomainResources(t *testing.T) {
	var h Handler
	result := h.mcpResourceTemplatesList()
	templates := result["resourceTemplates"].([]mcpResourceTemplate)

	seen := map[string]bool{}
	for _, template := range templates {
		seen[template.URITemplate] = true
		if got := template.Annotations["audience"]; got == nil {
			t.Fatalf("%s must include audience annotation", template.URITemplate)
		}
	}
	for _, uri := range []string{
		"accweb://instances",
		"accweb://instances/{instanceId}/status",
		"accweb://instances/{instanceId}/weather",
		"accweb://instances/{instanceId}/config",
	} {
		if !seen[uri] {
			t.Fatalf("missing resource template %s", uri)
		}
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

type testInstanceSpec struct {
	id            string
	name          string
	track         string
	ambientTemp   int
	cloudLevel    float64
	rain          float64
	randomness    int
	adminPass     string
	serverPass    string
	spectatorPass string
}

func newMCPTestHandlerWithInstances(t *testing.T, specs ...testInstanceSpec) Handler {
	t.Helper()

	baseDir := t.TempDir()
	for _, spec := range specs {
		writeTestInstance(t, baseDir, spec)
	}

	config := &cfg.Config{ConfigPath: baseDir, MCP: cfg.MCP{Token: "test-token"}}
	sm := server_manager.New(config)
	if err := sm.LoadAll(); err != nil {
		t.Fatal(err)
	}
	return Handler{config: config, sm: sm}
}

func writeTestInstance(t *testing.T, baseDir string, spec testInstanceSpec) {
	t.Helper()
	if spec.ambientTemp == 0 {
		spec.ambientTemp = 26
	}
	if spec.name == "" {
		spec.name = "Test SERVER"
	}
	if spec.track == "" {
		spec.track = "monza"
	}

	dir := filepath.Join(baseDir, spec.id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	accCfg := instance.AccConfigFiles{
		Configuration: instance.ConfigurationJson{UdpPort: 9231, TcpPort: 9232, MaxConnections: 20},
		Settings: instance.SettingsJson{
			ServerName:        spec.name,
			Password:          spec.serverPass,
			AdminPassword:     spec.adminPass,
			SpectatorPassword: spec.spectatorPass,
			MaxCarSlots:       20,
			CarGroup:          "GT3",
		},
		Event: instance.EventJson{
			Track:                         spec.track,
			AmbientTemp:                   spec.ambientTemp,
			CloudLevel:                    spec.cloudLevel,
			Rain:                          spec.rain,
			WeatherRandomness:             spec.randomness,
			IsFixedConditionQualification: 1,
			Sessions: []instance.SessionSettings{
				{HourOfDay: 12, DayOfWeekend: 2, TimeMultiplier: 1, SessionType: "Q", SessionDurationMinutes: 5},
				{HourOfDay: 13, DayOfWeekend: 3, TimeMultiplier: 1, SessionType: "R", SessionDurationMinutes: 20},
			},
		},
		Entrylist:   instance.EntrylistJson{Entries: []instance.EntrySettings{}},
		Bop:         instance.BopJson{Entries: []instance.BopSettings{}},
		AssistRules: instance.AssistRulesJson{},
	}
	instance.SetConfigVersion(&accCfg)

	writeJSON(t, dir, "accwebConfig.json", instance.AccWebConfigJson{
		ID:        spec.id,
		Settings:  instance.AccWebSettingsJson{},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	writeJSON(t, dir, "configuration.json", accCfg.Configuration)
	writeJSON(t, dir, "settings.json", accCfg.Settings)
	writeJSON(t, dir, "event.json", accCfg.Event)
	writeJSON(t, dir, "eventRules.json", accCfg.EventRules)
	writeJSON(t, dir, "entrylist.json", accCfg.Entrylist)
	writeJSON(t, dir, "bop.json", accCfg.Bop)
	writeJSON(t, dir, "assistRules.json", accCfg.AssistRules)
}

func writeJSON(t *testing.T, dir string, name string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
		t.Fatal(err)
	}
}
