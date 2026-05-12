# Env-переменные

Этот файл описывает переменные из production-файла `deploy/.env.example`.

Production Compose читает `.env` и передает настройки в контейнер ACCWeb. Обычно файл лежит здесь:

```text
/opt/accweb-mcp/.env
```

После изменения `.env` контейнер нужно пересоздать:

```sh
sudo docker compose --env-file /opt/accweb-mcp/.env \
  -f /opt/accweb-mcp/docker-compose.yml up -d
```

Если используется systemd:

```sh
sudo systemctl restart accweb-mcp
```

## Docker image и имя контейнера

| Переменная | Пример | Что делает |
| --- | --- | --- |
| `ACCWEB_IMAGE` | `ghcr.io/dukentre/accweb-mcp:latest` | Docker image, который будет запущен. Для production обычно используется `latest` или конкретный release tag. |
| `ACCWEB_CONTAINER_NAME` | `accweb-mcp` | Имя контейнера в Docker. Удобно для логов и ручной диагностики. |

## Пути к ACC Dedicated Server

| Переменная | Пример | Что делает |
| --- | --- | --- |
| `ACCSERVER_HOST_PATH` | `/opt/accweb-mcp/accserver` | Папка на хосте с вручную положенными файлами ACC Dedicated Server. Монтируется в контейнер read-only. |
| `ACCWEB_ACC_SERVER_PATH` | `/accserver/server` | Путь внутри контейнера до папки, где лежит `accServer.exe`. Обычно менять не нужно, если layout стандартный. |
| `ACCWEB_ACC_SERVER_EXE` | `accServer.exe` | Имя исполняемого файла ACC server. Обычно менять не нужно. |

Стандартный layout:

```text
ACCSERVER_HOST_PATH=/opt/accweb-mcp/accserver
ACCWEB_ACC_SERVER_PATH=/accserver/server
ACCWEB_ACC_SERVER_EXE=accServer.exe
```

В итоге ACCWeb будет запускать:

```text
/accserver/server/accServer.exe
```

## Порты

| Переменная | Пример | Что делает |
| --- | --- | --- |
| `ACCWEB_HTTP_PORT` | `8080` | Внешний TCP-порт web-интерфейса ACCWeb и MCP endpoint `/mcp`. |
| `ACC_LAN_PORT` | `8999` | Внешний UDP-порт LAN discovery. Должен совпадать с настройками ACC server. |
| `ACC_UDP_PORT` | `9231` | Внешний UDP-порт игрового сервера. Должен совпадать с `acc.configuration.udpPort`. |
| `ACC_TCP_PORT` | `9232` | Внешний TCP-порт игрового сервера. Должен совпадать с `acc.configuration.tcpPort`. |

Важно: Docker публикует только те порты, которые описаны в Compose. Если в web UI поменять ACC UDP/TCP ports, такие же значения нужно прописать в `.env` и пересоздать контейнер.

## Пароли ACCWeb

| Переменная | Пример | Что делает |
| --- | --- | --- |
| `ACCWEB_ADMIN_PASSWORD` | `long-admin-password` | Пароль администратора web-интерфейса. Дает полный доступ. |
| `ACCWEB_MOD_PASSWORD` | `long-mod-password` | Пароль модератора. Подходит для ограниченного управления. |
| `ACCWEB_RO_PASSWORD` | `long-readonly-password` | Read-only пароль. Подходит для просмотра без изменений. |

Эти значения обязательны в production Compose. Не оставляйте `change-me-*`.

## Базовые настройки ACCWeb

| Переменная | Пример | Что делает |
| --- | --- | --- |
| `ACCWEB_TIMEOUT` | `24h` | Время жизни web-сессии/токена ACCWeb. |
| `ACCWEB_ENABLE_TLS` | `false` | Включает TLS внутри ACCWeb. Обычно TLS проще завершать на reverse proxy. |
| `ACCWEB_LOGLEVEL` | `info` | Уровень логов. Частые значения: `debug`, `info`, `warn`, `error`. |
| `ACCWEB_CORS` | `*` | CORS-настройка для HTTP API ACCWeb. Для публичного сервера лучше ограничивать доменом. |
| `ACCWEB_LOG_WITH_TIMESTAMP` | `true` | Добавляет timestamp в логи приложения. |

Если включить `ACCWEB_ENABLE_TLS=true`, сертификат и ключ должны быть доступны внутри контейнера:

```text
/sslcerts/certificate.crt
/sslcerts/private.key
```

В production Compose для этого есть volume `acccerts`.

## MCP

| Переменная | Пример | Что делает |
| --- | --- | --- |
| `ACCWEB_MCP_ENABLED` | `true` | Включает endpoint `POST /mcp`. |
| `ACCWEB_MCP_TOKEN` | `long-random-token` | Bearer token для MCP. Клиент должен отправлять `Authorization: Bearer <token>`. |
| `ACCWEB_MCP_ALLOWED_ORIGINS` | `https://client.example.com,http://localhost:5173` | Список разрешенных browser origins через запятую. Нужен для защиты browser MCP clients. |

Если `ACCWEB_MCP_ALLOWED_ORIGINS` пустой, запросы без `Origin` проходят. Если `Origin` есть и список задан, origin должен совпасть с одним из значений.

Пример MCP-запроса:

```sh
curl -s http://SERVER_IP:8080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H 'MCP-Protocol-Version: 2025-06-18' \
  -H "Authorization: Bearer $ACCWEB_MCP_TOKEN" \
  -d '{"jsonrpc":"2.0","id":1,"method":"ping","params":{}}'
```

## Callback

ACCWeb умеет отправлять HTTP callback на события инстансов.

| Переменная | Пример | Что делает |
| --- | --- | --- |
| `ACCWEB_CALLBACK_ENABLED` | `false` | Включает callback. |
| `ACCWEB_CALLBACK_TIMEOUT` | `500ms` | Timeout HTTP-запроса callback. |
| `ACCWEB_CALLBACK_URL` | `https://example.com/accweb/callback` | URL, куда ACCWeb будет отправлять события. |
| `ACCWEB_CALLBACK_HEADER_KEY` | `Authorization` | Имя дополнительного HTTP header для callback-запросов. |
| `ACCWEB_CALLBACK_HEADER_VALUE` | `Bearer token` | Значение дополнительного HTTP header. |
| `ACCWEB_CALLBACK_EVENTS` | `instance_started,instance_stopped` | Список событий, которые нужно отправлять. Если пусто, поведение зависит от логики ACCWeb callback config. |

Если callback не нужен, оставьте `ACCWEB_CALLBACK_ENABLED=false`.

## Минимальный production `.env`

```env
ACCWEB_IMAGE=ghcr.io/dukentre/accweb-mcp:latest
ACCWEB_CONTAINER_NAME=accweb-mcp

ACCSERVER_HOST_PATH=/opt/accweb-mcp/accserver
ACCWEB_ACC_SERVER_PATH=/accserver/server
ACCWEB_ACC_SERVER_EXE=accServer.exe

ACCWEB_HTTP_PORT=8080
ACC_LAN_PORT=8999
ACC_UDP_PORT=9231
ACC_TCP_PORT=9232

ACCWEB_ADMIN_PASSWORD=change-this-admin
ACCWEB_MOD_PASSWORD=change-this-mod
ACCWEB_RO_PASSWORD=change-this-readonly

ACCWEB_MCP_ENABLED=true
ACCWEB_MCP_TOKEN=change-this-long-random-token
ACCWEB_MCP_ALLOWED_ORIGINS=
```

## Как выбирать значения

* Для `ACCWEB_MCP_TOKEN` используйте длинное случайное значение.
* Для публичного сервера не оставляйте `ACCWEB_CORS=*`, если web UI доступен через конкретный домен.
* Если MCP доступен из браузерного клиента, задайте `ACCWEB_MCP_ALLOWED_ORIGINS`.
* Если ACC server ports меняются в настройках инстанса, синхронизируйте `ACC_LAN_PORT`, `ACC_UDP_PORT`, `ACC_TCP_PORT`.
* Если запускаете несколько ACC инстансов, каждому набору портов нужен свой опубликованный порт в Compose.
