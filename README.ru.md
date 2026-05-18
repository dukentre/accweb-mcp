# ACCWeb MCP

ACCWeb MCP - это форк [assetto-corsa-web/accweb](https://github.com/assetto-corsa-web/accweb) для управления серверами Assetto Corsa Competizione через web-интерфейс, Docker Compose и MCP HTTP endpoint.

Главная идея форка: сохранить удобный ACCWeb, но сделать его нормальным self-hosted сервисом, который можно поднять готовым Docker-образом и подключить к агентам через MCP.

## Что добавлено

* готовый Docker-образ `ghcr.io/dukentre/accweb-mcp`
* production `docker-compose.yml` без локального билда
* `systemd` unit для запуска Compose-стека как сервиса
* ручное подключение файлов ACC Dedicated Server с хоста
* MCP endpoint `POST /mcp` с авторизацией по bearer-токену
* MCP resources со списком параметров ACC и описаниями
* MCP prompts для типовых задач
* MCP tools для создания, чтения, изменения, старта и остановки инстансов

## Как это устроено

Контейнер содержит ACCWeb и Wine. Сам ACC Dedicated Server внутрь образа не кладется: его нужно скачать или установить руками на хосте и примонтировать в контейнер read-only.

Такой подход проще и надежнее для Steam Guard: контейнеру не нужны Steam-логин, пароль, guard-код и временные env-переменные. Серверные файлы лежат в понятной папке, а ACCWeb просто запускает `accServer.exe` через Wine.

Базовая схема:

```text
host /opt/accweb-mcp/accserver  ->  container /accserver
host docker volume accweb-config ->  container /accweb/config
host docker volume accweb-secrets -> container /accweb/secrets
```

Ожидаемый layout ACC server files:

```text
/opt/accweb-mcp/accserver/server/accServer.exe
```

## Установка без локального билда

Требования:

* Linux-сервер
* Docker Engine
* Docker Compose plugin
* файлы ACC Dedicated Server, вручную положенные на сервер

Создать директорию:

```sh
sudo install -d -m 0755 /opt/accweb-mcp/accserver
```

Положить файлы ACC Dedicated Server так, чтобы существовал файл:

```text
/opt/accweb-mcp/accserver/server/accServer.exe
```

Скачать production Compose и env example:

```sh
sudo curl -fsSL -o /opt/accweb-mcp/docker-compose.yml \
  https://raw.githubusercontent.com/dukentre/accweb-mcp/master/deploy/docker-compose.yml
sudo curl -fsSL -o /opt/accweb-mcp/.env \
  https://raw.githubusercontent.com/dukentre/accweb-mcp/master/deploy/.env.example
```

Отредактировать `/opt/accweb-mcp/.env`. Минимум нужно заменить:

```env
ACCWEB_ADMIN_PASSWORD=...
ACCWEB_MOD_PASSWORD=...
ACCWEB_RO_PASSWORD=...
ACCWEB_MCP_TOKEN=...
```

Запустить:

```sh
sudo docker compose --env-file /opt/accweb-mcp/.env \
  -f /opt/accweb-mcp/docker-compose.yml pull
sudo docker compose --env-file /opt/accweb-mcp/.env \
  -f /opt/accweb-mcp/docker-compose.yml up -d
```

Открыть:

```text
http://SERVER_IP:8080
```

## Установка как systemd service

```sh
sudo curl -fsSL -o /etc/systemd/system/accweb-mcp.service \
  https://raw.githubusercontent.com/dukentre/accweb-mcp/master/deploy/systemd/accweb-mcp.service
sudo systemctl daemon-reload
sudo systemctl enable --now accweb-mcp
```

Команды:

```sh
sudo systemctl status accweb-mcp
sudo systemctl reload accweb-mcp
sudo systemctl stop accweb-mcp
sudo journalctl -u accweb-mcp -n 100
```

`reload` делает `docker compose pull` и пересоздает контейнер.

## MCP сервер

MCP сервер нужен, чтобы агент или MCP-клиент мог работать с ACCWeb не как с web-страницей, а как со структурированным API:

* узнать все доступные параметры ACC и их смысл
* прочитать список инстансов
* получить полный конфиг конкретного инстанса
* изменить параметры через ACCWeb
* стартовать и останавливать инстансы
* создать быстрый Q/R сервер

Endpoint:

```text
POST http://SERVER_IP:8080/mcp
```

Заголовки:

```http
Authorization: Bearer <ACCWEB_MCP_TOKEN>
Content-Type: application/json
Accept: application/json, text/event-stream
MCP-Protocol-Version: 2025-06-18
```

MCP resources:

* `accweb://parameters` - справочник ACC параметров
* `accweb://tracks` - каталог трасс ACC с id, названиями, странами и aliases
* `accweb://instances` - список инстансов
* `accweb://instances/{id}/status` - статус инстанса
* `accweb://instances/{id}/weather` - погода инстанса
* `accweb://instances/{id}/config` - fallback-конфиг с замаскированными секретами

MCP prompts:

* `acc_server_overview`
* `acc_weather_answer`
* `acc_race_setup_summary`
* `acc_config_explain`
* `configure_quick_race`
* `explain_parameter`

MCP tools:

* `list_tracks`
* `list_instances`
* `get_instance_status`
* `get_instance_weather`
* `get_instance_track`
* `get_instance_config`
* `set_instance_parameters`
* `start_instance`
* `stop_instance`
* `delete_instance`
* `create_quick_race_instance`

`delete_instance` помечен как destructive tool и требует явный `instanceIdOrName`; default instance fallback для удаления не используется.

Read-only tools отдают `annotations.readOnlyHint: true`, `idempotentHint: true`, `openWorldHint: false` и `structuredContent`, поэтому совместимый MCP-клиент может не просить подтверждение для вопросов про карты/трассы, погоду и статус.

Для вопросов "какие карты/трассы есть в ACC?" используйте общий каталог `accweb://tracks` или tool `list_tracks`. Это именно список трасс, не car groups и не машины. Для `configure_quick_race.track` и `accweb://tracks/{trackId}` включен MCP `completion/complete`.

Пример запроса:

```sh
curl -s http://SERVER_IP:8080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H 'MCP-Protocol-Version: 2025-06-18' \
  -H "Authorization: Bearer $ACCWEB_MCP_TOKEN" \
  -d '{"jsonrpc":"2.0","id":1,"method":"resources/list","params":{}}'
```

## Погода, время и сессии через MCP

Для вопроса "какая погода?" используйте read-only tool `get_instance_weather`. Для изменения погоды значения лежат в `event.json`:

```text
acc.event.ambientTemp
acc.event.trackTemp
acc.event.cloudLevel
acc.event.rain
acc.event.weatherRandomness
```

Время суток задается по каждой сессии:

```text
acc.event.sessions[0].hourOfDay
acc.event.sessions[0].timeMultiplier
```

Можно оставить одну сессию или задать несколько. Для добавления и удаления сессий удобнее заменить весь массив:

```text
acc.event.sessions
```

Подробные MCP-примеры есть в [docs/mcp.md](docs/mcp.md).

## Env-переменные

Главные переменные:

* `ACCWEB_IMAGE` - Docker image, обычно `ghcr.io/dukentre/accweb-mcp:latest`
* `ACCSERVER_HOST_PATH` - путь на хосте, где лежат файлы ACC Dedicated Server
* `ACCWEB_ACC_SERVER_PATH` - путь внутри контейнера, откуда ACCWeb запускает сервер
* `ACCWEB_HTTP_PORT` - внешний порт web-интерфейса и MCP
* `ACC_LAN_PORT`, `ACC_UDP_PORT`, `ACC_TCP_PORT` - опубликованные порты ACC сервера
* `ACCWEB_ADMIN_PASSWORD`, `ACCWEB_MOD_PASSWORD`, `ACCWEB_RO_PASSWORD` - пароли ролей ACCWeb
* `ACCWEB_MCP_TOKEN` - токен для MCP
* `ACCWEB_MCP_ALLOWED_ORIGINS` - whitelist browser origins для MCP

Полный справочник env-переменных: [docs/env.ru.md](docs/env.ru.md).

## Обновление

С systemd:

```sh
sudo systemctl reload accweb-mcp
```

Без systemd:

```sh
sudo docker compose --env-file /opt/accweb-mcp/.env \
  -f /opt/accweb-mcp/docker-compose.yml pull
sudo docker compose --env-file /opt/accweb-mcp/.env \
  -f /opt/accweb-mcp/docker-compose.yml up -d
```

## Локальная разработка

Root `docker-compose.yml` предназначен для разработки и билдит образ из текущего checkout:

```sh
cp .env.example .env
mkdir -p accserver
# положить ACC Dedicated Server в ./accserver
docker compose up -d --build
```

Для production-установки использовать `deploy/docker-compose.yml`.

## Ссылки

* [Репозиторий ACCWeb MCP](https://github.com/dukentre/accweb-mcp)
* [Docker image в GHCR](https://github.com/dukentre/accweb-mcp/pkgs/container/accweb-mcp)
* [Upstream ACCWeb](https://github.com/assetto-corsa-web/accweb)

## License

MIT
