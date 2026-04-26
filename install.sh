#!/usr/bin/env bash
# =============================================================================
# power-bridge install script for Raspberry Pi OS Lite 32-bit (ARMv6)
# Run as root:  sudo bash install.sh
# =============================================================================
set -euo pipefail

BINARY_URL="${BINARY_URL:-}"     # optional: URL to pre-built binary
BINARY_SRC="${BINARY_SRC:-}"     # optional: local path to binary
CONFIG_DIR="/etc/power-bridge"
AVAHI_SVC_DIR="/etc/avahi/services"
AP_SSID="ShellyMeter-Setup"
AP_IP="192.168.4.1"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || error "Please run as root: sudo bash install.sh"

# ── 1. System packages ────────────────────────────────────────────────────────
info "Installing required packages…"
apt-get update -qq
apt-get install -y --no-install-recommends \
    hostapd \
    dnsmasq \
    avahi-daemon \
    avahi-utils \
    iptables \
    iw \
    wpasupplicant

systemctl unmask hostapd || true
systemctl unmask dnsmasq || true

# ── 2. Create service user ────────────────────────────────────────────────────
info "Creating power-bridge service user"
if ! id power-bridge &>/dev/null; then
    useradd --system --no-create-home --shell /usr/sbin/nologin power-bridge
    # Allow the service user to control systemd units via sudo (for restart)
    echo "power-bridge ALL=(root) NOPASSWD: /bin/systemctl restart power-bridge, /bin/systemctl stop hostapd, /bin/systemctl stop dnsmasq, /bin/systemctl restart wpa_supplicant@wlan0" \
        > /etc/sudoers.d/power-bridge
    chmod 440 /etc/sudoers.d/power-bridge
fi

# ── 3. Binary ─────────────────────────────────────────────────────────────────
if [ -n "$BINARY_SRC" ] && [ -f "$BINARY_SRC" ]; then
    info "Installing binary from $BINARY_SRC"
    install -m 755 "$BINARY_SRC" /usr/local/bin/power-bridge
elif [ -n "$BINARY_URL" ]; then
    info "Downloading binary from $BINARY_URL"
    curl -fsSL "$BINARY_URL" -o /usr/local/bin/power-bridge
    chmod 755 /usr/local/bin/power-bridge
else
    warn "No binary provided. Build it with:"
    warn "  GOARCH=arm GOARM=6 go build -o power-bridge ./cmd/power-bridge"
    warn "Then re-run: BINARY_SRC=./power-bridge sudo bash install.sh"
fi

# ── 4. Config directory ───────────────────────────────────────────────────────
info "Creating config directory $CONFIG_DIR"
mkdir -p "$CONFIG_DIR"
if [ ! -f "$CONFIG_DIR/config.yaml" ]; then
    cp config.yaml.example "$CONFIG_DIR/config.yaml"
    info "Default config written to $CONFIG_DIR/config.yaml"
fi

# ── 4. systemd service ────────────────────────────────────────────────────────
info "Installing systemd service"
cp power-bridge.service /etc/systemd/system/power-bridge.service
systemctl daemon-reload
systemctl enable power-bridge.service

# ── 5. Avahi / mDNS ──────────────────────────────────────────────────────────
info "Registering mDNS service with Avahi"
mkdir -p "$AVAHI_SVC_DIR"
cp avahi/power-bridge.service "$AVAHI_SVC_DIR/power-bridge.service"
systemctl enable avahi-daemon
systemctl restart avahi-daemon || true

# ── 6. Hostname ───────────────────────────────────────────────────────────────
NEW_HOSTNAME="shellypro3em-poweropti"
info "Setting hostname to $NEW_HOSTNAME"
hostnamectl set-hostname "$NEW_HOSTNAME" || echo "$NEW_HOSTNAME" > /etc/hostname
sed -i "s/^127\.0\.1\.1.*/127.0.1.1\t$NEW_HOSTNAME/" /etc/hosts || \
    echo "127.0.1.1 $NEW_HOSTNAME" >> /etc/hosts

# ── 7. Access Point setup ─────────────────────────────────────────────────────
info "Configuring Access Point (hostapd + dnsmasq) for first-run setup"

# hostapd config
cat > /etc/hostapd/hostapd.conf << EOF
interface=wlan0
driver=nl80211
ssid=${AP_SSID}
hw_mode=g
channel=6
wmm_enabled=0
macaddr_acl=0
auth_algs=1
ignore_broadcast_ssid=0
wpa=0
EOF

# Tell hostapd where its config is
grep -q "^DAEMON_CONF" /etc/default/hostapd 2>/dev/null || \
    echo 'DAEMON_CONF="/etc/hostapd/hostapd.conf"' >> /etc/default/hostapd
sed -i 's|^#\?DAEMON_CONF=.*|DAEMON_CONF="/etc/hostapd/hostapd.conf"|' /etc/default/hostapd

# dnsmasq config for AP DHCP
cat > /etc/dnsmasq.d/power-bridge-ap.conf << EOF
# power-bridge AP mode DHCP
interface=wlan0
dhcp-range=192.168.4.10,192.168.4.50,255.255.255.0,24h
address=/#/${AP_IP}
EOF

# dhcpcd: static IP for wlan0 in AP mode
if ! grep -q "power-bridge AP" /etc/dhcpcd.conf 2>/dev/null; then
    cat >> /etc/dhcpcd.conf << EOF

# power-bridge AP mode static IP
interface wlan0
    static ip_address=${AP_IP}/24
    nohook wpa_supplicant
EOF
fi

# Enable IP forwarding
sed -i 's|^#\?net.ipv4.ip_forward=.*|net.ipv4.ip_forward=1|' /etc/sysctl.conf
sysctl -w net.ipv4.ip_forward=1

# ── 8. Firewall / ports ───────────────────────────────────────────────────────
info "Opening required ports (80, 5353)"
iptables -I INPUT -p tcp --dport 80  -j ACCEPT 2>/dev/null || true
iptables -I INPUT -p udp --dport 5353 -j ACCEPT 2>/dev/null || true
# Persist iptables rules if iptables-persistent is available
if command -v netfilter-persistent &>/dev/null; then
    netfilter-persistent save || true
fi

# ── 9. Start services ─────────────────────────────────────────────────────────
info "Starting hostapd and dnsmasq for AP mode"
systemctl restart dhcpcd || true
systemctl restart hostapd || warn "hostapd failed to start (may need reboot)"
systemctl restart dnsmasq || warn "dnsmasq failed to start"

info "Starting power-bridge service"
systemctl start power-bridge.service || warn "power-bridge failed to start"

# ── Done ──────────────────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║  power-bridge installed successfully!                        ║${NC}"
echo -e "${GREEN}╠══════════════════════════════════════════════════════════════╣${NC}"
echo -e "${GREEN}║  Next steps:                                                 ║${NC}"
echo -e "${GREEN}║  1. Connect your phone/laptop to WiFi: ${AP_SSID}     ║${NC}"
echo -e "${GREEN}║  2. Open http://${AP_IP} in your browser                ║${NC}"
echo -e "${GREEN}║  3. Fill in the setup form and click Save                   ║${NC}"
echo -e "${GREEN}║  4. The Pi will connect to your home WiFi automatically     ║${NC}"
echo -e "${GREEN}║                                                              ║${NC}"
echo -e "${GREEN}║  Status:  http://shellypro3em-poweropti.local               ║${NC}"
echo -e "${GREEN}║  API:     http://shellypro3em-poweropti.local/rpc/EM.GetStatus?id=0 ║${NC}"
echo -e "${GREEN}╚══════════════════════════════════════════════════════════════╝${NC}"
