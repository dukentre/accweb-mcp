# Docker deployment

This fork ships two Compose flows:

* `deploy/docker-compose.yml` for production. It pulls `ghcr.io/dukentre/accweb-mcp` and does not build locally.
* `docker-compose.yml` in the repository root for development. It builds the image from the current checkout.

The ACC Dedicated Server files are provided manually by mounting a host folder. This keeps Steam login and Steam Guard outside the container.

## Published image

```text
ghcr.io/dukentre/accweb-mcp:latest
```

The image:

* contains ACCWeb and Wine
* reads runtime configuration from environment variables
* persists ACCWeb instance configs in `/accweb/config`
* expects ACC server files to be mounted into `/accserver`
* exposes the web UI and MCP endpoint on port `8080`

Release tags are created by GitHub Actions:

* `latest` from `master`
* `master` from the default branch build
* `vX.Y.Z`, `X.Y.Z`, `X.Y`, `X` for release tags
* `sha-...` for traceable builds

## Production quick start

Create the install directory:

```sh
sudo install -d -m 0755 /opt/accweb-mcp/accserver
```

Copy the ACC Dedicated Server installation into `/opt/accweb-mcp/accserver`.

Expected default layout:

```text
/opt/accweb-mcp/accserver/server/accServer.exe
```

Download the production files:

```sh
sudo curl -fsSL -o /opt/accweb-mcp/docker-compose.yml \
  https://raw.githubusercontent.com/dukentre/accweb-mcp/master/deploy/docker-compose.yml
sudo curl -fsSL -o /opt/accweb-mcp/.env \
  https://raw.githubusercontent.com/dukentre/accweb-mcp/master/deploy/.env.example
```

Edit `/opt/accweb-mcp/.env`:

```env
ACCWEB_ADMIN_PASSWORD=change-this
ACCWEB_MOD_PASSWORD=change-this
ACCWEB_RO_PASSWORD=change-this
ACCWEB_MCP_TOKEN=change-this-long-random-token
ACCSERVER_HOST_PATH=/opt/accweb-mcp/accserver
```

Detailed Russian env reference: [env.ru.md](env.ru.md).

Start:

```sh
sudo docker compose --env-file /opt/accweb-mcp/.env \
  -f /opt/accweb-mcp/docker-compose.yml up -d
```

Open:

```text
http://SERVER_IP:8080
```

## systemd

Install:

```sh
sudo curl -fsSL -o /etc/systemd/system/accweb-mcp.service \
  https://raw.githubusercontent.com/dukentre/accweb-mcp/master/deploy/systemd/accweb-mcp.service
sudo systemctl daemon-reload
sudo systemctl enable --now accweb-mcp
```

Operate:

```sh
sudo systemctl status accweb-mcp
sudo systemctl reload accweb-mcp
sudo systemctl stop accweb-mcp
sudo journalctl -u accweb-mcp -n 100
```

The unit uses `/opt/accweb-mcp` as its working directory, so Docker Compose automatically reads `/opt/accweb-mcp/.env`.

## Volumes

`ACCSERVER_HOST_PATH`

* Host directory with manually provided ACC Dedicated Server files.
* Mounted read-only to `/accserver`.
* Default production value is `/opt/accweb-mcp/accserver`.
* ACCWeb uses `/accserver/server/accServer.exe` by default.

`accweb-config`

* Stores ACCWeb-managed server profiles and exported ACC JSON files.

`accweb-secrets`

* Stores generated JWT signing keys.

`acccerts`

* Optional TLS certificate storage if TLS is enabled.

## Ports

The Compose defaults match the sample ACC setup:

* `8080/tcp` for ACCWeb and MCP
* `8999/udp` for LAN discovery
* `9231/udp` for ACC UDP traffic
* `9232/tcp` for ACC TCP traffic

If you configure different ACC ports in the web UI, update `.env` and recreate the container so Docker publishes the same ports.

## Updating

With systemd:

```sh
sudo systemctl reload accweb-mcp
```

Without systemd:

```sh
sudo docker compose --env-file /opt/accweb-mcp/.env \
  -f /opt/accweb-mcp/docker-compose.yml pull
sudo docker compose --env-file /opt/accweb-mcp/.env \
  -f /opt/accweb-mcp/docker-compose.yml up -d
```

## Updating ACC server files

Stop ACCWeb, replace the files in `ACCSERVER_HOST_PATH`, then start it again:

```sh
sudo docker compose --env-file /opt/accweb-mcp/.env \
  -f /opt/accweb-mcp/docker-compose.yml stop accweb
# replace /opt/accweb-mcp/accserver contents
sudo docker compose --env-file /opt/accweb-mcp/.env \
  -f /opt/accweb-mcp/docker-compose.yml up -d
```

## Local development build

From the repository root:

```sh
cp .env.example .env
mkdir -p accserver
# copy the ACC Dedicated Server installation into ./accserver
docker compose up -d --build
```

The development Compose file builds `accweb/accweb:local` from the current checkout.
