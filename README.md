# TripGo — сервис поездок

HTTP-сервис на Go для создания, получения и завершения поездок. Данные хранятся
в PostgreSQL. Сделаны лабораторные работы:

- [№1](https://github.com/course-go-autumn-2026/course/blob/main/homework/tasks/01-http-and-postgres/README.md):
  HTTP API, PostgreSQL, менеджер транзакций, идемпотентность и Docker-сборка;
- [№2](https://github.com/course-go-autumn-2026/course/blob/main/homework/tasks/02-architecture-and-observability/README.md):
  слои, тесты, линтер, логи, метрики и трейсы через OpenTelemetry;
- [№3](https://github.com/course-go-autumn-2026/course/blob/main/homework/tasks/03-positions-and-worker-pool/README.md):
  координаты поездки и фоновый опрос устаревших поездок через очередь и пул воркеров.

Сделаны и задания со звёздочкой: мутационное тестирование (ЛР2), выборка батчами и
приоритетная очередь (ЛР3).

## Требования

- Go 1.27.1 или новее.
- Git, Make и curl.
- Запущенный Docker (на macOS — Docker Desktop или Colima).
- Утилита курса [tripgoctl](https://github.com/course-go-autumn-2026/course-infra).
- [golangci-lint](https://golangci-lint.run/) v2 для `make lint`.

PostgreSQL, коллектор телеметрии, Jaeger, Prometheus, Grafana и заглушку Push
Service поднимает `tripgoctl`. Версии `goose`, `oapi-codegen` и `mockgen` закреплены в `go.mod`,
команды Makefile запускают их через `go tool`.

## Запуск

```sh
git clone https://github.com/KirillKrainov22/tripgo.git
cd tripgo
tripgoctl cluster start
tripgoctl environment start
tripgoctl connect
make migrate
make run
```

`tripgoctl environment start` поднимает компоненты из `environment.toml` и
создаёт `.env` с адресами базы, коллектора и Push Service. `make run` загружает сначала
`.env.example`, затем `.env`, поэтому адреса берутся из запущенного окружения.
`tripgoctl connect` печатает ссылки на Jaeger, Grafana и Prometheus.

Проверка во втором терминале:

```sh
curl -i http://localhost:8080/health
curl -i http://localhost:8080/ready
```

Оба запроса должны вернуть `200` и `{"status":"ok"}`. Остановка сервиса —
`Ctrl+C`, остановка окружения — `tripgoctl environment stop`.

## API

| Метод и путь | Назначение |
|---|---|
| `POST /api/v1/trips` | Создать поездку |
| `GET /api/v1/trips/{tripId}` | Получить поездку по ID |
| `POST /api/v1/trips/{tripId}/finish` | Завершить поездку |
| `POST /api/v1/trips/{tripId}/positions` | Сохранить координату поездки |
| `GET /api/v1/trips/{tripId}/positions` | Получить маршрут поездки |
| `GET /health` | Проверить работу HTTP-сервера |
| `GET /ready` | Проверить доступность PostgreSQL |

Форматы запросов и ответов заданы в [OpenAPI-контракте](contracts/openapi/trip-service.openapi.yaml).
Ошибки возвращаются в `application/problem+json`: поле `code` стабильное, в
`detail` написано, что именно не так.

```sh
curl -i -X POST http://localhost:8080/api/v1/trips \
  -H 'Content-Type: application/json' \
  --data '{"user_id":"5cb72c04-7650-45c9-a79b-bcdba0631e0c","driver_id":"8860b315-ec86-42eb-a17c-7c163d721ff5","start_point":{"latitude":59.9398,"longitude":30.3146},"end_point":{"latitude":59.929,"longitude":30.3626},"price":1450}'
```

Ожидается `201` и заголовок `Location`. Получение — `200`, первое завершение —
`200`, повторное — `409 trip_completed`. Вторая активная поездка у водителя —
`409 driver_busy`, неверные данные — `400 invalid_request`, неизвестный ID —
`404 trip_not_found`, внутренняя ошибка — `500 internal_error` без подробностей.

```sh
curl -i -X POST http://localhost:8080/api/v1/trips/$TRIP_ID/positions \
  -H 'Content-Type: application/json' \
  --data "{\"coordinates\":{\"latitude\":59.935,\"longitude\":30.335},\"recorded_at\":\"$(date -u +%FT%TZ)\"}"
curl -i http://localhost:8080/api/v1/trips/$TRIP_ID/positions
```

Сохранение координаты отвечает `201`, в завершённую поездку — `409 trip_completed`,
координаты вне диапазона или `recorded_at` дальше чем на минуту в будущем —
`400 invalid_request`. Маршрут отсортирован по `recorded_at`, затем по `id`.

## Настройки

Все настройки обязательны и берутся из окружения. Значения для запуска — в
[.env.example](.env.example), адреса базы и коллектора `tripgoctl` пишет в `.env`.

| Переменная | Значение в примере | Назначение |
|---|---|---|
| `HTTP_ADDR` | `:8080` | Адрес HTTP-сервера |
| `HTTP_READ_TIMEOUT` | `5s` | Время чтения запроса |
| `HTTP_READ_HEADER_TIMEOUT` | `2s` | Время чтения заголовков |
| `HTTP_WRITE_TIMEOUT` | `10s` | Время записи ответа |
| `HTTP_IDLE_TIMEOUT` | `60s` | Время ожидания следующего запроса в соединении |
| `LOG_LEVEL` | `info` | Уровень логирования |
| `SHUTDOWN_TIMEOUT` | `10s` | Общее время на остановку |
| `DATABASE_URL` | Из `.env` | Строка подключения к PostgreSQL |
| `DATABASE_MAX_CONNS` | `10` | Максимальное число соединений с БД |
| `DATABASE_MIN_CONNS` | `2` | Минимальное число соединений с БД |
| `DATABASE_CONNECT_TIMEOUT` | `5s` | Время подключения и проверки БД при запуске |
| `DATABASE_QUERY_TIMEOUT` | `3s` | Время одного SQL-запроса |
| `DATABASE_TX_TIMEOUT` | `8s` | Время всей транзакции `Do` |
| `DATABASE_MAX_CONN_LIFETIME` | `30m` | Время жизни соединения с БД |
| `IDEMPOTENCY_TTL` | `24h` | Срок действия ключа повторного запроса |
| `IDEMPOTENCY_CLEANUP_INTERVAL` | `1h` | Период удаления просроченных ключей |
| `POSITION_CHECK_INTERVAL` | `30s` | Период тикера сканера и пауза между повторными пушами |
| `POSITION_STALE_AFTER` | `60s` | Сколько поездка может молчать, прежде чем её опросят |
| `POSITION_WORKERS` | `4` | Число воркеров |
| `POSITION_QUEUE_SIZE` | `100` | Ёмкость очереди |
| `POSITION_SCAN_BATCH_SIZE` | `100` | Сколько поездок сканер читает за один запрос |
| `PUSH_HTTP_URL` | Из `.env` | Адрес Push Service |
| `PUSH_REQUEST_TIMEOUT` | `1s` | Таймаут одного запроса в Push Service |

Телеметрия настраивается стандартными переменными OpenTelemetry, SDK читает их
сам: `OTEL_SERVICE_NAME`, `OTEL_EXPORTER_OTLP_ENDPOINT`,
`OTEL_EXPORTER_OTLP_PROTOCOL`, `OTEL_EXPORTER_OTLP_TIMEOUT`,
`OTEL_RESOURCE_ATTRIBUTES`, `OTEL_TRACES_SAMPLER`.

## Сборка и проверки

| Команда | Что делает |
|---|---|
| `make build` | Собирает программу в `bin/trip-service` |
| `make lint` | Запускает `golangci-lint` с конфигом курса |
| `make test` | Unit-тесты с `-race`, печатает покрытие `internal/usecase` и `internal/domain` |
| `make test-integration` | Интеграционные тесты репозиториев на базе из `tripgoctl` |
| `make mutation` | Мутационное тестирование `internal/domain` и `internal/usecase` |
| `make generate` | Генерирует код по OpenAPI и моки |
| `make migrate` | Применяет миграции |
| `make migrate-down` | Откатывает последнюю миграцию |

Интеграционные тесты помечены тегом `integration` и работают с базой из
`tripgoctl environment start`, поэтому перед ними нужны запущенное окружение и
`make migrate`. Каждый тест сам создаёт и удаляет свои данные и проходит при
повторном запуске.

Отчёт мутационного тестирования (доп. задание ЛР2) с разбором выживших мутантов —
в [docs/mutation-testing.md](docs/mutation-testing.md).

## Архитектура

```text
http (ручки, middleware) ──► usecase ──► domain
repository ─────────────────────────────► domain
infra: пул pgx, менеджер транзакций, телеметрия
container собирает всё; main читает конфиг, запускает и ждёт сигнала
```

| Пакет | Что внутри |
|---|---|
| `internal/domain/trip` | Поездка, статусы, переход `Finish`, проверка данных, доменные ошибки |
| `internal/model` | Структуры, которые ходят между слоями |
| `internal/usecase/trip/{create,get,finish}` | Бизнес-действия, по пакету на действие |
| `internal/http/trip/{create,get,finish}` | Ручки, по пакету на ручку |
| `internal/http/domainerror` | Перевод доменной ошибки в `Problem`, одно место |
| `internal/http/middleware` | Recover, Tracing, Logging, Metrics |
| `internal/http/router` | Роутер chi и сгенерированный по OpenAPI интерфейс |
| `internal/repository/{trip,idempotency}` | SQL на squirrel |
| `internal/infra/postgres` | Пул pgx с трейсами и метриками запросов |
| `internal/infra/postgres/transaction` | Менеджер транзакций |
| `internal/infra/telemetry` | OpenTelemetry SDK, логгер, бизнес-метрики |
| `internal/worker/idempotency` | Фоновая очистка просроченных ключей |
| `internal/worker/positions` | Сканер, очередь и пул воркеров опроса поездок |
| `internal/gateway/push` | HTTP-клиент Push Service |
| `internal/container` | Создание и связывание всех зависимостей, порядок остановки |

**Зависимости направлены внутрь.** Ручки знают про usecase, usecase — про домен,
домен ни от кого не зависит. Правило закреплено `depguard` в `.golangci.yml`:
`domain` не импортирует `net/http`, `chi`, `pgx`, `squirrel` и `otel`; `usecase`
не импортирует транспорт и драйвер бд; `http` не импортирует репозитории и `infra`.

**Интерфейсы объявляет потребитель.** У каждого usecase и каждой ручки есть
`contract.go` с маленькими неэкспортируемыми интерфейсами того, что им нужно:
usecase создания знает только про `Create` и `RecordStatus` репозитория, а не про
весь репозиторий. Моки генерирует `mockgen` в `mocks_test.go` рядом с кодом.
Когда появится второй транспорт (gRPC или консьюмер в работе 4), он вызовет те
же usecase: их интерфейсы не зависят от HTTP, менять usecase не придётся.

**Ошибки.** Доменные ошибки объявлены в `domain/trip/errors.go`, ошибка
валидации — отдельный тип `ValidationError` с причиной. Ручка не разбирает ошибку
сама, а отдаёт её в `domainerror.Problem`: там `errors.Is` / `errors.As`
переводят её в HTTP-код и `code`. Всё неизвестное становится `500 internal_error`,
текст ошибки попадает только в лог.

**Завершение поездки.** Переход статуса описан в домене: `Trip.Finish` вернёт
`ErrTripCompleted`, если поездка уже не активна. Usecase в одной транзакции
читает поездку с блокировкой строки (`SELECT … FOR UPDATE`), вызывает `Finish`,
сохраняет результат (`UPDATE … WHERE status = 'active'`) и пишет историю. Из двух
одновременных завершений второе ждёт блокировку, затем читает уже завершённую
поездку и получает `409 trip_completed`.

## Observability

Сервис отправляет трейсы и метрики по OTLP в коллектор, оттуда они попадают в
Jaeger, Prometheus и Grafana. Провайдеры трейсов и метрик создаются в `container`
и передаются явно, глобальных провайдеров OpenTelemetry сервис не выставляет. Если коллектор недоступен, сервис работает дальше и
пишет предупреждение `telemetry export failed`.

**Логи** — `slog` в JSON в stdout. В каждой записи есть `service`, а внутри спана
ещё `trace_id` и `span_id`, их добавляет обёртка над JSON-хендлером. На каждый
запрос middleware пишет одну запись `request handled` с методом, шаблоном
маршрута, статусом и длительностью: `Info` для `2xx` и `4xx`, `Error` для `5xx`.
Текст внутренней ошибки попадает в эту же запись в поле `error`, отдельно ошибка
не логируется.
Создание и завершение поездки логируются отдельно в usecase.

**Трейсы.** `otelhttp` создаёт корневой спан с именем `{method} {route}`,
например `POST /api/v1/trips`. Запросы в бд трейсятся на уровне пула через
`otelpgx`, к спанам добавляются `db.operation.name` и `db.collection.name`.
Внутренняя ошибка помечает спан `codes.Error` и пишет `error.type`.

**Метрики:**

| Метрика | Тип | Лейблы |
|---|---|---|
| `http.server.request.duration` | histogram, секунды | `http.request.method`, `http.route`, `http.response.status_code` |
| `http.server.active_requests` | up-down counter | `http.request.method`, `http.route` |
| `db.client.operation.duration` | histogram, секунды | `db.operation.name`, `db.collection.name`, `error.type` |
| `tripgo.trips.created` | counter | — |
| `tripgo.trips.completed` | counter | — |
| `tripgo.trips.active` | gauge | — |
| `tripgo.positions.saved` | counter | — |
| `tripgo.stale_checks` | counter | `result` = `queued`, `duplicate`, `dropped`, `cancelled` |
| `tripgo.position_requests` | counter | `result` = `success`, `error`, `dropped` |
| `tripgo.position_requests.duration` | histogram, секунды | `result` |
| `tripgo.position_requests.queue.size` | gauge | — |
| `tripgo.position_requests.workers.active` | gauge | — |
| `tripgo.stale_scan.rows` | histogram | — |

В `http.route` всегда шаблон маршрута (`/api/v1/trips/{tripId}`): если класть
туда настоящий ID, на каждую поездку появится своя временная серия, и метрика
перестанет агрегироваться. Шаблон берётся из роутера chi до обработки запроса.

**Middleware** подключены в порядке Recover → Tracing → Logging → Metrics → роутер.
У роутера есть дополнительная граница Recover: она преобразует панику обработчика
в `500 internal_error` до завершения middleware. Поэтому метрики учитывают `500`,
счётчик активных запросов уменьшается, а спан отмечается ошибкой. Стектрейс попадает
в ту же запись `request handled` с `trace_id` и `span_id`. Внешний Recover остаётся
страховкой для ошибок самих middleware. Если ответ уже начат, соединение прерывается:
дописать к нему новый HTTP-ответ нельзя.

Проверка: после запросов из раздела API открыть Jaeger из `tripgoctl connect` и
найти трейс `POST /api/v1/trips` со спанами `INSERT`; в Grafana найти
`http_server_request_duration_seconds` и `tripgo_trips_created_total`;
`trace_id` из лога ищется в Jaeger.

## Опрос устаревших поездок

```text
тикер ──► сканер ──► очередь на POSITION_QUEUE_SIZE ──► POSITION_WORKERS воркеров ──► Push Service
            │         (по времени молчания)                         │
            └── SQL батчами: активные поездки без свежих координат   └── «сейчас в очереди»: дедупликация и отмена
```

**Сканер** — одна горутина с тикером на `POSITION_CHECK_INTERVAL`. На каждом тике
он забирает из бд активные поездки, у которых `COALESCE(last_position_at,
started_at)` старше `POSITION_STALE_AFTER`; условие по времени считается в SQL,
первыми идут те, кто молчит дольше всех. Тики не накладываются: обход выполняется
в той же горутине, что читает тикер.

**Выборка батчами** (доп. задание). Запрос читает не больше
`POSITION_SCAN_BATCH_SIZE` строк, следующий батч продолжается с ключа последней
прочитанной строки — `(COALESCE(last_position_at, started_at), id) > (…)`, а не через
`OFFSET`: так при равном времени поездки различаются по `id` и ни одна не теряется.
За один тик сканер либо разбирает все батчи, либо останавливается, как только
очередь заполнилась, и на следующем тике начинает сначала. Сколько строк прочитано
за тик, показывает `tripgo.stale_scan.rows`. Под запрос есть частичный индекс
`trips_active_last_seen_id_idx` по выражению и `id`, `EXPLAIN (ANALYZE)` показывает
`Index Scan` с условием по ключу, без сортировки.

**Очередь** ограничена `POSITION_QUEUE_SIZE`, сканер ставит задачу без ожидания.
Если очередь заполнена, задача дропается, растут `tripgo.stale_checks{result="dropped"}`
и `tripgo.position_requests{result="dropped"}`, пишется лог `Warn`. Сканер никогда не
ждёт воркеров, поэтому медленный Push Service не останавливает проверку остальных
поездок, а память не растёт. Дропнутая поездка попадёт в выборку на следующем тике.

**Приоритет** (доп. задание). Очередь — куча по времени последней координаты:
воркер всегда берёт поездку, которая молчит дольше всех. Воркеров будит канал с
жетонами, по жетону на задачу в куче, поэтому воркер ждёт задачу через `select` и
так же реагирует на остановку. Если очередь заполнена, новая поездка, которая
молчит дольше, вытесняет самую свежую из очереди (для вытесненной растёт
`position_requests{result="dropped"}` и пишется `Warn`); задачи, отменённые
координатой, вытесняются первыми. Если новая поездка свежее всех в очереди, она
дропается сама.

**Дедупликация и отмена.** Структура «сейчас в очереди» — карта поездок под
мьютексом, у каждой постановки свой номер. Сканер ставит поездку, только если её
там нет, иначе `duplicate`; воркер убирает поездку после обработки. Пришедшая
координата удаляет поездку из карты: задача, которая уже лежит в очереди, при
обработке отбрасывается, потому что её номера в карте больше нет. Карта и куча
защищены одним мьютексом.

До конца текущего обхода сканер также запоминает отменённые поездки: старый
результат SQL не может вернуть их в очередь. Это работает и между батчами:
запоздалая координата может сдвинуть поездку за ключ продолжения, но повторного
push в этом обходе не будет. Такие постановки получают
`stale_checks{result="cancelled"}`. Временный набор ограничен
`POSITION_SCAN_BATCH_SIZE`; если отмен больше, сканер пишет `Warn` и заканчивает
обход. Затем набор очищается, и следующий тик снова читает актуальное состояние
из БД.

**Воркеры** — `POSITION_WORKERS` горутин, запускаются при старте и живут до
остановки, горутину на задачу не создаём. Каждая задача обрабатывается с
таймаутом `PUSH_REQUEST_TIMEOUT`; ошибка Push Service пишется в лог `Warn` и в
метрику, воркер берёт следующую задачу. Спан обработки связан со спаном проверки
через span link, вызов Push Service — дочерний спан.

**Сохранение координаты** в одной транзакции обновляет
`last_position_at = GREATEST(last_position_at, recorded_at)` только у активной
поездки и вставляет точку в `trip_positions`. Сравнение делает сам `UPDATE`, поэтому
опоздавшая старая координата не «состарит» поездку.

**Остановка** идёт по порядку: HTTP-сервер дожидается текущих запросов, сканер
останавливается, воркеры дорабатывают то, что уже в очереди, в пределах
`SHUTDOWN_TIMEOUT`, затем закрываются HTTP-клиент, телеметрия и пул бд. Если сначала
остановить воркеров, сканер продолжал бы класть задачи в очередь, которую никто не
читает; если сначала закрыть пул бд, сканер и ручки получали бы ошибки посреди работы.

**Сознательное упрощение:** время отправленной команды не хранится, поэтому период
тикера и есть пауза между повторными пушами. В проде хранили бы время последней
отправки и развели бы эти два интервала.

Проверка: создать поездку и не слать координаты — через `POSITION_STALE_AFTER` в
`tripgoctl environment logs push-service` появится `REQUEST_POSITION`, не чаще раза
за `POSITION_CHECK_INTERVAL`; если слать координаты каждые 5 секунд, команд нет.
Задержку заглушки можно включить через админку из `tripgoctl connect`
(`/admin/behaviour`, `{"latency_ms":5000}`) — ручки отвечают быстро, вызовы
обрываются по `PUSH_REQUEST_TIMEOUT`, сканер продолжает тикать.

## Решения

**Работа с БД.** Подключения переиспользуются через `pgxpool`, пул создаётся в
`container` и передаётся явно. SQL строится через `squirrel`, значения передаются
параметрами `$1`, `$2` и далее.

**Транзакции.** `transaction.Manager.Do` открывает транзакцию с уровнем
Read Committed и кладёт её в контекст. Репозиторий берёт исполнителя через
`transaction.ExecutorFrom`: внутри `Do` — транзакцию, вне — пул. Ошибка или паника
приводят к `ROLLBACK`, вложенный `Do` использует ту же транзакцию, а его ошибка не
даёт закоммитить изменения. На всю транзакцию действует `DATABASE_TX_TIMEOUT`, на
каждый запрос — `DATABASE_QUERY_TIMEOUT`.

**Уровень изоляции — Read Committed.** Его достаточно: создание защищено
уникальным индексом, завершение — блокировкой строки и условием в `UPDATE`.

**Один водитель — одна активная поездка.** Частичный уникальный индекс
`trips_driver_active_uidx` запрещает повтор `driver_id` среди активных поездок, в
том числе при одновременных запросах. Нарушение (`23505`) превращается в
`409 driver_busy`.

**Проверка состояния и остановка.** `/health` не обращается к бд, `/ready`
проверяет её через `Ping`. По `SIGINT` или `SIGTERM` сервис перестаёт принимать
запросы и ждёт текущие, останавливает фоновые компоненты, выгружает телеметрию и
закрывает пул. Если `SHUTDOWN_TIMEOUT` истёк, запросы отменяются и в лог пишется
ошибка.

## Дополнительное задание ЛР1: идемпотентность

Клиент передаёт UUID в заголовке `Idempotency-Key`. Первый запрос создаёт поездку
и возвращает `201`, повтор с тем же ключом и данными — первоначальный ответ и
`200`, тот же ключ с другими данными — `409 idempotency_conflict`. Ключ хранится в
`trip_idempotency_keys` в одной транзакции с поездкой. Просроченные ключи удаляет
фоновая очистка при запуске и затем каждые `IDEMPOTENCY_CLEANUP_INTERVAL`,
пачками по 1000 с `SKIP LOCKED`.

## Дополнительное задание ЛР1: Docker

```sh
make docker-build
make docker-run DOCKER_PORT=8081
```

Сборка в `deploy/Dockerfile` двухэтапная: программа компилируется в образе Go,
затем только исполняемый файл копируется в образ на `scratch`. Процесс работает от
пользователя `65532:65532`. `make docker-run` берёт адреса из `.env` и переводит
локальные адреса PostgreSQL, Push Service и OTLP-коллектора на `host.docker.internal`,
чтобы контейнер обращался к окружению на хосте. Внешние адреса остаются прежними.

## Структура проекта

```text
cmd/trip-service/     запуск: конфиг, контейнер, ожидание сигнала
internal/config/      чтение настроек
internal/container/   сборка зависимостей и остановка
internal/domain/      доменные правила и ошибки
internal/model/       структуры между слоями
internal/usecase/     бизнес-действия
internal/http/        транспорт: ручки, маппинг ошибок, middleware, роутер
internal/repository/  SQL-запросы
internal/infra/       пул бд, транзакции, телеметрия
internal/worker/      фоновые компоненты: очистка ключей и опрос поездок
internal/gateway/     клиенты внешних сервисов
internal/generated/   код, сгенерированный из OpenAPI
migrations/           SQL-миграции
contracts/            исходные контракты курса
deploy/               Dockerfile
```
