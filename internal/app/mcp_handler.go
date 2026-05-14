package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/assetto-corsa-web/accweb/internal/pkg/instance"
	"github.com/gin-gonic/gin"
)

const (
	mcpProtocolVersion = "2025-06-18"
	mcpEndpointPath    = "/mcp"

	mcpRestartStopTimeout      = 15 * time.Second
	mcpRestartStopPollInterval = 100 * time.Millisecond
)

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *mcpError `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpTool struct {
	Name         string         `json:"name"`
	Title        string         `json:"title,omitempty"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema"`
	Annotations  map[string]any `json:"annotations,omitempty"`
}

type mcpResource struct {
	URI         string         `json:"uri"`
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	MimeType    string         `json:"mimeType,omitempty"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

type mcpResourceTemplate struct {
	URITemplate string         `json:"uriTemplate"`
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	MimeType    string         `json:"mimeType,omitempty"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

type mcpPrompt struct {
	Name        string              `json:"name"`
	Title       string              `json:"title,omitempty"`
	Description string              `json:"description,omitempty"`
	Arguments   []mcpPromptArgument `json:"arguments,omitempty"`
}

type mcpPromptArgument struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type accParameterDoc struct {
	File                  string   `json:"file"`
	Path                  string   `json:"path"`
	Type                  string   `json:"type"`
	Description           string   `json:"description"`
	Values                []string `json:"values,omitempty"`
	Range                 string   `json:"range,omitempty"`
	AllowedValuesResource string   `json:"allowedValuesResource,omitempty"`
}

type mcpSetParametersArgs struct {
	InstanceID    string              `json:"instanceId"`
	Updates       []mcpParameterPatch `json:"updates"`
	RestartIfLive bool                `json:"restartIfLive"`
}

type mcpParameterPatch struct {
	Path  string `json:"path"`
	Value any    `json:"value"`
}

type mcpInstanceSelectorArgs struct {
	InstanceID       string `json:"instanceId"`
	InstanceIDOrName string `json:"instanceIdOrName"`
}

type mcpCreateQuickRaceArgs struct {
	ServerName      string `json:"serverName"`
	Track           string `json:"track"`
	CarGroup        string `json:"carGroup"`
	MaxCarSlots     int    `json:"maxCarSlots"`
	QualifyingMins  int    `json:"qualifyingMinutes"`
	RaceMins        int    `json:"raceMinutes"`
	HourOfDay       int    `json:"hourOfDay"`
	RegisterToLobby int    `json:"registerToLobby"`
	LanDiscovery    int    `json:"lanDiscovery"`
	TCPPort         int    `json:"tcpPort"`
	UDPPort         int    `json:"udpPort"`
}

func (h *Handler) HandleMCP(c *gin.Context) {
	c.Header("MCP-Protocol-Version", mcpProtocolVersion)

	if c.Request.Method != http.MethodPost && c.Request.Method != http.MethodGet && c.Request.Method != http.MethodDelete {
		c.Status(http.StatusMethodNotAllowed)
		return
	}

	if !h.authorizeMCP(c) {
		return
	}

	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodDelete {
		c.Header("Allow", "POST")
		c.Status(http.StatusMethodNotAllowed)
		return
	}

	if !h.validateMCPProtocolVersion(c) {
		return
	}

	var req mcpRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, mcpResponse{
			JSONRPC: "2.0",
			ID:      nil,
			Error:   &mcpError{Code: -32700, Message: "Parse error"},
		})
		return
	}

	if req.ID == nil && strings.HasPrefix(req.Method, "notifications/") {
		c.Status(http.StatusAccepted)
		return
	}

	c.JSON(http.StatusOK, h.dispatchMCP(req))
}

func (h *Handler) validateMCPProtocolVersion(c *gin.Context) bool {
	requested := c.GetHeader("MCP-Protocol-Version")
	if requested == "" || requested == mcpProtocolVersion {
		return true
	}

	c.JSON(http.StatusBadRequest, gin.H{
		"error":    "unsupported MCP protocol version",
		"version":  mcpProtocolVersion,
		"received": requested,
	})
	return false
}

func (h *Handler) authorizeMCP(c *gin.Context) bool {
	if !h.isMCPOriginAllowed(c.GetHeader("Origin")) {
		c.JSON(http.StatusForbidden, gin.H{"error": "origin is not allowed"})
		return false
	}

	token := h.mcpToken()
	if token == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MCP token is not configured"})
		return false
	}

	if c.GetHeader("Authorization") != "Bearer "+token {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid MCP bearer token"})
		return false
	}

	return true
}

func (h *Handler) mcpToken() string {
	if h.config == nil {
		return ""
	}
	if h.config.MCP.Token != "" {
		return h.config.MCP.Token
	}
	return h.config.MCP.BearerToken
}

func (h *Handler) isMCPOriginAllowed(origin string) bool {
	if origin == "" || h.config == nil || h.config.MCP.AllowedOrigins == "" {
		return true
	}

	for _, allowed := range strings.Split(h.config.MCP.AllowedOrigins, ",") {
		allowed = strings.TrimSpace(allowed)
		if allowed == "*" || allowed == origin {
			return true
		}
	}

	return false
}

func (h *Handler) dispatchMCP(req mcpRequest) mcpResponse {
	if req.JSONRPC != "2.0" {
		return mcpErr(req.ID, -32600, "Invalid Request")
	}

	var result any
	var err error

	switch req.Method {
	case "initialize":
		result = h.mcpInitialize()
	case "ping":
		result = map[string]any{}
	case "resources/list":
		result = h.mcpResourcesList()
	case "resources/templates/list":
		result = h.mcpResourceTemplatesList()
	case "resources/read":
		result, err = h.mcpResourcesRead(req.Params)
	case "completion/complete":
		result, err = h.mcpCompletionComplete(req.Params)
	case "prompts/list":
		result = h.mcpPromptsList()
	case "prompts/get":
		result, err = h.mcpPromptsGet(req.Params)
	case "tools/list":
		result = h.mcpToolsList()
	case "tools/call":
		result, err = h.mcpToolsCall(req.Params)
	case "notifications/initialized":
		result = nil
	default:
		return mcpErr(req.ID, -32601, "Method not found")
	}

	if err != nil {
		return mcpErr(req.ID, -32602, err.Error())
	}

	return mcpResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
}

func mcpErr(id any, code int, message string) mcpResponse {
	return mcpResponse{JSONRPC: "2.0", ID: id, Error: &mcpError{Code: code, Message: message}}
}

func (h *Handler) mcpInitialize() map[string]any {
	return map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities": map[string]any{
			"resources":   map[string]any{},
			"prompts":     map[string]any{},
			"tools":       map[string]any{},
			"completions": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "accweb-mcp",
			"version": "0.1.0",
		},
	}
}

func (h *Handler) mcpResourcesList() map[string]any {
	resources := []mcpResource{
		mcpJSONResource("accweb://parameters", "parameters", "ACC server parameter reference", "All ACCWeb-managed ACC Dedicated Server parameters and descriptions.", 0.8),
		mcpJSONResource("accweb://tracks", "tracks", "ACC track catalog", "All supported ACC track ids, display names, countries and aliases. Use this for questions about maps/tracks.", 1.0),
		mcpJSONResource("accweb://instances", "instances", "ACCWeb instances", "Configured ACCWeb instances with ids, names, tracks and runtime state.", 1.0),
	}

	for _, srv := range h.sm.GetServers() {
		resources = append(resources, mcpResource{
			URI:         "accweb://instances/" + srv.GetID() + "/config",
			Name:        "instance_config_" + srv.GetID(),
			Title:       "Instance " + srv.GetID() + " configuration",
			Description: "Redacted ACCWeb and ACC JSON configuration for this instance. Prefer status, track and weather tools for normal questions.",
			MimeType:    "application/json",
			Annotations: mcpResourceAnnotations(0.35),
		})
		resources = append(resources, mcpJSONResource("accweb://instances/"+srv.GetID()+"/status", "instance_status_"+srv.GetID(), "Instance "+srv.GetID()+" status", "Runtime status for this ACC instance.", 0.9))
		resources = append(resources, mcpJSONResource("accweb://instances/"+srv.GetID()+"/weather", "instance_weather_"+srv.GetID(), "Instance "+srv.GetID()+" weather", "Weather configuration and source paths for this ACC instance.", 0.95))
	}

	return map[string]any{"resources": resources}
}

func (h *Handler) mcpResourceTemplatesList() map[string]any {
	return map[string]any{
		"resourceTemplates": []mcpResourceTemplate{
			mcpJSONResourceTemplate("accweb://instances", "instances", "ACCWeb instances", "Configured ACCWeb instances and runtime state.", 1.0),
			mcpJSONResourceTemplate("accweb://instances/{instanceId}/status", "instance_status", "ACC instance status", "Runtime status for one ACC instance.", 0.9),
			mcpJSONResourceTemplate("accweb://instances/{instanceId}/weather", "instance_weather", "ACC instance weather", "Weather configuration for one ACC instance.", 0.95),
			mcpJSONResourceTemplate("accweb://instances/{instanceId}/config", "instance_config", "ACC instance configuration", "Redacted full ACCWeb and ACC JSON configuration for fallback/debug use.", 0.35),
			mcpJSONResourceTemplate("accweb://tracks/{trackId}", "track", "ACC track", "One ACC track from the global track catalog. The trackId argument supports completion.", 1.0),
		},
	}
}

func (h *Handler) mcpResourcesRead(raw json.RawMessage) (map[string]any, error) {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}

	var payload any
	switch {
	case params.URI == "accweb://parameters":
		payload = accParameterDocs()
	case params.URI == "accweb://tracks":
		payload = accTracksPayload()
	case strings.HasPrefix(params.URI, "accweb://tracks/"):
		id := strings.TrimPrefix(params.URI, "accweb://tracks/")
		track, ok := findACCTrack(id)
		if !ok {
			return nil, fmt.Errorf("unknown track id: %s", id)
		}
		payload = map[string]any{"track": track}
	case params.URI == "accweb://instances":
		payload = map[string]any{"instances": h.mcpInstanceSummaries()}
	case strings.HasPrefix(params.URI, "accweb://instances/") && strings.HasSuffix(params.URI, "/config"):
		id := strings.TrimSuffix(strings.TrimPrefix(params.URI, "accweb://instances/"), "/config")
		srv, err := h.sm.GetServerByID(id)
		if err != nil {
			return nil, err
		}
		payload = h.mcpInstanceConfigPayload(srv)
	case strings.HasPrefix(params.URI, "accweb://instances/") && strings.HasSuffix(params.URI, "/status"):
		id := strings.TrimSuffix(strings.TrimPrefix(params.URI, "accweb://instances/"), "/status")
		srv, err := h.sm.GetServerByID(id)
		if err != nil {
			return nil, err
		}
		payload = h.mcpInstanceStatusPayload(srv)
	case strings.HasPrefix(params.URI, "accweb://instances/") && strings.HasSuffix(params.URI, "/weather"):
		id := strings.TrimSuffix(strings.TrimPrefix(params.URI, "accweb://instances/"), "/weather")
		srv, err := h.sm.GetServerByID(id)
		if err != nil {
			return nil, err
		}
		payload = h.mcpInstanceWeatherPayload(srv)
	default:
		return nil, fmt.Errorf("unknown resource URI: %s", params.URI)
	}

	text, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"contents": []map[string]any{
			{
				"uri":         params.URI,
				"mimeType":    "application/json",
				"text":        string(text),
				"annotations": mcpResourceAnnotations(0.8),
			},
		},
	}, nil
}

func (h *Handler) mcpCompletionComplete(raw json.RawMessage) (map[string]any, error) {
	var params struct {
		Ref struct {
			Type string `json:"type"`
			Name string `json:"name"`
			URI  string `json:"uri"`
		} `json:"ref"`
		Argument struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"argument"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}

	values := []string{}
	if isTrackCompletionRef(params.Ref.Type, params.Ref.Name, params.Ref.URI, params.Argument.Name) {
		values = completeACCTrackIDs(params.Argument.Value)
	}

	return map[string]any{
		"completion": map[string]any{
			"values":  values,
			"total":   len(values),
			"hasMore": false,
		},
	}, nil
}

func isTrackCompletionRef(refType string, refName string, refURI string, argumentName string) bool {
	argumentName = strings.TrimSpace(argumentName)
	switch refType {
	case "ref/prompt":
		return refName == "configure_quick_race" && argumentName == "track"
	case "ref/resource":
		return refURI == "accweb://tracks/{trackId}" && (argumentName == "trackId" || argumentName == "track")
	default:
		return false
	}
}

func (h *Handler) mcpInstanceSummaries() []ListServerItem {
	out := []ListServerItem{}
	for _, srv := range h.sortedMCPServers() {
		out = append(out, buildListServerItem(srv))
	}
	return out
}

func (h *Handler) mcpPromptsList() map[string]any {
	return map[string]any{
		"prompts": []mcpPrompt{
			{
				Name:        "acc_server_overview",
				Title:       "ACC server overview",
				Description: "Answer general questions about configured ACC instances using list_instances and domain read-only tools first.",
				Arguments: []mcpPromptArgument{
					{Name: "instanceIdOrName", Title: "Instance", Description: "Optional ACC instance id or exact/partial server name."},
				},
			},
			{
				Name:        "acc_weather_answer",
				Title:       "ACC weather answer",
				Description: "Answer weather questions by calling get_instance_weather instead of reading raw configuration.",
				Arguments: []mcpPromptArgument{
					{Name: "instanceIdOrName", Title: "Instance", Description: "Optional ACC instance id or exact/partial server name."},
				},
			},
			{
				Name:        "acc_race_setup_summary",
				Title:       "ACC race setup summary",
				Description: "Summarize track, sessions, weather, slots and car group for an ACC instance.",
				Arguments: []mcpPromptArgument{
					{Name: "instanceIdOrName", Title: "Instance", Description: "Optional ACC instance id or exact/partial server name."},
				},
			},
			{
				Name:        "acc_config_explain",
				Title:       "ACC config explain",
				Description: "Explain one ACC Dedicated Server parameter and prefer domain tools for common status, track and weather questions.",
				Arguments: []mcpPromptArgument{
					{Name: "path", Title: "Path", Description: "Parameter path, e.g. acc.settings.maxCarSlots", Required: true},
				},
			},
			{
				Name:        "configure_quick_race",
				Title:       "Configure quick race",
				Description: "Prepare an ACC quick race configuration with track, car group, slots and session durations.",
				Arguments: []mcpPromptArgument{
					{Name: "track", Title: "Track", Description: "ACC track id from accweb://tracks, e.g. monza", Required: true},
					{Name: "carGroup", Description: "Car group, e.g. GT3", Required: true},
					{Name: "qualifyingMinutes", Description: "Qualifying duration in minutes", Required: true},
					{Name: "raceMinutes", Description: "Race duration in minutes", Required: true},
				},
			},
			{
				Name:        "explain_parameter",
				Title:       "Explain parameter",
				Description: "Explain one ACC Dedicated Server parameter and where it is stored.",
				Arguments: []mcpPromptArgument{
					{Name: "path", Description: "Parameter path, e.g. acc.settings.maxCarSlots", Required: true},
				},
			},
		},
	}
}

func (h *Handler) mcpPromptsGet(raw json.RawMessage) (map[string]any, error) {
	var params struct {
		Name      string            `json:"name"`
		Arguments map[string]string `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}

	switch params.Name {
	case "acc_server_overview":
		return mcpPromptMessages("Use list_instances first. If the user asks about one server and only one running/configured ACC instance exists, use that instance. For track questions call get_instance_track; for weather questions call get_instance_weather; for runtime questions call get_instance_status. Use get_instance_config only as fallback/debug context."), nil
	case "acc_weather_answer":
		return mcpPromptMessages("For ACC weather questions, call get_instance_weather with instanceIdOrName when available. Do not ask which raw JSON path stores weather. The tool returns ambientTempC, cloudLevel, rain, weatherRandomness, a summary, and sourcePaths."), nil
	case "acc_race_setup_summary":
		return mcpPromptMessages("Summarize the ACC race setup by combining get_instance_track, get_instance_weather and get_instance_status. Mention track, current state, session, connected clients, slots and weather. Use get_instance_config only if a specific field is missing."), nil
	case "acc_config_explain":
		return mcpPromptMessages("Explain this ACCWeb parameter path, including JSON file, valid values, and operational impact. If the path is about weather, track or status, mention the specialized read-only tool that exposes it directly: " + params.Arguments["path"]), nil
	case "configure_quick_race":
		return mcpPromptMessages(fmt.Sprintf(
			"Configure an ACC Dedicated Server quick race. Use the parameter reference resource first, then use tools to create or update an instance. Desired values: track=%s, carGroup=%s, qualifyingMinutes=%s, raceMinutes=%s.",
			params.Arguments["track"], params.Arguments["carGroup"], params.Arguments["qualifyingMinutes"], params.Arguments["raceMinutes"],
		)), nil
	case "explain_parameter":
		return mcpPromptMessages("Explain this ACCWeb parameter path, including JSON file, valid values, and operational impact: " + params.Arguments["path"]), nil
	default:
		return nil, fmt.Errorf("unknown prompt: %s", params.Name)
	}
}

func mcpPromptMessages(text string) map[string]any {
	return map[string]any{
		"messages": []map[string]any{
			{
				"role": "user",
				"content": map[string]any{
					"type": "text",
					"text": text,
				},
			},
		},
	}
}

func (h *Handler) mcpToolsList() map[string]any {
	return map[string]any{
		"tools": []mcpTool{
			{
				Name:         "list_tracks",
				Title:        "List ACC tracks",
				Description:  "List the global ACC track catalog with valid track ids, names, countries and aliases. Use this for questions about maps/tracks; it is not a car list.",
				InputSchema:  schemaObject(map[string]any{}),
				OutputSchema: mcpTracksOutputSchema(),
				Annotations:  mcpReadOnlyToolAnnotations(),
			},
			{
				Name:         "list_instances",
				Title:        "List ACC instances",
				Description:  "List ACCWeb server instances with ids, names, tracks and runtime state. Use this before asking the user to identify a server.",
				InputSchema:  schemaObject(map[string]any{}),
				OutputSchema: mcpListInstancesOutputSchema(),
				Annotations:  mcpReadOnlyToolAnnotations(),
			},
			{
				Name:         "get_instance_status",
				Title:        "Get ACC instance status",
				Description:  "Get runtime status for an ACC instance. Accepts id, exact/partial server name, or omitted selector when only one running/configured instance exists.",
				InputSchema:  mcpInstanceSelectorInputSchema(),
				OutputSchema: mcpInstanceStatusOutputSchema(),
				Annotations:  mcpReadOnlyToolAnnotations(),
			},
			{
				Name:         "get_instance_weather",
				Title:        "Get ACC instance weather",
				Description:  "Get weather settings for an ACC instance with semantic fields and sourcePaths. Use this for questions about weather instead of raw config.",
				InputSchema:  mcpInstanceSelectorInputSchema(),
				OutputSchema: mcpInstanceWeatherOutputSchema(),
				Annotations:  mcpReadOnlyToolAnnotations(),
			},
			{
				Name:         "get_instance_track",
				Title:        "Get ACC instance track",
				Description:  "Get configured/live/effective track for an ACC instance. Use this for map/track questions instead of raw config.",
				InputSchema:  mcpInstanceSelectorInputSchema(),
				OutputSchema: mcpInstanceTrackOutputSchema(),
				Annotations:  mcpReadOnlyToolAnnotations(),
			},
			{
				Name:         "get_instance_config",
				Title:        "Get redacted ACC config",
				Description:  "Fallback/debug tool that returns a redacted full ACCWeb and ACC configuration. Prefer status, track and weather tools for normal questions.",
				InputSchema:  mcpInstanceSelectorInputSchema(),
				OutputSchema: mcpInstanceConfigOutputSchema(),
				Annotations:  mcpReadOnlyToolAnnotations(),
			},
			{
				Name:        "set_instance_parameters",
				Title:       "Set ACC parameters",
				Description: "Update one or more ACC configuration parameters using paths like acc.settings.maxCarSlots or acc.event.sessions[0].sessionDurationMinutes. The instance must be stopped unless restartIfLive is true; with restartIfLive, MCP stops the process, waits until it is fully stopped, saves the config, then starts it again.",
				InputSchema: schemaObject(map[string]any{
					"instanceId":    schemaString("ACCWeb instance id"),
					"restartIfLive": schemaBoolean("When true and the instance is running, stop it, wait until stopped, save changes, and start it again"),
					"updates": map[string]any{
						"type":        "array",
						"description": "Parameter updates",
						"items": schemaObject(map[string]any{
							"path":  schemaString("Parameter path"),
							"value": map[string]any{"description": "New JSON value"},
						}, "path", "value"),
					},
				}, "instanceId", "updates"),
				OutputSchema: mcpSetParametersOutputSchema(),
				Annotations:  mcpWriteToolAnnotations(true, false),
			},
			{
				Name:         "start_instance",
				Title:        "Start ACC instance",
				Description:  "Start an ACC server instance.",
				InputSchema:  mcpInstanceSelectorInputSchema(),
				OutputSchema: mcpActionOutputSchema(),
				Annotations:  mcpWriteToolAnnotations(false, false),
			},
			{
				Name:         "stop_instance",
				Title:        "Stop ACC instance",
				Description:  "Stop an ACC server instance.",
				InputSchema:  mcpInstanceSelectorInputSchema(),
				OutputSchema: mcpActionOutputSchema(),
				Annotations:  mcpWriteToolAnnotations(false, true),
			},
			{
				Name:        "create_quick_race_instance",
				Title:       "Create ACC quick race",
				Description: "Create a new simple Q/R ACC instance with common settings.",
				InputSchema: schemaObject(map[string]any{
					"serverName":        schemaString("Server name"),
					"track":             schemaString("Track id, e.g. monza"),
					"carGroup":          schemaString("Car group, e.g. GT3"),
					"maxCarSlots":       schemaInteger("Maximum car slots"),
					"qualifyingMinutes": schemaInteger("Qualifying duration"),
					"raceMinutes":       schemaInteger("Race duration"),
					"hourOfDay":         schemaInteger("Session start hour, 0-23"),
					"registerToLobby":   schemaInteger("0 or 1"),
					"lanDiscovery":      schemaInteger("0 or 1"),
					"tcpPort":           schemaInteger("TCP port"),
					"udpPort":           schemaInteger("UDP port"),
				}, "serverName", "track", "carGroup", "maxCarSlots", "qualifyingMinutes", "raceMinutes"),
				OutputSchema: mcpCreateQuickRaceOutputSchema(),
				Annotations:  mcpWriteToolAnnotations(false, false),
			},
		},
	}
}

func (h *Handler) mcpToolsCall(raw json.RawMessage) (map[string]any, error) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}

	switch params.Name {
	case "list_tracks":
		return mcpToolStructured(accTracksPayload())
	case "list_instances":
		return mcpToolStructured(map[string]any{"instances": h.mcpInstanceSummaries()})
	case "get_instance_status":
		var args mcpInstanceSelectorArgs
		if err := decodeMCPArguments(params.Arguments, &args); err != nil {
			return nil, err
		}
		srv, toolErr := h.mcpResolveInstance(args.Selector())
		if toolErr != nil {
			return toolErr, nil
		}
		return mcpToolStructured(h.mcpInstanceStatusPayload(srv))
	case "get_instance_weather":
		var args mcpInstanceSelectorArgs
		if err := decodeMCPArguments(params.Arguments, &args); err != nil {
			return nil, err
		}
		srv, toolErr := h.mcpResolveInstance(args.Selector())
		if toolErr != nil {
			return toolErr, nil
		}
		return mcpToolStructured(h.mcpInstanceWeatherPayload(srv))
	case "get_instance_track":
		var args mcpInstanceSelectorArgs
		if err := decodeMCPArguments(params.Arguments, &args); err != nil {
			return nil, err
		}
		srv, toolErr := h.mcpResolveInstance(args.Selector())
		if toolErr != nil {
			return toolErr, nil
		}
		return mcpToolStructured(h.mcpInstanceTrackPayload(srv))
	case "get_instance_config":
		var args mcpInstanceSelectorArgs
		if err := decodeMCPArguments(params.Arguments, &args); err != nil {
			return nil, err
		}
		srv, toolErr := h.mcpResolveInstance(args.Selector())
		if toolErr != nil {
			return toolErr, nil
		}
		return mcpToolStructured(h.mcpInstanceConfigPayload(srv))
	case "set_instance_parameters":
		var args mcpSetParametersArgs
		if err := decodeMCPArguments(params.Arguments, &args); err != nil {
			return nil, err
		}
		result, err := h.mcpSetInstanceParameters(args)
		if err != nil {
			return mcpToolErrorStructured("set_parameters_failed", err.Error(), "Check the parameter path/value types and whether restartIfLive is needed.", h.mcpInstanceSummaries()), nil
		}
		return result, nil
	case "start_instance":
		var args mcpInstanceSelectorArgs
		if err := decodeMCPArguments(params.Arguments, &args); err != nil {
			return nil, err
		}
		srv, toolErr := h.mcpResolveInstance(args.Selector())
		if toolErr != nil {
			return toolErr, nil
		}
		if err := h.sm.Start(srv.GetID()); err != nil {
			return mcpToolErrorStructured("start_failed", err.Error(), "Check whether the instance is already running and whether ACC server files are available.", h.mcpInstanceSummaries()), nil
		}
		return mcpToolStructured(map[string]any{"instance": mcpInstanceRef(srv), "action": "started", "success": true})
	case "stop_instance":
		var args mcpInstanceSelectorArgs
		if err := decodeMCPArguments(params.Arguments, &args); err != nil {
			return nil, err
		}
		srv, toolErr := h.mcpResolveInstance(args.Selector())
		if toolErr != nil {
			return toolErr, nil
		}
		if err := srv.Stop(); err != nil {
			return mcpToolErrorStructured("stop_failed", err.Error(), "Check the instance status with get_instance_status and try again.", h.mcpInstanceSummaries()), nil
		}
		return mcpToolStructured(map[string]any{"instance": mcpInstanceRef(srv), "action": "stopped", "success": true})
	case "create_quick_race_instance":
		var args mcpCreateQuickRaceArgs
		if err := decodeMCPArguments(params.Arguments, &args); err != nil {
			return nil, err
		}
		result, err := h.mcpCreateQuickRaceInstance(args)
		if err != nil {
			return mcpToolErrorStructured("create_instance_failed", err.Error(), "Check ACC server files, ports and required quick race arguments.", h.mcpInstanceSummaries()), nil
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unknown tool: %s", params.Name)
	}
}

func (h *Handler) mcpSetInstanceParameters(args mcpSetParametersArgs) (map[string]any, error) {
	srv, err := h.sm.GetServerByID(args.InstanceID)
	if err != nil {
		return mcpToolErrorStructured("instance_not_found", "No ACC instance matched '"+args.InstanceID+"'", "Call list_instances or use one of availableInstances.", h.mcpInstanceSummaries()), nil
	}

	wasRunning := srv.IsRunning()
	if wasRunning {
		if !args.RestartIfLive {
			return mcpToolErrorStructured("instance_running", "Instance is running; set restartIfLive=true to stop, save and restart.", "Call set_instance_parameters again with restartIfLive=true, or stop the instance first.", h.mcpInstanceSummaries()), nil
		}
		if err := srv.Stop(); err != nil {
			return mcpToolErrorStructured("stop_failed", err.Error(), "Call get_instance_status, then stop_instance manually if it is still running.", h.mcpInstanceSummaries()), nil
		}
		if err := waitForMCPInstanceStopped(srv, mcpRestartStopTimeout, mcpRestartStopPollInterval); err != nil {
			return mcpToolErrorStructured("stop_timeout", err.Error(), "Call get_instance_status, then stop_instance manually and retry set_instance_parameters.", h.mcpInstanceSummaries()), nil
		}
	}

	for _, update := range args.Updates {
		if err := setPathValue(&srv.AccCfg, update.Path, update.Value); err != nil {
			return nil, err
		}
	}

	instance.SetConfigVersion(&srv.AccCfg)
	srv.Cfg.SetUpdateAt()
	if err := srv.Save(); err != nil {
		return nil, err
	}

	if wasRunning {
		if err := h.sm.Start(args.InstanceID); err != nil {
			return mcpToolErrorStructured("restart_failed", err.Error(), "Configuration was saved, but restart failed. Check server files and ports, then call start_instance.", h.mcpInstanceSummaries()), nil
		}
	}

	return mcpToolStructured(map[string]any{"updated": redactParameterPatches(args.Updates), "restarted": wasRunning, "instanceId": args.InstanceID})
}

func waitForMCPInstanceStopped(srv interface{ IsRunning() bool }, timeout, pollInterval time.Duration) error {
	if waitForMCPCondition(timeout, pollInterval, func() bool { return !srv.IsRunning() }) {
		return nil
	}
	return fmt.Errorf("instance did not stop within %s", timeout)
}

func waitForMCPCondition(timeout, pollInterval time.Duration, done func() bool) bool {
	if done() {
		return true
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(pollInterval)
		if done() {
			return true
		}
	}
	return done()
}

func (h *Handler) mcpCreateQuickRaceInstance(args mcpCreateQuickRaceArgs) (map[string]any, error) {
	if args.QualifyingMins == 0 {
		args.QualifyingMins = 5
	}
	if args.RaceMins == 0 {
		args.RaceMins = 20
	}
	if args.HourOfDay == 0 {
		args.HourOfDay = 6
	}
	if args.TCPPort == 0 {
		args.TCPPort = 9232
	}
	if args.UDPPort == 0 {
		args.UDPPort = 9231
	}
	if args.MaxCarSlots == 0 {
		args.MaxCarSlots = 10
	}
	if args.ServerName == "" {
		args.ServerName = "ACCWeb MCP Server"
	}

	accCfg := instance.AccConfigFiles{
		Configuration: instance.ConfigurationJson{UdpPort: args.UDPPort, TcpPort: args.TCPPort, MaxConnections: args.MaxCarSlots, RegisterToLobby: args.RegisterToLobby, LanDiscovery: args.LanDiscovery},
		Settings: instance.SettingsJson{
			ServerName:                 args.ServerName,
			TrackMedalsRequirement:     0,
			SafetyRatingRequirement:    -1,
			RacecraftRatingRequirement: -1,
			IgnorePrematureDisconnects: 0,
			MaxCarSlots:                args.MaxCarSlots,
			AllowAutoDQ:                1,
			FormationLapType:           3,
			CarGroup:                   args.CarGroup,
		},
		Event: instance.EventJson{
			Track:                         args.Track,
			PreRaceWaitingTimeSeconds:     30,
			SessionOverTimeSeconds:        120,
			AmbientTemp:                   26,
			CloudLevel:                    0.1,
			Rain:                          0,
			WeatherRandomness:             0,
			PostQualySeconds:              15,
			PostRaceSeconds:               30,
			IsFixedConditionQualification: 1,
			Sessions: []instance.SessionSettings{
				{HourOfDay: args.HourOfDay, DayOfWeekend: 2, TimeMultiplier: 1, SessionType: "Q", SessionDurationMinutes: args.QualifyingMins},
				{HourOfDay: args.HourOfDay, DayOfWeekend: 3, TimeMultiplier: 1, SessionType: "R", SessionDurationMinutes: args.RaceMins},
			},
		},
		EventRules: instance.EventRulesJson{
			QualifyStandingType:       1,
			PitWindowLengthSec:        -1,
			DriverStintTimeSec:        -1,
			MandatoryPitstopCount:     0,
			MaxTotalDrivingTime:       -1,
			MaxDriversCount:           1,
			IsRefuellingAllowedInRace: true,
			TyreSetCount:              50,
		},
		Entrylist:   instance.EntrylistJson{Entries: []instance.EntrySettings{}},
		Bop:         instance.BopJson{Entries: []instance.BopSettings{}},
		AssistRules: instance.AssistRulesJson{StabilityControlLevelMax: 100},
	}
	instance.SetConfigVersion(&accCfg)

	srv, err := h.sm.Create(&accCfg, instance.AccWebSettingsJson{})
	if err != nil {
		return nil, err
	}

	return mcpToolStructured(map[string]any{"instance": mcpInstanceRef(srv), "config": redactSecrets(srv.AccCfg)})
}

func (args mcpInstanceSelectorArgs) Selector() string {
	if strings.TrimSpace(args.InstanceIDOrName) != "" {
		return args.InstanceIDOrName
	}
	return args.InstanceID
}

func decodeMCPArguments(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	return json.Unmarshal(raw, dst)
}

func (h *Handler) mcpResolveInstance(selector string) (*instance.Instance, map[string]any) {
	selector = strings.TrimSpace(selector)
	servers := h.sortedMCPServers()
	if len(servers) == 0 {
		return nil, mcpToolErrorStructured("instance_not_found", "No ACC instances are configured.", "Create an instance first, or check ACCWeb configuration.", []ListServerItem{})
	}

	if selector == "" {
		if srv := defaultMCPServer(servers); srv != nil {
			return srv, nil
		}
		return nil, mcpToolErrorStructured("instance_ambiguous", "More than one ACC instance is available.", "Call list_instances and pass instanceIdOrName with an id or exact/partial server name.", h.mcpInstanceSummaries())
	}

	normalized := normalizeMCPSelector(selector)
	matches := make([]*instance.Instance, 0, len(servers))
	for _, srv := range servers {
		if normalizeMCPSelector(srv.GetID()) == normalized || normalizeMCPSelector(srv.AccCfg.Settings.ServerName) == normalized {
			matches = append(matches, srv)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return nil, mcpToolErrorStructured("instance_ambiguous", "More than one ACC instance matched '"+selector+"'.", "Use one of availableInstances with an exact id.", mcpListServerItems(matches))
	}

	for _, srv := range servers {
		if strings.Contains(normalizeMCPSelector(srv.GetID()), normalized) || strings.Contains(normalizeMCPSelector(srv.AccCfg.Settings.ServerName), normalized) {
			matches = append(matches, srv)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return nil, mcpToolErrorStructured("instance_ambiguous", "More than one ACC instance matched '"+selector+"'.", "Use one of availableInstances with an exact id.", mcpListServerItems(matches))
	}

	if srv := defaultMCPServer(servers); srv != nil {
		return srv, nil
	}

	return nil, mcpToolErrorStructured("instance_not_found", "No ACC instance matched '"+selector+"'.", "Call list_instances or use one of availableInstances.", h.mcpInstanceSummaries())
}

func (h *Handler) sortedMCPServers() []*instance.Instance {
	servers := make([]*instance.Instance, 0, len(h.sm.GetServers()))
	for _, srv := range h.sm.GetServers() {
		servers = append(servers, srv)
	}
	sort.Slice(servers, func(i, j int) bool {
		return servers[i].GetID() < servers[j].GetID()
	})
	return servers
}

func defaultMCPServer(servers []*instance.Instance) *instance.Instance {
	running := make([]*instance.Instance, 0, len(servers))
	for _, srv := range servers {
		if srv.IsRunning() {
			running = append(running, srv)
		}
	}
	if len(running) == 1 {
		return running[0]
	}
	if len(servers) == 1 {
		return servers[0]
	}
	return nil
}

func normalizeMCPSelector(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func mcpListServerItems(servers []*instance.Instance) []ListServerItem {
	out := make([]ListServerItem, 0, len(servers))
	for _, srv := range servers {
		out = append(out, buildListServerItem(srv))
	}
	return out
}

func mcpInstanceRef(srv *instance.Instance) map[string]any {
	return map[string]any{
		"id":   srv.GetID(),
		"name": srv.AccCfg.Settings.ServerName,
	}
}

func (h *Handler) mcpInstanceStatusPayload(srv *instance.Instance) map[string]any {
	return map[string]any{
		"instance": mcpInstanceRef(srv),
		"status": map[string]any{
			"isRunning":        srv.IsRunning(),
			"pid":              srv.GetProcessID(),
			"serverState":      srv.Live.ServerState,
			"nrClients":        srv.Live.NrClients,
			"sessionType":      srv.Live.SessionType,
			"sessionPhase":     srv.Live.SessionPhase,
			"sessionRemaining": srv.Live.SessionRemaining,
			"tcpPort":          srv.AccCfg.Configuration.TcpPort,
			"udpPort":          srv.AccCfg.Configuration.UdpPort,
			"maxCarSlots":      srv.AccCfg.Settings.MaxCarSlots,
			"track":            effectiveTrack(srv),
		},
		"sourcePaths": []string{
			"accweb.live.serverState",
			"accweb.live.nrClients",
			"accweb.live.sessionType",
			"accweb.live.sessionPhase",
			"accweb.live.sessionRemaining",
			"acc.configuration.tcpPort",
			"acc.configuration.udpPort",
			"acc.settings.maxCarSlots",
		},
	}
}

func (h *Handler) mcpInstanceWeatherPayload(srv *instance.Instance) map[string]any {
	event := srv.AccCfg.Event
	weather := map[string]any{
		"ambientTempC":                  event.AmbientTemp,
		"trackTempC":                    event.TrackTemp,
		"cloudLevel":                    event.CloudLevel,
		"rain":                          event.Rain,
		"weatherRandomness":             event.WeatherRandomness,
		"simracerWeatherConditions":     event.SimracerWeatherConditions,
		"isFixedConditionQualification": event.IsFixedConditionQualification == 1,
		"summary":                       weatherSummary(event),
	}
	return map[string]any{
		"instance": mcpInstanceRef(srv),
		"weather":  weather,
		"sourcePaths": []string{
			"acc.event.ambientTemp",
			"acc.event.trackTemp",
			"acc.event.cloudLevel",
			"acc.event.rain",
			"acc.event.weatherRandomness",
			"acc.event.simracerWeatherConditions",
			"acc.event.isFixedConditionQualification",
		},
	}
}

func (h *Handler) mcpInstanceTrackPayload(srv *instance.Instance) map[string]any {
	configured := srv.AccCfg.Event.Track
	live := srv.Live.Track
	effective := effectiveTrack(srv)
	return map[string]any{
		"instance": mcpInstanceRef(srv),
		"track": map[string]any{
			"configuredTrack": configured,
			"liveTrack":       live,
			"effectiveTrack":  effective,
			"summary":         "Track is " + effective,
		},
		"sourcePaths": []string{
			"acc.event.track",
			"accweb.live.track",
		},
	}
}

func (h *Handler) mcpInstanceConfigPayload(srv *instance.Instance) map[string]any {
	return map[string]any{
		"instance":  mcpInstanceRef(srv),
		"isRunning": srv.IsRunning(),
		"pid":       srv.GetProcessID(),
		"accWeb":    redactSecrets(srv.Cfg.Settings),
		"acc":       redactSecrets(srv.AccCfg),
		"live":      redactSecrets(srv.Live),
		"redacted": []string{
			"password",
			"adminPassword",
			"spectatorPassword",
			"token",
			"secret",
			"credential",
			"path",
		},
	}
}

func effectiveTrack(srv *instance.Instance) string {
	if srv.Live.Track != "" {
		return srv.Live.Track
	}
	return srv.AccCfg.Event.Track
}

func weatherSummary(event instance.EventJson) string {
	condition := "dry"
	if event.Rain > 0 {
		condition = "rain"
	}
	return fmt.Sprintf("%s, %dC, %.0f%% cloud cover, %.0f%% rain", condition, event.AmbientTemp, event.CloudLevel*100, event.Rain*100)
}

func mcpToolText(text string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}
}

func mcpToolStructured(v map[string]any) (map[string]any, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	result := mcpToolText(string(data))
	result["structuredContent"] = v
	return result, nil
}

func mcpToolErrorStructured(code string, message string, recoveryHint string, availableInstances []ListServerItem) map[string]any {
	payload := map[string]any{
		"code":         code,
		"message":      message,
		"recoveryHint": recoveryHint,
	}
	if availableInstances != nil {
		payload["availableInstances"] = availableInstances
	}
	result, err := mcpToolStructured(payload)
	if err != nil {
		result = mcpToolText(message)
	}
	result["isError"] = true
	return result
}

func mcpReadOnlyToolAnnotations() map[string]any {
	return map[string]any{
		"readOnlyHint":    true,
		"destructiveHint": false,
		"idempotentHint":  true,
		"openWorldHint":   false,
	}
}

func mcpWriteToolAnnotations(destructive bool, idempotent bool) map[string]any {
	return map[string]any{
		"readOnlyHint":    false,
		"destructiveHint": destructive,
		"idempotentHint":  idempotent,
		"openWorldHint":   false,
	}
}

func mcpResourceAnnotations(priority float64) map[string]any {
	return map[string]any{
		"audience": []string{"assistant"},
		"priority": priority,
	}
}

func mcpJSONResource(uri string, name string, title string, description string, priority float64) mcpResource {
	return mcpResource{
		URI:         uri,
		Name:        name,
		Title:       title,
		Description: description,
		MimeType:    "application/json",
		Annotations: mcpResourceAnnotations(priority),
	}
}

func mcpJSONResourceTemplate(uriTemplate string, name string, title string, description string, priority float64) mcpResourceTemplate {
	return mcpResourceTemplate{
		URITemplate: uriTemplate,
		Name:        name,
		Title:       title,
		Description: description,
		MimeType:    "application/json",
		Annotations: mcpResourceAnnotations(priority),
	}
}

func redactParameterPatches(updates []mcpParameterPatch) []map[string]any {
	out := make([]map[string]any, 0, len(updates))
	for _, update := range updates {
		value := redactSecrets(update.Value)
		if isSensitiveKey(update.Path) {
			value = "[redacted]"
		}
		out = append(out, map[string]any{"path": update.Path, "value": value})
	}
	return out
}

func redactSecrets(v any) any {
	data, err := json.Marshal(v)
	if err != nil {
		return v
	}

	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return v
	}
	return redactSecretsValue(decoded)
}

func redactSecretsValue(v any) any {
	switch value := v.(type) {
	case map[string]any:
		for key, child := range value {
			if isSensitiveKey(key) {
				value[key] = "[redacted]"
				continue
			}
			value[key] = redactSecretsValue(child)
		}
		return value
	case []any:
		for i, child := range value {
			value[i] = redactSecretsValue(child)
		}
		return value
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(key)
	return strings.Contains(key, "password") ||
		strings.Contains(key, "token") ||
		strings.Contains(key, "secret") ||
		strings.Contains(key, "credential") ||
		key == "path" ||
		strings.HasSuffix(key, "path")
}

func mcpInstanceSelectorInputSchema() map[string]any {
	return schemaObject(map[string]any{
		"instanceIdOrName": schemaString("ACC instance id, exact/partial server name, or previously mentioned instance. If omitted and only one running/configured instance exists, that instance is used."),
		"instanceId":       schemaString("Backward-compatible ACCWeb instance id. Prefer instanceIdOrName for new clients."),
	})
}

func mcpTracksOutputSchema() map[string]any {
	return schemaObject(map[string]any{
		"tracks": schemaArray("Supported ACC track catalog. Values are track ids, not car groups.", mcpTrackSchema()),
	})
}

func mcpTrackSchema() map[string]any {
	return schemaObject(map[string]any{
		"id":      schemaString("ACC event.json track id, e.g. monza"),
		"name":    schemaString("Human-readable track name"),
		"country": schemaString("Track country"),
		"aliases": schemaArray("Common English/Russian aliases for matching user text", schemaString("Track alias")),
	})
}

func mcpListInstancesOutputSchema() map[string]any {
	return schemaObject(map[string]any{
		"instances": schemaArray("Configured ACC instances", mcpInstanceSummarySchema()),
		"code":      schemaString("Error code when isError is true"),
		"message":   schemaString("Error message when isError is true"),
	})
}

func mcpInstanceStatusOutputSchema() map[string]any {
	return schemaObject(map[string]any{
		"instance":           mcpInstanceRefSchema(),
		"status":             schemaLooseObject("Runtime status, ports, clients, session and track"),
		"sourcePaths":        schemaArray("Source configuration/live paths", schemaString("ACCWeb or ACC path")),
		"code":               schemaString("Error code when isError is true"),
		"message":            schemaString("Error message when isError is true"),
		"recoveryHint":       schemaString("Recovery hint when isError is true"),
		"availableInstances": schemaArray("Available instances when selector failed", mcpInstanceSummarySchema()),
	})
}

func mcpInstanceWeatherOutputSchema() map[string]any {
	return schemaObject(map[string]any{
		"instance": mcpInstanceRefSchema(),
		"weather": schemaObject(map[string]any{
			"ambientTempC":                  schemaInteger("Ambient temperature in Celsius"),
			"trackTempC":                    schemaInteger("Track temperature in Celsius when configured"),
			"cloudLevel":                    schemaNumber("Cloud level from 0 to 1"),
			"rain":                          schemaNumber("Rain level from 0 to 1"),
			"weatherRandomness":             schemaInteger("ACC weather randomness"),
			"simracerWeatherConditions":     schemaInteger("ACC simracer weather conditions flag"),
			"isFixedConditionQualification": schemaBoolean("Whether qualification uses fixed conditions"),
			"summary":                       schemaString("Human-readable weather summary"),
		}),
		"sourcePaths":        schemaArray("Source ACC JSON paths", schemaString("ACC path")),
		"code":               schemaString("Error code when isError is true"),
		"message":            schemaString("Error message when isError is true"),
		"recoveryHint":       schemaString("Recovery hint when isError is true"),
		"availableInstances": schemaArray("Available instances when selector failed", mcpInstanceSummarySchema()),
	})
}

func mcpInstanceTrackOutputSchema() map[string]any {
	return schemaObject(map[string]any{
		"instance": mcpInstanceRefSchema(),
		"track": schemaObject(map[string]any{
			"configuredTrack": schemaString("Track configured in acc.event.track"),
			"liveTrack":       schemaString("Track reported by live state when the server is running"),
			"effectiveTrack":  schemaString("Live track when available, otherwise configured track"),
			"summary":         schemaString("Human-readable track summary"),
		}),
		"sourcePaths":        schemaArray("Source configuration/live paths", schemaString("ACCWeb or ACC path")),
		"code":               schemaString("Error code when isError is true"),
		"message":            schemaString("Error message when isError is true"),
		"recoveryHint":       schemaString("Recovery hint when isError is true"),
		"availableInstances": schemaArray("Available instances when selector failed", mcpInstanceSummarySchema()),
	})
}

func mcpInstanceConfigOutputSchema() map[string]any {
	return schemaObject(map[string]any{
		"instance":           mcpInstanceRefSchema(),
		"isRunning":          schemaBoolean("Whether the instance process is running"),
		"pid":                schemaInteger("Process id when running"),
		"accWeb":             schemaLooseObject("Redacted ACCWeb instance settings"),
		"acc":                schemaLooseObject("Redacted ACC Dedicated Server JSON configuration"),
		"live":               schemaLooseObject("Redacted live state"),
		"redacted":           schemaArray("Names of redaction classes", schemaString("Redaction class")),
		"code":               schemaString("Error code when isError is true"),
		"message":            schemaString("Error message when isError is true"),
		"recoveryHint":       schemaString("Recovery hint when isError is true"),
		"availableInstances": schemaArray("Available instances when selector failed", mcpInstanceSummarySchema()),
	})
}

func mcpSetParametersOutputSchema() map[string]any {
	return schemaObject(map[string]any{
		"instanceId":         schemaString("ACCWeb instance id"),
		"updated":            schemaArray("Applied updates with sensitive values redacted", schemaLooseObject("Parameter patch")),
		"restarted":          schemaBoolean("Whether ACCWeb restarted the instance"),
		"code":               schemaString("Error code when isError is true"),
		"message":            schemaString("Error message when isError is true"),
		"recoveryHint":       schemaString("Recovery hint when isError is true"),
		"availableInstances": schemaArray("Available instances when selector failed", mcpInstanceSummarySchema()),
	})
}

func mcpActionOutputSchema() map[string]any {
	return schemaObject(map[string]any{
		"instance":           mcpInstanceRefSchema(),
		"action":             schemaString("Action performed"),
		"success":            schemaBoolean("Whether the action succeeded"),
		"code":               schemaString("Error code when isError is true"),
		"message":            schemaString("Error message when isError is true"),
		"recoveryHint":       schemaString("Recovery hint when isError is true"),
		"availableInstances": schemaArray("Available instances when selector failed", mcpInstanceSummarySchema()),
	})
}

func mcpCreateQuickRaceOutputSchema() map[string]any {
	return schemaObject(map[string]any{
		"instance": mcpInstanceRefSchema(),
		"config":   schemaLooseObject("Redacted ACC Dedicated Server configuration for the created instance"),
		"code":     schemaString("Error code when isError is true"),
		"message":  schemaString("Error message when isError is true"),
	})
}

func mcpInstanceRefSchema() map[string]any {
	return schemaObject(map[string]any{
		"id":   schemaString("ACCWeb instance id"),
		"name": schemaString("ACC server name"),
	})
}

func mcpInstanceSummarySchema() map[string]any {
	return schemaObject(map[string]any{
		"id":               schemaString("ACCWeb instance id"),
		"name":             schemaString("ACC server name"),
		"isRunning":        schemaBoolean("Whether the process is running"),
		"pid":              schemaInteger("Process id when running"),
		"udpPort":          schemaInteger("UDP port"),
		"tcpPort":          schemaInteger("TCP port"),
		"track":            schemaString("Configured track"),
		"serverState":      schemaString("Live server state"),
		"nrClients":        schemaInteger("Connected clients"),
		"sessionType":      schemaString("Current session type"),
		"sessionPhase":     schemaString("Current session phase"),
		"sessionRemaining": schemaInteger("Remaining session time"),
	})
}

func schemaObject(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func schemaString(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func schemaInteger(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func schemaNumber(description string) map[string]any {
	return map[string]any{"type": "number", "description": description}
}

func schemaBoolean(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func schemaArray(description string, items map[string]any) map[string]any {
	return map[string]any{"type": "array", "description": description, "items": items}
}

func schemaLooseObject(description string) map[string]any {
	return map[string]any{"type": "object", "description": description}
}

func setPathValue(cfg *instance.AccConfigFiles, path string, value any) error {
	if !strings.HasPrefix(path, "acc.") {
		return errors.New("path must start with acc.")
	}

	segments := parsePath(strings.TrimPrefix(path, "acc."))
	if len(segments) == 0 {
		return errors.New("empty parameter path")
	}

	root := reflect.ValueOf(cfg).Elem()
	return setReflectPath(root, segments, value)
}

type pathSegment struct {
	Name  string
	Index *int
}

func parsePath(path string) []pathSegment {
	parts := strings.Split(path, ".")
	segments := make([]pathSegment, 0, len(parts))
	for _, part := range parts {
		seg := pathSegment{Name: part}
		if open := strings.Index(part, "["); open >= 0 && strings.HasSuffix(part, "]") {
			idx, err := strconv.Atoi(part[open+1 : len(part)-1])
			if err == nil {
				seg.Name = part[:open]
				seg.Index = &idx
			}
		}
		segments = append(segments, seg)
	}
	return segments
}

func setReflectPath(current reflect.Value, segments []pathSegment, raw any) error {
	if len(segments) == 0 {
		return setReflectValue(current, raw)
	}

	if current.Kind() == reflect.Pointer {
		current = current.Elem()
	}
	if current.Kind() != reflect.Struct {
		return fmt.Errorf("cannot descend into %s", current.Kind())
	}

	field, ok := fieldByJSONName(current, segments[0].Name)
	if !ok {
		return fmt.Errorf("unknown parameter: %s", segments[0].Name)
	}

	if segments[0].Index != nil {
		if field.Kind() != reflect.Slice {
			return fmt.Errorf("%s is not a list", segments[0].Name)
		}
		idx := *segments[0].Index
		if idx < 0 || idx >= field.Len() {
			return fmt.Errorf("index out of range for %s[%d]", segments[0].Name, idx)
		}
		field = field.Index(idx)
	}

	return setReflectPath(field, segments[1:], raw)
}

func fieldByJSONName(v reflect.Value, name string) (reflect.Value, bool) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		jsonName := strings.Split(sf.Tag.Get("json"), ",")[0]
		if jsonName == "" {
			jsonName = strings.ToLower(sf.Name[:1]) + sf.Name[1:]
		}
		if jsonName == name {
			return v.Field(i), true
		}
	}
	return reflect.Value{}, false
}

func setReflectValue(v reflect.Value, raw any) error {
	if !v.CanSet() {
		return errors.New("field cannot be set")
	}

	switch v.Kind() {
	case reflect.String:
		s, ok := raw.(string)
		if !ok {
			return errors.New("value must be string")
		}
		v.SetString(s)
	case reflect.Int:
		n, err := toInt64(raw)
		if err != nil {
			return err
		}
		v.SetInt(n)
	case reflect.Float64:
		n, err := toFloat64(raw)
		if err != nil {
			return err
		}
		v.SetFloat(n)
	case reflect.Bool:
		b, ok := raw.(bool)
		if !ok {
			return errors.New("value must be boolean")
		}
		v.SetBool(b)
	default:
		data, err := json.Marshal(raw)
		if err != nil {
			return err
		}
		ptr := reflect.New(v.Type())
		if err := json.Unmarshal(data, ptr.Interface()); err != nil {
			return err
		}
		v.Set(ptr.Elem())
	}
	return nil
}

func toInt64(raw any) (int64, error) {
	switch v := raw.(type) {
	case float64:
		return int64(v), nil
	case int:
		return int64(v), nil
	case string:
		return strconv.ParseInt(v, 10, 64)
	default:
		return 0, errors.New("value must be integer")
	}
}

func toFloat64(raw any) (float64, error) {
	switch v := raw.(type) {
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case string:
		return strconv.ParseFloat(v, 64)
	default:
		return 0, errors.New("value must be number")
	}
}

func accParameterDocs() []accParameterDoc {
	return []accParameterDoc{
		{File: "configuration.json", Path: "acc.configuration.configVersion", Type: "integer", Description: "ACC configuration schema version written by ACCWeb."},
		{File: "configuration.json", Path: "acc.configuration.udpPort", Type: "integer", Description: "UDP port used by the ACC server.", Range: "1-65535"},
		{File: "configuration.json", Path: "acc.configuration.tcpPort", Type: "integer", Description: "TCP port used by the ACC server.", Range: "1-65535"},
		{File: "configuration.json", Path: "acc.configuration.maxConnections", Type: "integer", Description: "Maximum network connections accepted by the server."},
		{File: "configuration.json", Path: "acc.configuration.registerToLobby", Type: "integer", Description: "Whether the server registers to the public ACC lobby.", Values: []string{"0", "1"}},
		{File: "configuration.json", Path: "acc.configuration.lanDiscovery", Type: "integer", Description: "Whether LAN discovery is enabled.", Values: []string{"0", "1"}},
		{File: "configuration.json", Path: "acc.configuration.publicIP", Type: "string", Description: "Optional public IP override for lobby registration."},

		{File: "settings.json", Path: "acc.settings.configVersion", Type: "integer", Description: "ACC settings schema version written by ACCWeb."},
		{File: "settings.json", Path: "acc.settings.serverName", Type: "string", Description: "Server name shown to clients."},
		{File: "settings.json", Path: "acc.settings.password", Type: "string", Description: "Join password. Empty means public/no password."},
		{File: "settings.json", Path: "acc.settings.adminPassword", Type: "string", Description: "Password for in-game admin commands."},
		{File: "settings.json", Path: "acc.settings.spectatorPassword", Type: "string", Description: "Password for spectator access."},
		{File: "settings.json", Path: "acc.settings.trackMedalsRequirement", Type: "integer", Description: "Required track medals.", Range: "0-3"},
		{File: "settings.json", Path: "acc.settings.safetyRatingRequirement", Type: "integer", Description: "Required safety rating. -1 disables the requirement.", Range: "-1-99"},
		{File: "settings.json", Path: "acc.settings.racecraftRatingRequirement", Type: "integer", Description: "Required racecraft rating. -1 disables the requirement.", Range: "-1-99"},
		{File: "settings.json", Path: "acc.settings.ignorePrematureDisconnects", Type: "integer", Description: "Controls whether early disconnects are ignored by server result handling.", Values: []string{"0", "1"}},
		{File: "settings.json", Path: "acc.settings.dumpLeaderboards", Type: "integer", Description: "Controls leaderboard dump output.", Values: []string{"0", "1"}},
		{File: "settings.json", Path: "acc.settings.isRaceLocked", Type: "integer", Description: "Locks the race session from new joins after the session has started.", Values: []string{"0", "1"}},
		{File: "settings.json", Path: "acc.settings.randomizeTrackWhenEmpty", Type: "integer", Description: "Allows track randomization while the server is empty.", Values: []string{"0", "1"}},
		{File: "settings.json", Path: "acc.settings.maxCarSlots", Type: "integer", Description: "Maximum visible car slots."},
		{File: "settings.json", Path: "acc.settings.centralEntryListPath", Type: "string", Description: "Optional path to a central entry list file."},
		{File: "settings.json", Path: "acc.settings.shortFormationLap", Type: "integer", Description: "Legacy short formation lap switch.", Values: []string{"0", "1"}},
		{File: "settings.json", Path: "acc.settings.allowAutoDQ", Type: "integer", Description: "Allows automatic disqualification by the ACC server.", Values: []string{"0", "1"}},
		{File: "settings.json", Path: "acc.settings.dumpEntryList", Type: "integer", Description: "Controls entry list dump output.", Values: []string{"0", "1"}},
		{File: "settings.json", Path: "acc.settings.formationLapType", Type: "integer", Description: "Formation lap mode.", Values: []string{"0", "1", "3"}},
		{File: "settings.json", Path: "acc.settings.carGroup", Type: "string", Description: "Allowed car group.", Values: []string{"FreeForAll", "GT2", "GT3", "GT4", "GTC", "TCX"}},

		{File: "event.json", Path: "acc.event.configVersion", Type: "integer", Description: "ACC event schema version written by ACCWeb."},
		{File: "event.json", Path: "acc.event.track", Type: "string", Description: "ACC track id selected for the event.", AllowedValuesResource: "accweb://tracks"},
		{File: "event.json", Path: "acc.event.preRaceWaitingTimeSeconds", Type: "integer", Description: "Waiting time before the race starts, in seconds."},
		{File: "event.json", Path: "acc.event.sessionOverTimeSeconds", Type: "integer", Description: "Extra time after session end before the server advances, in seconds."},
		{File: "event.json", Path: "acc.event.ambientTemp", Type: "integer", Description: "Ambient temperature in Celsius."},
		{File: "event.json", Path: "acc.event.trackTemp", Type: "integer", Description: "Optional track temperature override in Celsius."},
		{File: "event.json", Path: "acc.event.cloudLevel", Type: "number", Description: "Cloud level from 0 to 1.", Range: "0-1"},
		{File: "event.json", Path: "acc.event.rain", Type: "number", Description: "Rain level from 0 to 1.", Range: "0-1"},
		{File: "event.json", Path: "acc.event.weatherRandomness", Type: "integer", Description: "Weather randomness level."},
		{File: "event.json", Path: "acc.event.sessions", Type: "array", Description: "Race weekend sessions. Set the whole array to add or remove sessions."},
		{File: "event.json", Path: "acc.event.sessions[0].hourOfDay", Type: "integer", Description: "Session start hour.", Range: "0-23"},
		{File: "event.json", Path: "acc.event.sessions[0].dayOfWeekend", Type: "integer", Description: "Session day in the race weekend."},
		{File: "event.json", Path: "acc.event.sessions[0].timeMultiplier", Type: "integer", Description: "In-session time acceleration multiplier."},
		{File: "event.json", Path: "acc.event.sessions[0].sessionType", Type: "string", Description: "Session type.", Values: []string{"P", "Q", "R"}},
		{File: "event.json", Path: "acc.event.sessions[0].sessionDurationMinutes", Type: "integer", Description: "Session duration in minutes."},
		{File: "event.json", Path: "acc.event.metaData", Type: "string", Description: "Optional event metadata."},
		{File: "event.json", Path: "acc.event.postQualySeconds", Type: "integer", Description: "Time between qualifying and the next session, in seconds."},
		{File: "event.json", Path: "acc.event.postRaceSeconds", Type: "integer", Description: "Time after race finish before the event ends, in seconds."},
		{File: "event.json", Path: "acc.event.simracerWeatherConditions", Type: "integer", Description: "Controls whether weather conditions are sim-racer oriented.", Values: []string{"0", "1"}},
		{File: "event.json", Path: "acc.event.isFixedConditionQualification", Type: "integer", Description: "Keeps qualifying conditions fixed.", Values: []string{"0", "1"}},

		{File: "eventRules.json", Path: "acc.eventRules.configVersion", Type: "integer", Description: "ACC event rules schema version written by ACCWeb."},
		{File: "eventRules.json", Path: "acc.eventRules.qualifyStandingType", Type: "integer", Description: "Qualifying standing calculation mode."},
		{File: "eventRules.json", Path: "acc.eventRules.pitWindowLengthSec", Type: "integer", Description: "Pit window length in seconds. -1 disables a fixed pit window."},
		{File: "eventRules.json", Path: "acc.eventRules.driverStintTimeSec", Type: "integer", Description: "Maximum driver stint time in seconds. -1 disables the limit."},
		{File: "eventRules.json", Path: "acc.eventRules.mandatoryPitstopCount", Type: "integer", Description: "Mandatory pitstop count."},
		{File: "eventRules.json", Path: "acc.eventRules.maxTotalDrivingTime", Type: "integer", Description: "Maximum total driving time in seconds. -1 disables the limit."},
		{File: "eventRules.json", Path: "acc.eventRules.maxDriversCount", Type: "integer", Description: "Maximum number of drivers per entry."},
		{File: "eventRules.json", Path: "acc.eventRules.isRefuellingAllowedInRace", Type: "boolean", Description: "Allows refuelling during the race."},
		{File: "eventRules.json", Path: "acc.eventRules.isRefuellingTimeFixed", Type: "boolean", Description: "Uses fixed refuelling time."},
		{File: "eventRules.json", Path: "acc.eventRules.isMandatoryPitstopRefuellingRequired", Type: "boolean", Description: "Requires refuelling for mandatory pitstops."},
		{File: "eventRules.json", Path: "acc.eventRules.isMandatoryPitstopTyreChangeRequired", Type: "boolean", Description: "Requires tyre change for mandatory pitstops."},
		{File: "eventRules.json", Path: "acc.eventRules.isMandatoryPitstopSwapDriverRequired", Type: "boolean", Description: "Requires driver swap for mandatory pitstops."},
		{File: "eventRules.json", Path: "acc.eventRules.tyreSetCount", Type: "integer", Description: "Available tyre sets."},

		{File: "entrylist.json", Path: "acc.entrylist.configVersion", Type: "integer", Description: "ACC entry list schema version written by ACCWeb."},
		{File: "entrylist.json", Path: "acc.entrylist.entries", Type: "array", Description: "Manual entry list. Set the whole array to add or remove entries."},
		{File: "entrylist.json", Path: "acc.entrylist.entries[0].drivers", Type: "array", Description: "Drivers assigned to this entry."},
		{File: "entrylist.json", Path: "acc.entrylist.entries[0].drivers[0].firstName", Type: "string", Description: "Driver first name."},
		{File: "entrylist.json", Path: "acc.entrylist.entries[0].drivers[0].lastName", Type: "string", Description: "Driver last name."},
		{File: "entrylist.json", Path: "acc.entrylist.entries[0].drivers[0].shortName", Type: "string", Description: "Driver short name."},
		{File: "entrylist.json", Path: "acc.entrylist.entries[0].drivers[0].driverCategory", Type: "integer", Description: "Driver category used by ACC."},
		{File: "entrylist.json", Path: "acc.entrylist.entries[0].drivers[0].playerID", Type: "string", Description: "Driver platform/player id."},
		{File: "entrylist.json", Path: "acc.entrylist.entries[0].drivers[0].nationality", Type: "integer", Description: "Driver nationality id."},
		{File: "entrylist.json", Path: "acc.entrylist.entries[0].raceNumber", Type: "integer", Description: "Forced race number for this entry."},
		{File: "entrylist.json", Path: "acc.entrylist.entries[0].forcedCarModel", Type: "integer", Description: "Forced ACC car model id."},
		{File: "entrylist.json", Path: "acc.entrylist.entries[0].overrideDriverInfo", Type: "integer", Description: "Controls whether entry driver info overrides profile data.", Values: []string{"0", "1"}},
		{File: "entrylist.json", Path: "acc.entrylist.entries[0].isServerAdmin", Type: "integer", Description: "Marks the entry as server admin.", Values: []string{"0", "1"}},
		{File: "entrylist.json", Path: "acc.entrylist.entries[0].customCar", Type: "string", Description: "Optional custom car data."},
		{File: "entrylist.json", Path: "acc.entrylist.entries[0].overrideCarModelForCustomCar", Type: "integer", Description: "Controls car model override for custom cars.", Values: []string{"0", "1"}},
		{File: "entrylist.json", Path: "acc.entrylist.entries[0].ballastKg", Type: "integer", Description: "Ballast in kilograms for this entry."},
		{File: "entrylist.json", Path: "acc.entrylist.entries[0].restrictor", Type: "integer", Description: "Air restrictor value for this entry."},
		{File: "entrylist.json", Path: "acc.entrylist.entries[0].defaultGridPosition", Type: "integer", Description: "Default grid position for this entry."},
		{File: "entrylist.json", Path: "acc.entrylist.forceEntryList", Type: "integer", Description: "Forces the server to use only listed entries.", Values: []string{"0", "1"}},

		{File: "bop.json", Path: "acc.bop.configVersion", Type: "integer", Description: "ACC BOP schema version written by ACCWeb."},
		{File: "bop.json", Path: "acc.bop.entries", Type: "array", Description: "Balance of performance overrides. Set the whole array to add or remove entries."},
		{File: "bop.json", Path: "acc.bop.entries[0].track", Type: "string", Description: "Track id for this BOP override."},
		{File: "bop.json", Path: "acc.bop.entries[0].carModel", Type: "integer", Description: "ACC car model id for this BOP override."},
		{File: "bop.json", Path: "acc.bop.entries[0].ballastKg", Type: "integer", Description: "BOP ballast in kilograms."},
		{File: "bop.json", Path: "acc.bop.entries[0].restrictor", Type: "integer", Description: "BOP air restrictor value."},

		{File: "assistRules.json", Path: "acc.assistRules.configVersion", Type: "integer", Description: "ACC assist rules schema version written by ACCWeb."},
		{File: "assistRules.json", Path: "acc.assistRules.stabilityControlLevelMax", Type: "integer", Description: "Maximum stability control percentage.", Range: "0-100"},
		{File: "assistRules.json", Path: "acc.assistRules.disableAutosteer", Type: "integer", Description: "Disables autosteer assist.", Values: []string{"0", "1"}},
		{File: "assistRules.json", Path: "acc.assistRules.disableAutoLights", Type: "integer", Description: "Disables automatic lights.", Values: []string{"0", "1"}},
		{File: "assistRules.json", Path: "acc.assistRules.disableAutoWiper", Type: "integer", Description: "Disables automatic wipers.", Values: []string{"0", "1"}},
		{File: "assistRules.json", Path: "acc.assistRules.disableAutoEngineStart", Type: "integer", Description: "Disables automatic engine start.", Values: []string{"0", "1"}},
		{File: "assistRules.json", Path: "acc.assistRules.disableAutoPitLimiter", Type: "integer", Description: "Disables automatic pit limiter.", Values: []string{"0", "1"}},
		{File: "assistRules.json", Path: "acc.assistRules.disableAutoGear", Type: "integer", Description: "Disables automatic gears.", Values: []string{"0", "1"}},
		{File: "assistRules.json", Path: "acc.assistRules.disableAutoClutch", Type: "integer", Description: "Disables automatic clutch.", Values: []string{"0", "1"}},
		{File: "assistRules.json", Path: "acc.assistRules.disableIdealLine", Type: "integer", Description: "Disables the ideal racing line.", Values: []string{"0", "1"}},
	}
}
