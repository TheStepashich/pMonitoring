#!/usr/bin/env bash
# Перезапуск monitor.service на всех серверах сети pnet.
# Не пересобирает и не заливает бинарник — только systemctl restart.
#
# Логины: по умолчанию DEFAULT_USER, для отдельных серверов переопределяются в HOSTS.
#
# Использование:
#   ./restart.sh               # перезапустить на всех серверах
#   ./restart.sh 10.5.0.1      # перезапустить только на одном
#
# Переменные окружения:
#   RESTART_USER   логин по умолчанию (по умолчанию: potato)
set -uo pipefail
cd "$(dirname "$0")"

DEFAULT_USER="${RESTART_USER:-potato}"

# Список серверов: "ip|user" (user необязателен — берётся DEFAULT_USER).
# 10.5.0.2 (thestepashich-thinkbook) сейчас не в сети — раскомментировать при появлении.
HOSTS=(
  # "10.5.0.1"
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

# Перезапускает monitor.service на одном хосте. root -> без sudo;
# passwordless sudo -> тихо; иначе пароль запрашивается один раз.
restart_one() {
    local ip="$1" user="$2"
    local dest="$user@$ip"
    local cmd

    if [[ "$user" == "root" ]]; then
        echo "==> $ip ($user, root)"
        ssh "$dest" 'systemctl restart monitor && systemctl is-active monitor'
        return $?
    fi

    if ssh "$dest" 'sudo -n true' 2>/dev/null; then
        echo "==> $ip ($user, passwordless sudo)"
        ssh "$dest" 'sudo systemctl restart monitor && sudo systemctl is-active monitor'
        return $?
    fi

    read -r -s -p "Пароль sudo для $dest: " PASS
    echo
    if [[ -z "$PASS" ]]; then
        echo "пустой пароль, пропускаю $ip" >&2
        return 1
    fi
    echo "==> $ip ($user, sudo -S)"
    printf '%s\n' "$PASS" | ssh "$dest" \
        "sudo -S bash -c 'systemctl restart monitor && systemctl is-active monitor'"
}

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
    if restart_one "$ip" "$user"; then
        echo "OK: $ip"
    else
        echo "!!! НЕ УДАЛОСЬ: $ip"
        FAIL=1
    fi
done

echo ""
if [[ $FAIL -eq 0 ]]; then
    echo "Готово: все сервисы перезапущены"
else
    echo "Готово, но были ошибки (см. выше)" >&2
fi
exit $FAIL
