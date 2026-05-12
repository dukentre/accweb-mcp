package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/assetto-corsa-web/accweb/internal/pkg/instance"
	"github.com/gin-gonic/gin"
)

const (
	mcpProtocolVersion = "2025-06-18"
	mcpEndpointPath    = "/mcp"
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
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type mcpResource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

type mcpPrompt struct {
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Arguments   []mcpPromptArgument `json:"arguments,omitempty"`
}

type mcpPromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type accParameterDoc struct {
	File        string   `json:"file"`
	Path        string   `json:"path"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Values      []string `json:"values,omitempty"`
	Range       string   `json:"range,omitempty"`
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

type mcpInstanceIDArgs struct {
	InstanceID string `json:"instanceId"`
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
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodDelete {
		c.Header("Allow", "POST")
		c.Status(http.StatusMethodNotAllowed)
		return
	}

	if c.Request.Method != http.MethodPost {
		c.Status(http.StatusMethodNotAllowed)
		return
	}

	if !h.authorizeMCP(c) {
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

	c.Header("MCP-Protocol-Version", mcpProtocolVersion)
	if req.ID == nil && strings.HasPrefix(req.Method, "notifications/") {
		c.Status(http.StatusAccepted)
		return
	}

	c.JSON(http.StatusOK, h.dispatchMCP(req))
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
	case "resources/read":
		result, err = h.mcpResourcesRead(req.Params)
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
			"resources": map[string]any{},
			"prompts":   map[string]any{},
			"tools":     map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "accweb-mcp",
			"version": "0.1.0",
		},
	}
}

func (h *Handler) mcpResourcesList() map[string]any {
	resources := []mcpResource{
		{URI: "accweb://parameters", Name: "ACC server parameter reference", Description: "All ACCWeb-managed ACC Dedicated Server parameters and descriptions.", MimeType: "application/json"},
		{URI: "accweb://instances", Name: "ACCWeb instances", Description: "Configured ACC server instances and runtime status.", MimeType: "application/json"},
	}

	for _, srv := range h.sm.GetServers() {
		resources = append(resources, mcpResource{
			URI:         "accweb://instances/" + srv.GetID() + "/config",
			Name:        "Instance " + srv.GetID() + " configuration",
			Description: "Full ACCWeb and ACC JSON configuration for this instance.",
			MimeType:    "application/json",
		})
	}

	return map[string]any{"resources": resources}
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
	case params.URI == "accweb://instances":
		payload = h.mcpInstanceSummaries()
	case strings.HasPrefix(params.URI, "accweb://instances/") && strings.HasSuffix(params.URI, "/config"):
		id := strings.TrimSuffix(strings.TrimPrefix(params.URI, "accweb://instances/"), "/config")
		srv, err := h.sm.GetServerByID(id)
		if err != nil {
			return nil, err
		}
		payload = map[string]any{
			"id":        srv.GetID(),
			"path":      srv.Path,
			"isRunning": srv.IsRunning(),
			"pid":       srv.GetProcessID(),
			"accWeb":    srv.Cfg.Settings,
			"acc":       srv.AccCfg,
			"live":      srv.Live,
		}
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
				"uri":      params.URI,
				"mimeType": "application/json",
				"text":     string(text),
			},
		},
	}, nil
}

func (h *Handler) mcpInstanceSummaries() []ListServerItem {
	out := []ListServerItem{}
	for _, srv := range h.sm.GetServers() {
		out = append(out, buildListServerItem(srv))
	}
	return out
}

func (h *Handler) mcpPromptsList() map[string]any {
	return map[string]any{
		"prompts": []mcpPrompt{
			{
				Name:        "configure_quick_race",
				Description: "Prepare an ACC quick race configuration with track, car group, slots and session durations.",
				Arguments: []mcpPromptArgument{
					{Name: "track", Description: "ACC track id, e.g. monza", Required: true},
					{Name: "carGroup", Description: "Car group, e.g. GT3", Required: true},
					{Name: "qualifyingMinutes", Description: "Qualifying duration in minutes", Required: true},
					{Name: "raceMinutes", Description: "Race duration in minutes", Required: true},
				},
			},
			{
				Name:        "explain_parameter",
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
				Name:        "list_instances",
				Description: "List ACCWeb server instances and runtime state.",
				InputSchema: schemaObject(map[string]any{}),
			},
			{
				Name:        "get_instance_config",
				Description: "Get one instance's ACCWeb and ACC configuration.",
				InputSchema: schemaObject(map[string]any{"instanceId": schemaString("ACCWeb instance id")}, "instanceId"),
			},
			{
				Name:        "set_instance_parameters",
				Description: "Update one or more ACC configuration parameters using paths like acc.settings.maxCarSlots or acc.event.sessions[0].sessionDurationMinutes. The instance must be stopped unless restartIfLive is true.",
				InputSchema: schemaObject(map[string]any{
					"instanceId":    schemaString("ACCWeb instance id"),
					"restartIfLive": schemaBoolean("Stop the instance before saving and restart it afterwards"),
					"updates": map[string]any{
						"type":        "array",
						"description": "Parameter updates",
						"items": schemaObject(map[string]any{
							"path":  schemaString("Parameter path"),
							"value": map[string]any{"description": "New JSON value"},
						}, "path", "value"),
					},
				}, "instanceId", "updates"),
			},
			{
				Name:        "start_instance",
				Description: "Start an ACC server instance.",
				InputSchema: schemaObject(map[string]any{"instanceId": schemaString("ACCWeb instance id")}, "instanceId"),
			},
			{
				Name:        "stop_instance",
				Description: "Stop an ACC server instance.",
				InputSchema: schemaObject(map[string]any{"instanceId": schemaString("ACCWeb instance id")}, "instanceId"),
			},
			{
				Name:        "create_quick_race_instance",
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
	case "list_instances":
		return mcpToolJSON(h.mcpInstanceSummaries())
	case "get_instance_config":
		var args mcpInstanceIDArgs
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, err
		}
		srv, err := h.sm.GetServerByID(args.InstanceID)
		if err != nil {
			return nil, err
		}
		return mcpToolJSON(map[string]any{"id": srv.GetID(), "accWeb": srv.Cfg.Settings, "acc": srv.AccCfg, "live": srv.Live})
	case "set_instance_parameters":
		var args mcpSetParametersArgs
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, err
		}
		return h.mcpSetInstanceParameters(args)
	case "start_instance":
		var args mcpInstanceIDArgs
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, err
		}
		if err := h.sm.Start(args.InstanceID); err != nil {
			return nil, err
		}
		return mcpToolText("instance started: " + args.InstanceID), nil
	case "stop_instance":
		var args mcpInstanceIDArgs
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, err
		}
		srv, err := h.sm.GetServerByID(args.InstanceID)
		if err != nil {
			return nil, err
		}
		if err := srv.Stop(); err != nil {
			return nil, err
		}
		return mcpToolText("instance stopped: " + args.InstanceID), nil
	case "create_quick_race_instance":
		var args mcpCreateQuickRaceArgs
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, err
		}
		return h.mcpCreateQuickRaceInstance(args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", params.Name)
	}
}

func (h *Handler) mcpSetInstanceParameters(args mcpSetParametersArgs) (map[string]any, error) {
	srv, err := h.sm.GetServerByID(args.InstanceID)
	if err != nil {
		return nil, err
	}

	wasRunning := srv.IsRunning()
	if wasRunning {
		if !args.RestartIfLive {
			return nil, errors.New("instance is running; set restartIfLive=true to stop, save and restart")
		}
		if err := srv.Stop(); err != nil {
			return nil, err
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
			return nil, err
		}
	}

	return mcpToolJSON(map[string]any{"updated": args.Updates, "restarted": wasRunning, "instanceId": args.InstanceID})
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

	return mcpToolJSON(map[string]any{"instanceId": srv.GetID(), "config": srv.AccCfg})
}

func mcpToolText(text string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}
}

func mcpToolJSON(v any) (map[string]any, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return mcpToolText(string(data)), nil
}

func schemaObject(properties map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func schemaString(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func schemaInteger(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func schemaBoolean(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
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
		{File: "event.json", Path: "acc.event.track", Type: "string", Description: "Track id, e.g. monza."},
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
