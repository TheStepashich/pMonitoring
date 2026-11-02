#!/usr/bin/env bash
# Обновление монитора на всех серверах сети pnet.
# Последовательно заходит на каждый сервер: копирует бинарник/конфиг/юнит
# и перезапускает monitor.service (рестарт внутри deploy.sh).
#
# Логины: по умолчанию DEFAULT_USER, для отдельных серверов переопределяются в HOSTS.
#
# Использование:
#   ./update.sh               # собрать и обновить все серверы
#   ./update.sh 10.5.0.3      # обновить только один сервер
#
# Переменные окружения:
#   UPDATE_USER   логин по умолчанию (по умолчанию: potato)
#   EASYTIER_CMD  переопределить easytier.command в разворачиваемом конфиге
set -uo pipefail
cd "$(dirname "$0")"

DEFAULT_USER="${UPDATE_USER:-potato}"

# Список серверов: "ip|user" (user необязателен — берётся DEFAULT_USER).
# 10.5.0.2 (thestepashich-thinkbook) сейчас не в сети — раскомментировать при появлении.
HOSTS=(
  "10.5.0.2|potato"
  "10.5.0.3|potato"
  "10.5.0.4|potato"
  "10.5.0.5|potato"
  "10.5.0.6|potato"
  "10.5.0.10|potato"
  "10.5.0.11|molot"
  "10.5.0.15|bork"
  # "10.5.0.2|thestepashich"
)

ONLY="${1:-}"

echo "==> сборка"
if ! ./build.sh; then
    echo "!!! сборка не удалась" >&2
    exit 1
fi

# Пароль sudo спросим один раз на все серверы и передадим в deploy.sh через SUDO_PASS.
# Если оставить пустым — deploy.sh будет спрашивать по каждому серверу отдельно.
if [[ -z "${SUDO_PASS:-}" ]]; then
    read -r -s -p "Пароль sudo для серверов (пусто — спрашивать по каждому): " SUDO_PASS
    echo
fi
export SUDO_PASS

FAIL=0
for entry in "${HOSTS[@]}"; do
    ip="${entry%%|*}"
    user="${entry#*|}"
    if [[ "$user" == "$ip" ]]; then
        user="$DEFAULT_USER"
    fi
    if [[ -n "$ONLY" && "$ip" != "$ONLY" ]]; then
        continue
    fi
    echo ""
    echo "=== $ip ($user) ==="
    if ./deploy.sh "$ip" "$user"; then
        echo "OK: $ip"
    else
        echo "!!! НЕ УДАЛОСЬ: $ip"
        FAIL=1
    fi
done

echo ""
if [[ $FAIL -eq 0 ]]; then
    echo "Готово: все серверы обновлены"
else
    echo "Готово, но были ошибки (см. выше)" >&2
fi
exit $FAIL
