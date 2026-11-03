package main

import "testing"

func TestTrayTextLocalized(t *testing.T) {
	zh := trayText("zh")
	if zh.open != "打开 FlashRoute" || zh.checkUpdates != "检查更新…" || zh.quit != "退出 FlashRoute" {
		t.Fatalf("zh labels = %+v", zh)
	}
	en := trayText("en")
	if en.open != "Open FlashRoute" || en.checkUpdates != "Check for Updates…" || en.quit != "Quit FlashRoute" {
		t.Fatalf("en labels = %+v", en)
	}
}
