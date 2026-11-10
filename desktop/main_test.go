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

func TestRevealOnActivate(t *testing.T) {
	cases := []struct {
		name      string
		visible   bool
		minimised bool
		want      bool
	}{
		{"hidden window", false, false, true},
		{"minimised window", false, true, true},
		{"visible window", true, false, false},
	}
	for _, c := range cases {
		if got := revealOnActivate(c.visible, c.minimised); got != c.want {
			t.Errorf("%s: revealOnActivate(%v, %v) = %v, want %v", c.name, c.visible, c.minimised, got, c.want)
		}
	}
}
