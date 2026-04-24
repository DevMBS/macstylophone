//go:build darwin

package hardware

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type defaultsDomain struct {
	name        string
	currentHost bool
}

type defaultsBackup struct {
	domain defaultsDomain
	path   string
	exists bool
}

type GestureSuppressor struct {
	backups  []defaultsBackup
	active   bool
	dockOnly bool
}

func NewDockOnlyGestureSuppressor() *GestureSuppressor {
	return &GestureSuppressor{dockOnly: true}
}

func (g *GestureSuppressor) Start() error {
	if g.active {
		return nil
	}

	var backups []defaultsBackup
	for _, domain := range g.activeDomains() {
		backup, err := backupDefaultsDomain(domain)
		if err != nil {
			g.restore(backups)
			return err
		}
		backups = append(backups, backup)

		for _, key := range gestureKeys {
			if err := writeGestureKey(domain, key); err != nil {
				g.restore(backups)
				return fmt.Errorf("не удалось отключить жест %q для %q: %w", key, domain.name, err)
			}
		}
	}

	g.backups = backups
	g.active = true

	if err := writeDockGestureOverrides(); err != nil {
		g.restore(g.backups)
		g.backups = nil
		g.active = false
		return err
	}

	refreshTrackpadPreferences()

	return nil
}

func (g *GestureSuppressor) Stop() {
	if !g.active {
		return
	}

	g.restore(g.backups)
	removeDockGestureOverrides()
	g.backups = nil
	g.active = false
	refreshTrackpadPreferences()
}

func (g *GestureSuppressor) activeDomains() []defaultsDomain {
	if g.dockOnly {
		return dockGestureDomains
	}
	return gestureDomains
}

func (g *GestureSuppressor) restore(backups []defaultsBackup) {
	for i := len(backups) - 1; i >= 0; i-- {
		backup := backups[i]
		if backup.exists && backup.path != "" {
			_ = runDefaults(backup.domain, "import", backup.domain.name, backup.path)
		} else {
			for _, key := range gestureKeys {
				_ = runDefaults(backup.domain, "delete", backup.domain.name, key)
			}
		}
		if backup.path != "" {
			_ = os.Remove(backup.path)
		}
	}
}

func backupDefaultsDomain(domain defaultsDomain) (defaultsBackup, error) {
	backup := defaultsBackup{domain: domain}

	if !defaultsDomainExists(domain) {
		return backup, nil
	}

	backup.exists = true
	backupPath, err := createBackupPath(domain)
	if err != nil {
		return defaultsBackup{}, err
	}

	if err := runDefaults(domain, "export", domain.name, backupPath); err != nil {
		_ = os.Remove(backupPath)
		return defaultsBackup{}, fmt.Errorf("не удалось экспортировать настройки %q: %w", domain.name, err)
	}

	backup.path = backupPath
	return backup, nil
}

func defaultsDomainExists(domain defaultsDomain) bool {
	cmd := exec.Command("/usr/bin/defaults", appendHost(domain, "read", domain.name)...)
	return cmd.Run() == nil
}

func createBackupPath(domain defaultsDomain) (string, error) {
	hostPrefix := "global"
	if domain.currentHost {
		hostPrefix = "currenthost"
	}

	file, err := os.CreateTemp("", "stylophone-"+hostPrefix+"-"+sanitizeName(domain.name)+"-*.plist")
	if err != nil {
		return "", fmt.Errorf("не удалось создать временный файл бэкапа: %w", err)
	}

	path := file.Name()
	if closeErr := file.Close(); closeErr != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("не удалось закрыть временный файл бэкапа: %w", closeErr)
	}

	return path, nil
}

func sanitizeName(value string) string {
	sanitized := strings.ReplaceAll(value, ".", "-")
	sanitized = strings.ReplaceAll(sanitized, " ", "-")
	return strings.ReplaceAll(filepath.Clean(sanitized), string(filepath.Separator), "-")
}

func runDefaults(domain defaultsDomain, args ...string) error {
	cmdArgs := appendHost(domain, args...)
	cmd := exec.Command("/usr/bin/defaults", cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}

	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return err
	}

	if strings.Contains(trimmed, "does not exist") || strings.Contains(trimmed, "Could not find domain") {
		return nil
	}

	return fmt.Errorf("%w: %s", err, trimmed)
}

func writeGestureKey(domain defaultsDomain, key string) error {
	if domain.name == "com.apple.dock" || domain.name == "NSGlobalDomain" {
		return runDefaults(domain, "write", domain.name, key, "-bool", "false")
	}

	return runDefaults(domain, "write", domain.name, key, "-int", "0")
}

func appendHost(domain defaultsDomain, args ...string) []string {
	if !domain.currentHost {
		return args
	}

	withHost := make([]string, 0, len(args)+1)
	withHost = append(withHost, "-currentHost")
	withHost = append(withHost, args...)
	return withHost
}

func refreshTrackpadPreferences() {
	_ = exec.Command("/usr/bin/killall", "cfprefsd").Run()
	_ = exec.Command("/usr/bin/killall", "Dock").Run()
	_ = exec.Command("/usr/bin/killall", "SystemUIServer").Run()
}

func writeDockGestureOverrides() error {
	for _, key := range dockGestureDefaults {
		if err := runDefaults(defaultsDomain{name: "com.apple.dock"}, "write", "com.apple.dock", key, "-bool", "false"); err != nil {
			return fmt.Errorf("не удалось отключить dock gesture %q: %w", key, err)
		}
	}
	return nil
}

func removeDockGestureOverrides() {
	for _, key := range dockGestureDefaults {
		_ = runDefaults(defaultsDomain{name: "com.apple.dock"}, "delete", "com.apple.dock", key)
	}
}

var gestureDomains = []defaultsDomain{
	{name: "NSGlobalDomain", currentHost: false},
	{name: "NSGlobalDomain", currentHost: true},
	{name: "com.apple.dock", currentHost: false},
	{name: "com.apple.AppleMultitouchTrackpad", currentHost: true},
	{name: "com.apple.AppleMultitouchTrackpad", currentHost: false},
	{name: "com.apple.driver.AppleBluetoothMultitouch.trackpad", currentHost: true},
	{name: "com.apple.driver.AppleBluetoothMultitouch.trackpad", currentHost: false},
}

var dockGestureDomains = []defaultsDomain{
	{name: "NSGlobalDomain", currentHost: false},
	{name: "NSGlobalDomain", currentHost: true},
	{name: "com.apple.dock", currentHost: false},
}

var gestureKeys = []string{
	"AppleEnableSwipeNavigateWithScrolls",
	"AppleEnableMouseSwipeNavigateWithScrolls",
	"showMissionControlGestureEnabled",
	"showAppExposeGestureEnabled",
	"showDesktopGestureEnabled",
	"showLaunchpadGestureEnabled",
	"trackpadPinch",
	"trackpadRotate",
	"trackpadFourFingerHorizSwipeGesture",
	"trackpadFourFingerVertSwipeGesture",
	"trackpadThreeFingerHorizSwipeGesture",
	"trackpadThreeFingerVertSwipeGesture",
	"trackpadFiveFingerPinchSwipeGesture",
	"trackpadTwoFingerFromRightEdgeSwipeGesture",
	"trackpadTwoFingerDoubleTapGesture",
	"TrackpadScroll",
	"TrackpadMomentumScroll",
	"TrackpadPinch",
	"TrackpadRotate",
	"TrackpadThreeFingerDrag",
	"TrackpadThreeFingerTapGesture",
	"TrackpadThreeFingerHorizSwipeGesture",
	"TrackpadThreeFingerVertSwipeGesture",
	"TrackpadFourFingerHorizSwipeGesture",
	"TrackpadFourFingerVertSwipeGesture",
	"TrackpadFiveFingerPinchGesture",
	"TrackpadTwoFingerDoubleTapGesture",
	"TrackpadTwoFingerFromRightEdgeSwipeGesture",
}

var dockGestureDefaults = []string{
	"showMissionControlGestureEnabled",
	"showAppExposeGestureEnabled",
	"showDesktopGestureEnabled",
	"showLaunchpadGestureEnabled",
}
