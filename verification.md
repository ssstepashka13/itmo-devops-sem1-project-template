# Проверка проекта

Дата: 23 ноября 2026 года. Локально: Go 1.23.3, PostgreSQL 15.10, psql 17.12.

- Сборка сервиса — успешно.
- `TEST_POSTGRES=1 go test -race -v ./...` — успешно, оба теста пройдены, гонок не обнаружено.
- `go vet ./...` — успешно.
- `bash -n scripts/prepare.sh scripts/run.sh scripts/install-db.sh` — успешно.
- Исходный `scripts/tests.sh 2` — все проверки пройдены: POST ZIP, POST TAR, POST без type, GET ZIP с data.csv и SQL-подключение к PostgreSQL.
- После трёх штатных загрузок: 9 строк, 3 категории, сумма 1800.00.
- Git blob SHA штатного tests.sh: `92f8fdd582ddecba42f96f0e7733e6ec24ecd36f` — совпадает с шаблоном.

Удалённый деплой на учебную ВМ и запуск CI ещё не подтверждены; эти проверки должны быть завершены до сдачи проекта.
