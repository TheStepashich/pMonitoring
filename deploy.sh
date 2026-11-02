#!/usr/bin/env bash
# Развёртывание монитора на сервере: scp (бинарник, конфиг, systemd-юнит)
# и запуск через systemctl по тому же ssh.
#
# sudo на сервере:
#   - вход под root          -> sudo не нужен;
#   - passwordless sudo      -> тихо;
#   - иначе                  -> пароль возьмётся из переменной окружения SUDO_PASS,
#                               а если она не задана — запросится один раз локально
#                               и будет передан через sudo -S (одним sudo-вызовом).
#
# Требования на сервере:
#   - установлен easytier/pnet (путь в config.yaml -> easytier.command).
#
# Использование:
#   ./deploy.sh <host> [user] [arch]         # arch: amd64|arm64 (по умолчанию определяется по ssh)
#   ./deploy.sh 10.5.0.10
#   ./deploy.sh 10.5.0.10 root
#   ./deploy.sh 10.5.0.10 root arm64
#
# Переменные окружения:
#   SUDO_PASS    sudo-пароль (общий; для разных хостов/пользователей — запускать
#                ./deploy.sh без неё, тогда пароль спросится для каждого сервера)
#   EASYTIER_CMD переопределить easytier.command в разворачиваемом конфиге,
#                напр. EASYTIER_CMD=/usr/local/bin/pnet ./deploy.sh 10.5.0.10
set -euo pipefail
cd "$(dirname "$0")"

HOST="${1:-}"
if [[ -z "$HOST" ]]; then
    echo "использование: $0 <host> [user] [arch]"
    exit 2
fi
SSH_USER="${2:-$USER}"
ARCH="${3:-}"
SSH_DEST="$SSH_USER@$HOST"

echo "==> ssh $SSH_DEST"

if [[ -z "$ARCH" ]]; then
    MACHINE=$(ssh "$SSH_DEST" uname -m)
    case "$MACHINE" in
        x86_64|amd64)   ARCH=amd64 ;;
        aarch64|arm64)  ARCH=arm64 ;;
        *) echo "неизвестная архитектура сервера: $MACHINE" >&2; exit 1 ;;
    esac
fi
echo "    архитектура: $ARCH"

BIN="monitor-linux-$ARCH"
if [[ ! -f "$BIN" ]]; then
    echo "нет бинарника $BIN (собери через ./build.sh)" >&2
    exit 1
fi

# Копия конфига (возможен переопределённый easytier.command)
CONF="/tmp/monitor-config-$HOST.yaml"
cp config.yaml "$CONF"
if [[ -n "${EASYTIER_CMD:-}" ]]; then
    sed -i "s|^\([[:space:]]*command:\).*|\1 \"$EASYTIER_CMD\"|" "$CONF"
    echo "    easytier.command -> $EASYTIER_CMD"
fi

TMPBIN="/tmp/monitor-$HOST"
TMPSVC="/tmp/monitor-$HOST.service"
SETUP="/tmp/monitor-setup-$HOST.sh"

# Установочный скрипт, который выполнится под root на сервере.
# Аргументы: 1=бинарник, 2=конфиг, 3=юнит.
# Конфиг ставится только при первом развёртывании — существующий не перезаписываем,
# чтобы сохранить локальные правки на сервере.
cat > "$SETUP" <<'SETUP_EOF'
#!/usr/bin/env bash
set -euo pipefail
mkdir -p /opt/monitor
install -m 0755 "$1" /opt/monitor/monitor
if [[ ! -f /opt/monitor/config.yaml ]]; then
    install -m 0644 "$2" /opt/monitor/config.yaml
fi
install -m 0644 "$3" /etc/systemd/system/monitor.service
systemctl daemon-reload
systemctl enable monitor
systemctl restart monitor
systemctl is-active monitor
systemctl status --no-pager monitor 2>/dev/null | head -n 8 || true
SETUP_EOF

echo "==> scp $BIN $CONF monitor.service $SETUP -> $SSH_DEST:/tmp"
scp "$BIN" "$SSH_DEST:$TMPBIN"
scp "$CONF" "$SSH_DEST:$CONF"
scp monitor.service "$SSH_DEST:$TMPSVC"
scp "$SETUP" "$SSH_DEST:$SETUP"

# Выбор способа выполнения установки под root
RUN_CMD=""
if [[ "$SSH_USER" != "root" ]]; then
    if ssh "$SSH_DEST" "sudo -n true" 2>/dev/null; then
        echo "==> установка (passwordless sudo)"
        RUN_CMD="sudo bash $SETUP '$TMPBIN' '$CONF' '$TMPSVC'"
    else
        if [[ -n "${SUDO_PASS:-}" ]]; then
            PASS="$SUDO_PASS"
        else
            read -r -s -p "Пароль sudo для $SSH_DEST: " PASS
            echo
            if [[ -z "$PASS" ]]; then
                echo "пустой пароль, отмена" >&2
                exit 1
            fi
        fi
        echo "==> установка (sudo -S)"
        printf '%s\n' "$PASS" | ssh "$SSH_DEST" "sudo -S bash $SETUP '$TMPBIN' '$CONF' '$TMPSVC'"
    fi
else
    echo "==> установка (root)"
    RUN_CMD="bash $SETUP '$TMPBIN' '$CONF' '$TMPSVC'"
fi

if [[ -n "$RUN_CMD" ]]; then
    ssh "$SSH_DEST" "$RUN_CMD"
fi

ssh "$SSH_DEST" "rm -f '$TMPBIN' '$CONF' '$TMPSVC' '$SETUP'"
rm -f "$CONF" "$SETUP"

echo "==> готово: $HOST"
