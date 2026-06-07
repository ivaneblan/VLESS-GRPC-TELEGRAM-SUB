# VLESS gRPC VPN Toolkit

Кроссплатформенный набор инструментов на **Go** для VPN-узлов **VLESS + Reality + gRPC** и управления подписками.

| Бинарник | Где запускается | Назначение |
|----------|-----------------|------------|
| **`vpnctl`** | Ваша машина | Деплой инфраструктуры, пользователи и серверы через CLI |
| **`tgbot`** (опционально) | Один VPS (`bot.server_id`) | Те же операции через Telegram |

**`vpnctl users`** и **`vpnctl servers`** покрывают всё, что умеет бот. Telegram — удобная опция для админа и самообслуживания пользователей.

## Выбор режима работы

| | Только CLI | С Telegram-ботом |
|---|------------|------------------|
| Первичная установка | `vpnctl bootstrap --no-bot` | `vpnctl bootstrap` |
| `secrets.yaml` | только пароли | + `telegram.bot_token` |
| `config.yaml` | `servers[]`, `xray.*` | + `bot.approver_user_id` |
| Добавить подписчика | `vpnctl users add ID` | заявка пользователя → одобрение в чате |
| Отправить ссылки | `vpnctl users export ID` | бот отправляет после одобрения |
| Админские операции | `vpnctl users …` / `servers …` | inline-клавиатура + те же CLI-команды |

## Быстрый старт

Нужен **Go 1.22+** (или готовый бинарник из релиза, когда появится).

```powershell
# Windows
go build -o vpnctl.exe ./cmd/vpnctl
.\vpnctl.exe init
# отредактировать config.yaml, secrets.yaml
.\vpnctl.exe bootstrap --no-bot    # или bootstrap (с ботом)
.\vpnctl.exe users add client-1 --days 30
.\vpnctl.exe users show client-1 --format happ
```

```bash
# Linux / macOS
make build    # или: go build -o vpnctl ./cmd/vpnctl
./vpnctl init
./vpnctl bootstrap --no-bot
./vpnctl users add client-1 --days 30
```

### Типовые сценарии

| Ситуация | Команда |
|----------|---------|
| Новый VPS, пользователей нет | `vpnctl bootstrap` или `bootstrap --no-bot` |
| Новый VPS, сначала очистить старый стек | `vpnctl bootstrap --cleanup` |
| Переустановка, подписчиков сохранить | `vpnctl redeploy` |
| Добавить сервер к существующей схеме | правка `config.yaml` → `vpnctl vless newid` → `vpnctl users sync ID` для каждого пользователя |
| Добавить RU-мост (обход ТСПУ) | в `config.yaml` сервер с `relay_to: <exit_id>` → `vpnctl keys` → `vpnctl vless` → `vpnctl links refresh` |

### Мост / jump-host (обход ТСПУ)

Прямое подключение к иностранному exit на домашнем/мобильном интернете в РФ деградирует: ТСПУ «замораживает» TLS-сессию к иностранному IP. Решение — цепочка через российский узел.

Узел с полем `relay_to: <exit_id>` становится **мостом**: он поднимает обычный VLESS+Reality+gRPC inbound (свои Reality-ключи, те же пользователи), а весь трафик через VLESS-outbound форвардит на указанный exit (двойной VLESS). Клиент подключается к RU-IP (первый хоп — внутри РФ), мост сам ходит к exit.

- Мост даёт **дополнительный** набор ссылок поверх прямых; прямые схемы не меняются.
- Между мостом и exit используется отдельный `relay_uuid` (хранится в `secrets.yaml`, автоматически регистрируется на exit).
- Деплой: `vpnctl vless` сначала раскатывает exit'ы, затем мосты (порядок учитывается автоматически).

## Первичная настройка

После `vpnctl init` отредактируйте:

### config.yaml (обязательно)

| Поле | Только CLI | С ботом |
|------|------------|---------|
| `servers[]` (`id`, `host`, `name`) | да | да |
| `xray.*` | значения по умолчанию подходят | то же |
| `bot.server_id` | любой id сервера (хост бота, если задеплоите позже) | id VPS с ботом |
| `bot.approver_user_id` | `0` или не заполнять | ваш числовой Telegram id |
| `bot.default_subscription_days` | используется в `users add` | то же |

### secrets.yaml (обязательно)

| Поле | Только CLI | С ботом |
|------|------------|---------|
| `servers.<id>.password` | да | да |
| `telegram.bot_token` | не нужен | обязателен для `bootstrap` / `vpnctl bot` |

### state.yaml (автоматически)

Изначально пустой. Заполняется через `vpnctl users add` или одобрения в Telegram-боте.

**Не копируйте вручную:**

| Что | Откуда берётся |
|-----|----------------|
| SSH-ключи | `vpnctl init` → `keys/` |
| Reality-ключи | `vpnctl vless` / `bootstrap` → `secrets.yaml` |
| UUID и ссылки пользователей | `vpnctl users add` или бот |

**Перед `vpnctl bot`:** локальный `state.yaml` загружается на сервер и перезаписывает `/root/ssh/state.yaml`. При необходимости сначала синхронизируйте с хоста бота.

## Файлы конфигурации

| Файл | В git | Назначение |
|------|-------|------------|
| `config.example.yaml` | да | шаблон → `config.yaml` |
| `secrets.example.yaml` | да | шаблон → `secrets.yaml` |
| `state.example.yaml` | да | шаблон → `state.yaml` |
| `config.yaml` | нет | серверы, xray, настройки бота |
| `secrets.yaml` | нет | пароли, токен, Reality-ключи |
| `state.yaml` | нет | пользователи, подписки, заявки бота |

## Справочник команд

### Деплой и инфраструктура

| Команда | Описание |
|---------|----------|
| `vpnctl init` | Шаблоны YAML + SSH-ключ в `keys/` |
| `vpnctl bootstrap [--no-bot] [--cleanup]` | Чистая установка: keys → vless → [bot] → check |
| `vpnctl redeploy [--no-bot]` | backup → cleanup → полный деплой |
| `vpnctl all [--no-bot]` | keys → vless → links refresh → [bot] → check |
| `vpnctl keys` | Установить SSH public key на все серверы |
| `vpnctl vless [id...] [--new-keys]` | Деплой/обновление Xray |
| `vpnctl bot` | Загрузить `tgbot` и конфиги на VPS бота |
| `vpnctl links refresh` | Пересобрать VLESS-ссылки в `state.yaml` |
| `vpnctl check` | Xray активен, UUID на месте |
| `vpnctl backup` | Снимок yaml → `backups/` |
| `vpnctl cleanup` | Удалить xray/bot/legacy на всех серверах |
| `vpnctl passwd [id...] --generate` | Безопасная смена root-пароля |

`redeploy` прерывается, если backup не удался. Не используйте `vless --new-keys`, если не хотите инвалидировать текущие клиентские конфиги.

### Пользователи (`vpnctl users`)

| Команда | Описание |
|---------|----------|
| `list` | Подписчики в `state.yaml` |
| `add ID [--label NAME] [--never] [--days N]` | Создать, провижинить на всех серверах, вывести ссылки |
| `show ID [--format links\|all\|happ\|json]` | Показать ссылки (`links` — только gRPC по умолчанию) |
| `export ID [-o file]` | Блок подписки для Happ / отправки файлом |
| `revoke ID` | Удалить из `state.yaml` и Xray |
| `sync ID` | Добавить существующий UUID на новые серверы |
| `renew ID DAYS` | Продлить подписку |
| `never ID on\|off` | Вкл/выкл бессрочную подписку |
| `sweep` | Удалить просроченных из state и Xray |

`ID` — любая строка: числовой Telegram id (`123456789`) или произвольный ключ (`client-ivan`).

**Примеры:**

```powershell
vpnctl users add 123456789 --label admin-happ --never
vpnctl users add client-ivan --days 90
vpnctl users show client-ivan --format happ
vpnctl users export client-ivan -o subscription.txt
vpnctl users renew client-ivan 30
vpnctl users revoke client-ivan
```

### Серверы (`vpnctl servers`)

| Команда | Описание |
|---------|----------|
| `list` | Серверы из `config.yaml` (отмечает хост бота) |
| `traffic [id...]` | RX/TX с последней загрузки |
| `summary` | Число серверов, пользователи, ожидающие заявки бота |

### Смена root-пароля

```powershell
vpnctl passwd --generate
vpnctl passwd de --password 'NewSecurePass123'
```

После смены пароля на ноде бота выполните `vpnctl bot`, чтобы синхронизировать `secrets.yaml` на сервер.

## Telegram-бот (опционально)

Деплой: `vpnctl bot` или `vpnctl bootstrap` (без `--no-bot`).

- Бинарник: `dist/tgbot-linux-amd64` → `/root/ssh/tgbot` (собирается автоматически, если файла нет)
- Сервис: `tg-subscription-bot.service`

```bash
systemctl status tg-subscription-bot
journalctl -u tg-subscription-bot -f
```

Функции бота дублируют CLI: одобрение заявок, создание пользователя/подписки (кнопка «➕ Создать пользователя» или `/add_user <user_id> [days|never] [label]`, аналог `vpnctl users add`), renew/revoke/sync, трафик и статус. Нужны `telegram.bot_token` и `bot.approver_user_id` в конфиге.

Локальная разработка (нужен доступ к `api.telegram.org`):

```bash
go run ./cmd/tgbot
```

## Сборка

| Цель | Команда |
|------|---------|
| Локально | `make build` → `vpnctl` + `tgbot` |
| Кросс-компиляция | `make build-all` → `dist/vpnctl-*`, `dist/tgbot-*` |
| Бот для сервера | `make build-bot-linux` |

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o dist/vpnctl-linux-amd64 ./cmd/vpnctl
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o dist/tgbot-linux-amd64 ./cmd/tgbot
```

## Структура проекта

```
cmd/vpnctl/           точка входа CLI
cmd/tgbot/            точка входа Telegram-бота
internal/
  subscription/       общая логика пользователей/серверов (CLI + бот)
  botapp/             обработчики Telegram
  config/             типы и чтение/запись YAML
  deploy/             деплой + CLI users/servers
  links/              сборка VLESS URL
  sshclient/          SSH/SFTP
  xray/               удалённое управление Xray
systemd/              tg-subscription-bot.service
Makefile
config.example.yaml
secrets.example.yaml
state.example.yaml
```

## Безопасность

Не коммитьте `config.yaml`, `secrets.yaml`, `state.yaml`, `keys/` и `backups/`.
