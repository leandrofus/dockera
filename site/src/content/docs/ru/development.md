---
title: Разработка
description: Сборка ducktainer из исходного кода, структура кода и добавление перевода.
order: 8
---

## Сборка

```sh
make help           # показать все доступные цели make
make run            # собрать и запустить ducktainer serve локально (http://127.0.0.1:9900)
make ui             # собрать и запустить ducktainer ui (открывает браузер)
make build          # веб-интерфейс + статический бинарный файл (без PAM) в bin/
make build-pam      # с PAM (нужны gcc и pam-devel / libpam0g-dev)
make test           # запустить все тесты (модульные тесты Go + проверка типов веб-интерфейса)
make dev            # интерфейс с горячей перезагрузкой для работающего `ducktainer serve` на :9900
make packages       # rpm + deb (amd64, arm64), собираются в контейнере; нужен только Docker
make dist           # статические бинарные файлы для linux/darwin/windows
```

## Структура

- `main.go`: интерфейс командной строки.
- `internal/server`: HTTP/WebSocket API.
- `internal/docker`: клиент Engine API (unix, ssh, tcp+tls, npipe).
- `internal/auth`: PAM, роли и вспомогательный процесс от root.
- `internal/setup`: определение установки Docker.
- `internal/engine`: установка Docker в Windows.
- `internal/compose`, `internal/templates`, `internal/proxy`, `internal/access`, `internal/i18n`.
- `packaging/`: юниты systemd, файлы PAM, nfpm.
- `web/`: React + Vite.
- `site/`: этот сайт (Astro).

## Переводы

Исходный язык — английский. Чтобы добавить язык:

1. Скопируйте `web/src/i18n/es.ts` (интерфейс) и `internal/i18n/es.go` (ошибки API и CLI). Ключи — это английские строки; ключи вида `ctx|text` задают варианты по роду и числу, которые использует `tc()`.
2. Зарегистрируйте новый каталог в `web/src/i18n/index.ts` и `internal/i18n/i18n.go`.
3. Запустите `make test`: `internal/i18n/catalog_test.go` завершится с ошибкой, если в каталоге нет сообщения, которое использует код, или есть сообщение, которое код больше не использует.

## Релизы

Версия хранится в `web/package.json`. Чтобы опубликовать новый релиз, увеличьте её в своём pull request: когда слияние попадёт в `main`, workflow Tag создаст тег `v<version>` и запустит релиз, который соберёт пакеты и бинарные файлы и пересоберёт этот сайт. Слияния без изменения версии ничего не публикуют.
