# MCP server

ACCWeb MCP exposes ACCWeb as a Model Context Protocol server over HTTP. The point of the MCP server is to make the ACC Dedicated Server configuration machine-readable and safely editable by an MCP client or agent.

Instead of guessing JSON file names and paths, a client can:

* read `accweb://parameters` to learn every supported ACC JSON path, type and description
* read `accweb://instances` to discover configured ACCWeb instances and runtime state
* read `accweb://instances/{id}/config` to inspect a full instance configuration
* call tools to create, update, start and stop instances
* use prompts that guide common workflows such as quick race setup or parameter explanation

## Transport

Endpoint:

```text
POST /mcp
```

ACCWeb MCP implements JSON-RPC 2.0 over MCP Streamable HTTP with protocol version:

```text
2025-06-18
```

Required request headers:

```http
Authorization: Bearer <ACCWEB_MCP_TOKEN>
Content-Type: application/json
Accept: application/json, text/event-stream
MCP-Protocol-Version: 2025-06-18
```

The server returns `application/json` responses. It does not currently open server-to-client SSE streams, so authenticated `GET /mcp` and `DELETE /mcp` requests return `405 Method Not Allowed`.

If a request includes an unsupported `MCP-Protocol-Version` header, the server returns HTTP `400 Bad Request`. Requests without the header are still accepted for compatibility with simpler clients and initialization probes.

## Authentication

Enable MCP and set a token:

```env
ACCWEB_MCP_ENABLED=true
ACCWEB_MCP_TOKEN=change-this-long-random-token
```

Every MCP request must include:

```http
Authorization: Bearer change-this-long-random-token
```

Do not put the token in the URL query string.

For browser-based clients, you can restrict allowed origins:

```env
ACCWEB_MCP_ALLOWED_ORIGINS=https://client.example.com,http://localhost:5173
```

If `ACCWEB_MCP_ALLOWED_ORIGINS` is empty, requests without an `Origin` header are accepted. If it is set, a request with a non-matching `Origin` receives `403 Forbidden`.

This implementation uses a static bearer token from the environment. It is intentionally simple for self-hosted deployments and does not provide OAuth discovery endpoints.

## Initialize

```sh
curl -s http://localhost:8080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H 'MCP-Protocol-Version: 2025-06-18' \
  -H 'Authorization: Bearer change-this-long-random-token' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"1.0.0"}}}'
```

The response declares these capabilities:

* `resources`
* `prompts`
* `tools`

## Resources

List resources:

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "resources/list",
  "params": {}
}
```

Read the parameter reference:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "resources/read",
  "params": {
    "uri": "accweb://parameters"
  }
}
```

Available resources:

* `accweb://parameters` - ACC JSON parameter reference with file, path, type, description, ranges and known values
* `accweb://instances` - configured ACCWeb instances and runtime state
* `accweb://instances/{id}/config` - full ACCWeb and ACC JSON configuration for one instance

Parameter paths use this shape:

```text
acc.<fileWithoutJson>.<jsonField>
```

Examples:

```text
acc.configuration.registerToLobby
acc.settings.maxCarSlots
acc.event.track
acc.event.sessions[0].hourOfDay
acc.eventRules.mandatoryPitstopCount
acc.assistRules.stabilityControlLevelMax
```

## Prompts

List prompts:

```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "prompts/list",
  "params": {}
}
```

Available prompts:

* `configure_quick_race` - asks the model to configure a simple quick race using the parameter resource and tools
* `explain_parameter` - asks the model to explain one parameter path and operational impact

Get a prompt:

```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "method": "prompts/get",
  "params": {
    "name": "explain_parameter",
    "arguments": {
      "path": "acc.event.weatherRandomness"
    }
  }
}
```

## Tools

List tools:

```json
{
  "jsonrpc": "2.0",
  "id": 6,
  "method": "tools/list",
  "params": {}
}
```

Available tools:

* `list_instances` - returns configured instances and runtime state
* `get_instance_status` - returns runtime state, clients, session, ports and track
* `get_instance_weather` - returns semantic weather fields, summary and source paths
* `get_instance_track` - returns configured, live and effective track
* `get_instance_config` - fallback/debug tool that returns redacted full configuration
* `set_instance_parameters` - updates one or more ACC JSON values by path
* `start_instance` - starts an ACC server instance
* `stop_instance` - stops an ACC server instance
* `create_quick_race_instance` - creates a simple qualifying/race instance

All tools include `outputSchema`. Successful tool calls return `structuredContent` and the same JSON serialized into a text content block for backward compatibility.

`list_instances`, `get_instance_status`, `get_instance_weather`, `get_instance_track`, and `get_instance_config` include:

```json
{
  "readOnlyHint": true,
  "destructiveHint": false,
  "idempotentHint": true,
  "openWorldHint": false
}
```

Mutating tools include `annotations.readOnlyHint: false`; `set_instance_parameters` is also marked with `destructiveHint: true` because it overwrites existing ACC JSON values.

Read-only tools accept `instanceIdOrName` instead of requiring a strict id. It can be an ACCWeb id, exact server name, partial server name, or omitted when there is only one running/configured instance.

If an instance selector cannot be resolved, tools return `isError: true` with actionable `structuredContent`, including `code`, `message`, `recoveryHint`, and `availableInstances` when useful.

If an instance is running, `set_instance_parameters` requires `restartIfLive: true`. ACCWeb will stop the instance, save the configuration, and start it again.

## Resource templates

`resources/templates/list` returns:

* `accweb://instances`
* `accweb://instances/{instanceId}/status`
* `accweb://instances/{instanceId}/weather`
* `accweb://instances/{instanceId}/config`

Resources and templates include annotations for assistant use:

```json
{
  "audience": ["assistant"],
  "priority": 0.95
}
```

## Change weather

For read-only weather questions, use `get_instance_weather`:

```json
{
  "jsonrpc": "2.0",
  "id": 7,
  "method": "tools/call",
  "params": {
    "name": "get_instance_weather",
    "arguments": {
      "instanceIdOrName": "Dukentre"
    }
  }
}
```

For updates, weather is stored in `event.json` and can be changed with `set_instance_parameters`.

```json
{
  "jsonrpc": "2.0",
  "id": 8,
  "method": "tools/call",
  "params": {
    "name": "set_instance_parameters",
    "arguments": {
      "instanceId": "INSTANCE_ID",
      "restartIfLive": true,
      "updates": [
        { "path": "acc.event.ambientTemp", "value": 18 },
        { "path": "acc.event.cloudLevel", "value": 0.8 },
        { "path": "acc.event.rain", "value": 0.3 },
        { "path": "acc.event.weatherRandomness", "value": 2 }
      ]
    }
  }
}
```

Useful weather paths:

* `acc.event.ambientTemp`
* `acc.event.trackTemp`
* `acc.event.cloudLevel`
* `acc.event.rain`
* `acc.event.weatherRandomness`
* `acc.event.simracerWeatherConditions`

## Change time of day

Time of day is configured per session:

```json
{
  "jsonrpc": "2.0",
  "id": 9,
  "method": "tools/call",
  "params": {
    "name": "set_instance_parameters",
    "arguments": {
      "instanceId": "INSTANCE_ID",
      "restartIfLive": true,
      "updates": [
        { "path": "acc.event.sessions[0].hourOfDay", "value": 21 },
        { "path": "acc.event.sessions[0].timeMultiplier", "value": 2 }
      ]
    }
  }
}
```

Use indexes for existing sessions:

```text
acc.event.sessions[0].hourOfDay
acc.event.sessions[1].hourOfDay
acc.event.sessions[1].sessionDurationMinutes
```

## Multiple sessions

You can run one session or multiple sessions. ACC accepts practice (`P`), qualifying (`Q`) and race (`R`) session types.

To add or remove sessions, set the whole `acc.event.sessions` array:

```json
{
  "jsonrpc": "2.0",
  "id": 10,
  "method": "tools/call",
  "params": {
    "name": "set_instance_parameters",
    "arguments": {
      "instanceId": "INSTANCE_ID",
      "restartIfLive": true,
      "updates": [
        {
          "path": "acc.event.sessions",
          "value": [
            {
              "hourOfDay": 10,
              "dayOfWeekend": 1,
              "timeMultiplier": 1,
              "sessionType": "P",
              "sessionDurationMinutes": 20
            },
            {
              "hourOfDay": 13,
              "dayOfWeekend": 2,
              "timeMultiplier": 1,
              "sessionType": "Q",
              "sessionDurationMinutes": 15
            },
            {
              "hourOfDay": 16,
              "dayOfWeekend": 3,
              "timeMultiplier": 1,
              "sessionType": "R",
              "sessionDurationMinutes": 45
            }
          ]
        }
      ]
    }
  }
}
```

To keep only one race session, set the array to one object:

```json
[
  {
    "hourOfDay": 14,
    "dayOfWeekend": 3,
    "timeMultiplier": 1,
    "sessionType": "R",
    "sessionDurationMinutes": 30
  }
]
```

## Create a quick race instance

```json
{
  "jsonrpc": "2.0",
  "id": 11,
  "method": "tools/call",
  "params": {
    "name": "create_quick_race_instance",
    "arguments": {
      "serverName": "ACCWeb MCP Server",
      "track": "monza",
      "carGroup": "GT3",
      "maxCarSlots": 20,
      "qualifyingMinutes": 10,
      "raceMinutes": 30,
      "hourOfDay": 14,
      "registerToLobby": 1,
      "lanDiscovery": 0,
      "tcpPort": 9232,
      "udpPort": 9231
    }
  }
}
```

## Operational notes

* The MCP server changes ACCWeb instance configuration, then ACCWeb writes the ACC JSON files.
* Running instances must be restarted for ACC server settings to take effect.
* Docker port publishing must match the ports configured in ACCWeb.
* Keep `ACCWEB_MCP_TOKEN` secret and use HTTPS or a trusted private network for remote access.
* Restrict `ACCWEB_MCP_ALLOWED_ORIGINS` when exposing the endpoint to browser-based MCP clients.
