# MCP server

ACCWeb exposes a token-protected MCP endpoint at:

```text
POST /mcp
```

The endpoint implements JSON-RPC 2.0 over Streamable HTTP and currently returns
plain JSON responses. Server-to-client SSE streaming is not used, so `GET /mcp`
returns `405 Method Not Allowed`.

## Authentication

Set a token in `.env`:

```env
ACCWEB_MCP_ENABLED=true
ACCWEB_MCP_TOKEN=change-this-token
```

Every MCP request must include:

```http
Authorization: Bearer change-this-token
```

If `ACCWEB_MCP_ALLOWED_ORIGINS` is set, browser MCP clients must also send an
`Origin` header matching one of the comma-separated values.

## Methods

Supported MCP methods:

- `initialize`
- `ping`
- `resources/list`
- `resources/read`
- `prompts/list`
- `prompts/get`
- `tools/list`
- `tools/call`
- `notifications/initialized`

## Resources

`accweb://parameters`

Lists ACC server parameters managed by ACCWeb, including the source JSON file,
path, type, description, allowed values and ranges where known.

`accweb://instances`

Lists configured ACCWeb server instances and runtime state.

`accweb://instances/{id}/config`

Returns the full ACCWeb and ACC JSON configuration for one instance.

## Prompts

`configure_quick_race`

Guides a client/model through configuring a simple quick race.

`explain_parameter`

Explains a parameter path such as `acc.settings.maxCarSlots`.

## Tools

`list_instances`

Returns configured instances and runtime state.

`get_instance_config`

Returns the complete configuration for one instance.

`set_instance_parameters`

Updates one or more ACC JSON values by path. Example paths:

```text
acc.settings.maxCarSlots
acc.configuration.maxConnections
acc.event.track
acc.event.sessions[0].sessionDurationMinutes
```

If the instance is running, pass `restartIfLive: true` to stop, save and restart
it.

`start_instance`

Starts an ACC server instance.

`stop_instance`

Stops an ACC server instance.

`create_quick_race_instance`

Creates a simple Q/R instance with track, car group, slots, qualifying duration
and race duration.

## Example

```sh
curl -s http://localhost:8080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer change-this-token' \
  -d '{"jsonrpc":"2.0","id":1,"method":"resources/list","params":{}}'
```
