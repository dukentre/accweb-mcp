# Docker deployment

This fork ships a Docker image and a compose file for running ACCWeb with Wine.
The ACC Dedicated Server files are provided manually by mounting a host folder.

## Image

`accweb/accweb`

- Builds the ACCWeb Go/Vue application.
- Includes Wine for running `accServer.exe` on Linux.
- Reads its runtime config from environment variables.
- Persists ACCWeb instance configs in `/accweb/config`.

## Quick start

Copy `.env.example` to `.env`, change the three ACCWeb passwords, and put the
ACC Dedicated Server installation into `./accserver`.

Expected default layout:

```text
./accserver/server/accServer.exe
```

Then run:

```sh
docker compose up -d --build
```

Open ACCWeb at:

```text
http://localhost:8080
```

ACCWeb starts and uses `/accserver/server/accServer.exe` by default.

The same flow is also available through Make:

```sh
make compose-up
```

## Volumes

`./accserver`

- Host directory with manually provided ACC Dedicated Server files.
- ACCWeb uses `/accserver/server` as the ACC server path.
- Ignored by git so server binaries are not committed.

`accweb-config`

- Stores ACCWeb-managed server profiles and exported ACC JSON files.

`accweb-secrets`

- Stores generated JWT signing keys.

`acccerts`

- Optional TLS certificate storage if TLS is enabled.

## Ports

The compose defaults match the current simple ACC server setup:

- `8080/tcp` for ACCWeb
- `8999/udp` for LAN discovery
- `9231/udp` for ACC UDP traffic
- `9232/tcp` for ACC TCP traffic

If you configure different ACC ports in the web UI, update `.env` and recreate
the containers so Docker publishes the same ports.

## Updating ACC server files

Stop ACCWeb, replace the files in `./accserver`, then start it again:

```sh
docker compose stop accweb
# replace ./accserver contents
docker compose restart accweb
```

If you mount a different host directory, set `ACCSERVER_HOST_PATH` in `.env`.
