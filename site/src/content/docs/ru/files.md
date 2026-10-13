---
title: Файлы и секреты
description: Где ducktainer хранит свою конфигурацию и как шифруются секреты.
order: 7
---

## Файлы

| Путь | Содержимое |
|---|---|
| `/etc/ducktainer/ducktainer.env` | `DUCKTAINER_OPTS` для `ducktainer serve` |
| `/etc/pam.d/ducktainer` | Стек PAM для входа (`password-auth` в RHEL, `common-auth` в Debian) |
| `/var/lib/ducktainer/` | конфигурация, стеки, журнал аудита, TLS-сертификат, SSH-ключи службы |
| `/etc/systemd/system/ducktainer.service.d/10-docker-access.conf` | создаётся командой `ducktainer setup` |
| `/etc/ducktainer/secret.key` | ключ, которым шифруются секреты в конфигурации (только для root, передаётся службе через systemd `LoadCredential`) |

## Секреты

Секреты в `config.json` (секрет сеанса, токены хостов, пароли реестров) шифруются алгоритмом AES-256-GCM. Аварийный токен хранится только как хеш SHA-256.

Ключ хранится отдельно от данных, поэтому копия или резервная копия `/var/lib/ducktainer` без ключа ничего не раскрывает. **Делайте резервную копию `/etc/ducktainer/secret.key` отдельно**: без него конфигурация не может быть прочитана.

Для бинарного запуска без пакета ключ — это `secret.key` в каталоге данных, если только `DUCKTAINER_KEY_FILE` не указывает на другой путь. Незашифрованные конфигурации из предыдущих версий шифруются при первом запуске.
