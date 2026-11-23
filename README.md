# Финальный проект первого семестра — продвинутый уровень

Светлана Степанова. HTTP API для загрузки цен из ZIP/TAR и выгрузки данных PostgreSQL в ZIP.

## Требования

- Go 1.23.3 или новее, Bash, curl, zip, unzip, tar, клиент PostgreSQL.
- PostgreSQL 15+ на порту 5432; приложение слушает 8080.
- Для удалённого запуска: учебная ВМ `cse-linux` с Ubuntu/Debian, SSH-ключ и пользователь с `sudo -n`. ВМ создаётся в сервисе «Учебные окружения» по ссылке из задания. На локальной машине нужен SSH-клиент; исходящий SSH-доступ к ВМ должен быть разрешён.

База: `project-sem-1`, пользователь: `validator`, таблица: `prices`. Обязательная переменная `POSTGRES_PASSWORD` содержит учебный пароль из задания. По умолчанию подключение идёт к `localhost:5432`; переменные `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_SSLMODE` позволяют переопределить параметры. Для удалённого подключения PostgreSQL следует включать TLS; учебный деплой использует только локальное соединение.

## Подготовка и локальная проверка

В отдельном учебном Ubuntu/Debian окружении установить и запустить PostgreSQL, создать роль и базу:

```bash
read -rsp "Учебный пароль БД: " POSTGRES_PASSWORD; echo
export POSTGRES_PASSWORD
bash scripts/install-db.sh
```

Скрипт устанавливает отсутствующие PostgreSQL/клиент/curl через apt, запускает PostgreSQL и создаёт отсутствующие роль и базу. Не удаляет существующие данные и не открывает порт БД наружу. Требуются права root или sudo без запроса пароля. Если PostgreSQL уже подготовлен (например, в GitHub Actions), этот шаг не нужен.

```bash
chmod +x scripts/*.sh
./scripts/prepare.sh
./scripts/run.sh --local
./scripts/tests.sh 2
```

`prepare.sh` скачивает Go-модули, собирает `bin/prices` и создаёт отсутствующую таблицу в подготовленной БД. Приложение также проверяет наличие таблицы при запуске. Переменная `GO_BIN` задаёт путь к Go, если команда `go` не находится в PATH.

Локальный запуск работает в фоне. PID и журнал: `.run/prices.pid`, `.run/prices.log`. Остановка:

```bash
kill "$(cat .run/prices.pid)"
```

## Удалённый деплой

```bash
read -rsp "Учебный пароль БД: " POSTGRES_PASSWORD; echo
export POSTGRES_PASSWORD
export DEPLOY_USER=student  # заменить на SSH-пользователя из карточки учебной ВМ
export DEPLOY_HOST=VM_IP
export DEPLOY_SSH_KEY=/absolute/path/to/private_key
./scripts/run.sh
```

Вместо двух последних переменных допускаются аргументы:

```bash
DEPLOY_USER=student ./scripts/run.sh VM_IP /absolute/path/to/private_key
```

`run.sh` собирает Linux/amd64 бинарный файл без CGO, передаёт его и `install-db.sh` через SSH, подготавливает PostgreSQL на ВМ и устанавливает systemd-сервис `sem1-prices`. Пароль передаётся через SSH в защищённый файл `service.env` с правами 600 и подключается через systemd EnvironmentFile. Каталог на ВМ: `$HOME/sem1-prices`. Готовность проверяется через `/healthz`; только после успешного старта в stdout возвращается адрес ВМ. Диагностика выводится в stderr. Повторный деплой сохраняет таблицу и данные, обновляет бинарный файл и перезапускает сервис. По умолчанию новые SSH host keys сохраняются с `accept-new`; изменившийся ключ не принимается автоматически.

В GitHub Actions `run.sh` автоматически запускает локальный режим: встроенные автотесты проверяют API и PostgreSQL в runner, а не учебную ВМ. SSH-ключи, адреса и локальные `.env` не включаются в репозиторий.

Просмотр статуса на ВМ:

```bash
sudo systemctl status sem1-prices
sudo journalctl -u sem1-prices -n 50 --no-pager
```

## API

### POST /api/v0/prices?type=zip

`type`: `zip` (по умолчанию) или `tar`. Принимаются как архив непосредственно в теле запроса, так и multipart/form-data с полем `file` (формат штатных автотестов).

```bash
curl --fail -F 'file=@sample_data.zip' http://localhost:8080/api/v0/prices
curl --fail --data-binary @sample_data.zip http://localhost:8080/api/v0/prices?type=zip
```

Архив должен содержать один CSV. Допускаются `data.csv`, `test_data.csv` и файл внутри каталога; архив не распаковывается на диск. Поля CSV определяются по заголовку:

```csv
id,name,category,price,create_date
1,"Кофе, зерно",Напитки,12.34,2024-01-01
```

Цена хранится в `NUMERIC(18,2)` и передаётся в PostgreSQL строкой, без потерь из-за float64. Дата имеет формат `YYYY-MM-DD`. Поддерживаются CSV-кавычки и UTF-8. Ограничение архива — 32 МиБ, распакованного CSV — 128 МиБ.

Ответ HTTP 200:

```json
{"total_items":2,"total_categories":1,"total_price":13.35}
```

`total_items` — число строк, добавленных текущим запросом; `total_categories` — число различных категорий в БД после добавления; `total_price` — сумма цен всех записей в БД после добавления. Повторный импорт добавляет строки повторно: дедупликация для продвинутого уровня не требуется. `id` продукта не является уникальным ключом таблицы; технический `row_id` сохраняет порядок добавления.

Неподдерживаемый тип/повреждённый архив/некорректный CSV возвращает 400, превышение размера HTTP-тела — 413, сбой БД — 500. Пакет вставляется в одной транзакции: при ошибке частичная загрузка не сохраняется. Заголовок с пустым набором данных допустим.

### GET /api/v0/prices

Возвращает все записи в ZIP с единственным `data.csv`, заголовком и порядком колонок `id,name,category,price,create_date`. В продвинутом уровне фильтрация не применяется.

```bash
curl --fail http://localhost:8080/api/v0/prices -o prices.zip
unzip -p prices.zip data.csv
```

### GET /healthz

200 при доступной БД, 503 при недоступной. Остальные методы `/api/v0/prices` возвращают 405.

## Проверки

Штатные `scripts/tests.sh` и `.github/workflows/go_check.yaml` сохранены из шаблона без изменения содержимого. Команда выбранного уровня:

```bash
./scripts/tests.sh 2
```

Дополнительные проверки:

```bash
go test ./...
# POSTGRES_PASSWORD должен быть установлен
TEST_POSTGRES=1 go test -race -v ./...
go vet ./...
bash -n scripts/prepare.sh scripts/run.sh scripts/install-db.sh
```

Интеграционный Go-тест создаёт отдельную временную схему, проверяет API, точные суммы, повторные загрузки, round-trip ZIP/CSV и отсутствие частичной записи при ошибке; после выполнения удаляет только свою схему. Для него нужны права CREATE SCHEMA в учебной БД. Без `TEST_POSTGRES=1` этот тест пропускается.

Локальные результаты проверки — `verification.md`. Проверка реального SSH-деплоя фиксируется отдельно после настройки учебной ВМ.

## Контакт

Светлана Степанова — GitHub `ssstepashka13`.
