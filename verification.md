# Проверка проекта

Дата: 23 ноября 2026 года. Локально: Go 1.23.3, PostgreSQL 15.10, psql 17.12.

- Сборка сервиса — успешно.
- `TEST_POSTGRES=1 go test -race -v ./...` — успешно, оба теста пройдены, гонок не обнаружено.
- `go vet ./...` — успешно.
- `bash -n scripts/prepare.sh scripts/run.sh scripts/install-db.sh` — успешно.
- Исходный `scripts/tests.sh 2` — все проверки пройдены: POST ZIP, POST TAR, POST без type, GET ZIP с data.csv и SQL-подключение к PostgreSQL.
- После трёх штатных загрузок: 9 строк, 3 категории, сумма 1800.00.
- Git blob SHA штатного tests.sh: `92f8fdd582ddecba42f96f0e7733e6ec24ecd36f` — совпадает с шаблоном.

Сервис развёрнут на учебной ВМ. Результаты CI и удалённой проверки приведены ниже.

## CI и удалённая проверка

Штатный Go Test Pipeline №2 завершился успешно:
https://github.com/ssstepashka13/itmo-devops-sem1-project-template/actions/runs/45179031263
По журналу исходного tests.sh полностью пройдены уровни 1 и 2. Уровень 3 не реализован и не заявляется: его необязательная проверка ожидаемо отклоняет ответ, а итоговый job проходит по выбранному уровню 2.

Учебная ВМ cse-linux: `89.169.130.141`, SSH-пользователь `ubuntu`.

Реальный SSH-деплой через `scripts/run.sh` успешно выполнен из GitHub Actions:
https://github.com/ssstepashka13/itmo-devops-sem1-project-template/actions/runs/45185548518

На ВМ установлен PostgreSQL 16, созданы роль и БД, схема инициализирована. Сервис `sem1-prices.service` включён в автозапуск systemd; `/healthz` успешно проверен после запуска.

В отдельный workflow Deploy training VM добавлен запуск неизменённого `scripts/tests.sh 2` непосредственно на ВМ. Секреты хранятся в GitHub Secrets; приватный ключ и пароль не включены в исходный код. Штатные `tests.sh` и `go_check.yaml` не изменены.

Проверка продвинутого уровня на реальной ВМ завершилась успешно:
https://github.com/ssstepashka13/itmo-devops-sem1-project-template/actions/runs/45185806237

Исходный tests.sh 2 подтвердил POST ZIP, POST TAR, POST без type, GET с data.csv и SQL-запрос. После проверки в БД: 9 строк, 3 категории, сумма 1800.00.
