package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/fedzzito/power-bridge/internal/config"
)

// --------------------------------------------------------------------------
// Setup page (GET /setup)
// --------------------------------------------------------------------------

type setupPageData struct {
	Error   string
	Success string
	Cfg     *config.Config
}

func (s *Server) setupPage(w http.ResponseWriter, r *http.Request) {
	data := setupPageData{Cfg: s.cfg}
	if err := s.tmplSetup.Execute(w, data); err != nil {
		log.Printf("setup template error: %v", err)
	}
}

// --------------------------------------------------------------------------
// Setup save (POST /setup/save)
// --------------------------------------------------------------------------

func (s *Server) setupSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form data", http.StatusBadRequest)
		return
	}

	s.cfg.WIFISSID = strings.TrimSpace(r.FormValue("wifi_ssid"))
	s.cfg.WIFIPassword = r.FormValue("wifi_password")
	s.cfg.PoweroptiIP = strings.TrimSpace(r.FormValue("poweropti_ip"))
	s.cfg.PoweroptiAPIKey = strings.TrimSpace(r.FormValue("poweropti_api_key"))
	s.cfg.ShellyMAC = strings.TrimSpace(r.FormValue("shelly_mac"))
	s.cfg.Hostname = strings.TrimSpace(r.FormValue("hostname"))

	if profile := r.FormValue("device_profile"); profile != "" {
		s.cfg.DeviceProfile = config.DeviceProfile(profile)
	}
	if phaseMode := r.FormValue("phase_mode"); phaseMode != "" {
		s.cfg.PhaseMode = config.PhaseDistribution(phaseMode)
	}

	s.cfg.Configured = true

	if err := config.Save(s.configPath, s.cfg); err != nil {
		data := setupPageData{Error: fmt.Sprintf("Fehler beim Speichern: %v", err), Cfg: s.cfg}
		_ = s.tmplSetup.Execute(w, data)
		return
	}

	s.logf("Setup completed. SSID=%s, Poweropti=%s", s.cfg.WIFISSID, s.cfg.PoweroptiIP)

	// Write wpa_supplicant config and restart networking (best-effort).
	go func() {
		time.Sleep(500 * time.Millisecond)
		applyWifiConfig(s.cfg.WIFISSID, s.cfg.WIFIPassword)
	}()

	http.Redirect(w, r, "/?setup=done", http.StatusSeeOther)
}

// applyWifiConfig writes /etc/wpa_supplicant/wpa_supplicant.conf and triggers
// wpa_supplicant to reconnect. Errors are logged but not fatal.
func applyWifiConfig(ssid, password string) {
	wpaConf := fmt.Sprintf(`country=DE
ctrl_interface=DIR=/var/run/wpa_supplicant GROUP=netdev
update_config=1

network={
	ssid="%s"
	psk="%s"
	key_mgmt=WPA-PSK
}
`, ssid, password)

	if err := writeFileRoot("/etc/wpa_supplicant/wpa_supplicant.conf", wpaConf); err != nil {
		log.Printf("wpa_supplicant write failed: %v", err)
		return
	}
	// Restart networking; ignore errors on non-Pi systems.
	_ = exec.Command("systemctl", "restart", "wpa_supplicant@wlan0").Run()
	_ = exec.Command("systemctl", "stop", "hostapd").Run()
	_ = exec.Command("systemctl", "stop", "dnsmasq").Run()
	_ = exec.Command("systemctl", "restart", serviceName).Run()
}

func writeFileRoot(path, content string) error {
	cmd := exec.Command("tee", path)
	cmd.Stdin = strings.NewReader(content)
	return cmd.Run()
}

// --------------------------------------------------------------------------
// WiFi scan (GET /setup/scan) – returns JSON list of visible SSIDs
// --------------------------------------------------------------------------

func (s *Server) setupScanWifi(w http.ResponseWriter, r *http.Request) {
	jsonHeader(w)
	out, err := exec.Command("iwlist", "wlan0", "scan").Output()
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	var ssids []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ESSID:") {
			ssid := strings.Trim(strings.TrimPrefix(line, "ESSID:"), `"`)
			if ssid != "" {
				ssids = append(ssids, ssid)
			}
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ssids": ssids})
}
