# Assetto Corsa Competizione Server Web Interface

[![Go Report Card](https://goreportcard.com/badge/github.com/dukentre/accweb-mcp)](https://goreportcard.com/report/github.com/dukentre/accweb-mcp)

ACCWeb MCP is a fork of [assetto-corsa-web/accweb](https://github.com/assetto-corsa-web/accweb) for managing Assetto Corsa Competizione servers through a web UI, Docker Compose, and a token-protected MCP HTTP endpoint. The fork keeps the original ACCWeb workflow, adds container-first deployment, and gives MCP clients a structured way to read and change ACC Dedicated Server configuration.

## What this fork adds

* a Docker image with ACCWeb plus Wine for running `accServer.exe` on Linux
* a production Compose file that uses the published image, without a local build
* a systemd unit for running the Compose stack as a Linux service
* a manually mounted ACC Dedicated Server folder, so Steam Guard is handled outside the container
* an MCP endpoint at `POST /mcp` protected by `Authorization: Bearer <token>`
* MCP resources with ACC parameter documentation and instance configuration
* MCP prompts and tools for creating, inspecting, updating, starting and stopping instances

## Production install from image

The normal server install uses the published image:

```text
ghcr.io/dukentre/accweb-mcp:latest
```

Requirements:

* Linux host with Docker Engine and the Docker Compose plugin
* ACC Dedicated Server files copied manually to the host
* open TCP/UDP ports that match your ACC server configuration

Prepare the server directory:

```sh
sudo install -d -m 0755 /opt/accweb-mcp/accserver
```

Copy the ACC Dedicated Server installation into `/opt/accweb-mcp/accserver`. The default expected layout is:

```text
/opt/accweb-mcp/accserver/server/accServer.exe
```

Download the production Compose files:

```sh
sudo curl -fsSL -o /opt/accweb-mcp/docker-compose.yml \
  https://raw.githubusercontent.com/dukentre/accweb-mcp/master/deploy/docker-compose.yml
sudo curl -fsSL -o /opt/accweb-mcp/.env \
  https://raw.githubusercontent.com/dukentre/accweb-mcp/master/deploy/.env.example
```

Edit `/opt/accweb-mcp/.env` and change at least:

```env
ACCWEB_ADMIN_PASSWORD=...
ACCWEB_MOD_PASSWORD=...
ACCWEB_RO_PASSWORD=...
ACCWEB_MCP_TOKEN=...
```

Start the stack:

```sh
sudo docker compose --env-file /opt/accweb-mcp/.env \
  -f /opt/accweb-mcp/docker-compose.yml pull
sudo docker compose --env-file /opt/accweb-mcp/.env \
  -f /opt/accweb-mcp/docker-compose.yml up -d
```

Open ACCWeb:

```text
http://SERVER_IP:8080
```

## systemd service

Install the unit:

```sh
sudo curl -fsSL -o /etc/systemd/system/accweb-mcp.service \
  https://raw.githubusercontent.com/dukentre/accweb-mcp/master/deploy/systemd/accweb-mcp.service
sudo systemctl daemon-reload
sudo systemctl enable --now accweb-mcp
```

Useful service commands:

```sh
sudo systemctl status accweb-mcp
sudo systemctl reload accweb-mcp
sudo systemctl stop accweb-mcp
```

`reload` pulls the current image and recreates the Compose stack.

## MCP server

The MCP server is the automation interface for ACCWeb. It lets MCP clients and agents discover ACC parameters as data, read instance state, and call tools that update server JSON files through ACCWeb instead of editing files by hand.

Endpoint:

```text
POST http://SERVER_IP:8080/mcp
```

Required headers:

```http
Authorization: Bearer <ACCWEB_MCP_TOKEN>
Content-Type: application/json
Accept: application/json, text/event-stream
MCP-Protocol-Version: 2025-06-18
```

The implementation uses MCP Streamable HTTP with JSON-RPC 2.0 and returns JSON responses. Server-to-client SSE streaming is not used; `GET /mcp` returns `405 Method Not Allowed`.

MCP capabilities:

* resources: `accweb://parameters`, `accweb://instances`, `accweb://instances/{id}/config`
* prompts: `configure_quick_race`, `explain_parameter`
* tools: `list_instances`, `get_instance_config`, `set_instance_parameters`, `start_instance`, `stop_instance`, `create_quick_race_instance`

Example:

```sh
curl -s http://SERVER_IP:8080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H 'MCP-Protocol-Version: 2025-06-18' \
  -H "Authorization: Bearer $ACCWEB_MCP_TOKEN" \
  -d '{"jsonrpc":"2.0","id":1,"method":"resources/list","params":{}}'
```

See [docs/mcp.md](docs/mcp.md) for configuration, examples, weather/time changes, and multi-session updates.

## Docker development

The repository root `docker-compose.yml` is for local development and builds the image from the current checkout:

```sh
cp .env.example .env
mkdir -p accserver
# copy the ACC Dedicated Server installation into ./accserver
docker compose up -d --build
```

Use `deploy/docker-compose.yml` for production installs from the published image.

See [docs/docker.md](docs/docker.md) for the full layout, volumes, ports, systemd setup, and update flow.

## Original ACCWeb features

* create and manage multiple server instances
* configure instances in the browser
* start/stop instances and monitor their status
* view server logs
* copy server configurations
* import/export server configuration files
* delete server configurations
* three permissions: admin, mod and read only
* instance live view
* HTTP callback for many instance events
* no database required

## Backup

Back up the ACCWeb configuration volume and, for manual binary installs, the `config` directory and `config.yml`. With the provided Compose files, instance data is stored in the `accweb-config` Docker volume and JWT secrets are stored in `accweb-secrets`.

## Development

The frontend is in `public` and the Go backend is in `internal`.

Frontend watcher:

```sh
make run-dev-frontend
```

Backend:

```sh
make run-dev-backend
```

Local checks:

```sh
go test ./internal/app ./cmd
```

## Build release

To build a zip release locally:

```sh
./build/build_release.sh 1.2.3
```

Docker images are published by GitHub Actions to GHCR on `master` and `v*` tags.

## Links

* [ACCWeb MCP repository](https://github.com/dukentre/accweb-mcp)
* [Published Docker image](https://github.com/dukentre/accweb-mcp/pkgs/container/accweb-mcp)
* [Upstream ACCWeb repository](https://github.com/assetto-corsa-web/accweb)
* [Upstream Docker Hub image](https://hub.docker.com/r/accweb/accweb)
* [Assetto Corsa Forums](https://www.assettocorsa.net/forum/index.php?threads/release-accweb-assetto-corsa-competizione-server-management-tool-via-web-interface.57572/)

## License

MIT
