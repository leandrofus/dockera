---
title: Командная строка
description: Каждая команда ducktainer и её основные параметры.
order: 6
---

| Команда | Назначение |
|---|---|
| `ducktainer serve` | Веб-интерфейс и API (`--listen`, `--allow`, `--tls auto\|on\|off`, `--cert/--key`, `--expose :2376`, `--agent-listen :9901`) |
| `ducktainer ui` | То же, что `serve`, и дополнительно открывает браузер |
| `ducktainer agent` | Агент с исходящим подключением (`--server`, `--name`, `--fingerprint`, `--socket`, `--install-service`; токен в `DUCKTAINER_AGENT_TOKEN`) |
| `ducktainer expose` | Публикует только Docker API, защищённый списком разрешённых, токеном и TLS (`--read-only`, `--no-exec`) |
| `ducktainer engine install [--mode wsl\|native] \| status \| relay` | Windows: установка Docker без Docker Desktop |
| `ducktainer setup [--dry-run]` | Настраивает службу под установку Docker |
| `ducktainer auth show \| set … \| test <user>` | Вход под системным пользователем и сопоставление ролей |
| `ducktainer allow ls \| add \| rm` | Список разрешённых сетевых адресов |
| `ducktainer token --rotate` | Новый аварийный/API-токен (хранится только его хеш) |
| `ducktainer endpoint ls \| add \| rm` | Хосты Docker (`unix://`, `ssh://user@host`, `tcp://host:2376`, `agent://name`) |
| `ducktainer registry ls \| add \| rm` | Учётные данные приватных реестров (`--hosts` ограничивает, каким хостам они передаются) |
| `ducktainer ps \| logs \| sh` | Контейнеры по стекам, логи контейнера или всего стека, интерактивная оболочка (выбирает bash/ash/sh) |
| `ducktainer docker-env <host>` | `eval "$(ducktainer docker-env prod)"`, чтобы использовать docker CLI с указанным хостом |
| `ducktainer version` | Вывод версии |

`ducktainer help` показывает, где хранятся конфигурация и данные; изменить этот путь можно через `DUCKTAINER_HOME`.
