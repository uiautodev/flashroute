//go:build darwin

package update

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func installDesktopAsset(dmg, exe string) error {
	if exe == "" {
		return errors.New("executable path is unknown")
	}
	if _, err := os.Stat(dmg); err != nil {
		return err
	}
	mount, err := os.MkdirTemp("", "flashroute-dmg-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(mount)
	attach := exec.Command("hdiutil", "attach", "-nobrowse", "-readonly", "-quiet", "-mountpoint", mount, dmg)
	if out, err := attach.CombinedOutput(); err != nil {
		return fmt.Errorf("mount update image: %v: %s", err, string(out))
	}
	defer exec.Command("hdiutil", "detach", "-quiet", mount).Run()

	sourceApp := filepath.Join(mount, "FlashRoute.app")
	source := filepath.Join(sourceApp, "Contents", "MacOS", "flashroute")
	if _, err := os.Stat(source); err != nil {
		return fmt.Errorf("update image does not contain FlashRoute.app: %w", err)
	}
	if err := replaceExecutable(source, exe); err != nil {
		return err
	}
	bundle := appBundle(exe)
	if bundle == "" {
		return nil
	}
	if data, err := os.ReadFile(filepath.Join(sourceApp, "Contents", "Info.plist")); err == nil {
		_ = os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), data, 0o644)
	}
	if out, err := exec.Command("codesign", "--force", "--deep", "--sign", "-", bundle).CombinedOutput(); err != nil {
		log.Printf("re-sign updated app: %v: %s", err, string(out))
	}
	return nil
}

func appBundle(exe string) string {
	marker := ".app/Contents/MacOS/"
	if i := strings.Index(exe, marker); i >= 0 {
		return exe[:i+len(".app")]
	}
	return ""
}
