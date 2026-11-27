package app

import "testing"

func TestShouldHideOnClose(t *testing.T) {
	cases := []struct {
		name    string
		running int
		want    bool
	}{
		{name: "idle app quits", running: 0, want: false},
		{name: "one running task hides", running: 1, want: true},
		{name: "several running tasks hide", running: 4, want: true},
		{name: "negative count is idle", running: -1, want: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := shouldHideOnClose(testCase.running); got != testCase.want {
				t.Fatalf("shouldHideOnClose(%d) = %v, want %v", testCase.running, got, testCase.want)
			}
		})
	}
}

func TestCloseDecision(t *testing.T) {
	cases := []struct {
		name      string
		running   int
		trayReady bool
		want      closeAction
	}{
		{name: "idle with tray quits", running: 0, trayReady: true, want: closeQuit},
		{name: "running with tray hides", running: 2, trayReady: true, want: closeHide},
		{name: "idle without tray quits", running: 0, trayReady: false, want: closeQuit},
		{name: "running without tray falls back to quit", running: 2, trayReady: false, want: closeQuit},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := closeDecision(testCase.running, testCase.trayReady); got != testCase.want {
				t.Fatalf("closeDecision(%d, %v) = %v, want %v", testCase.running, testCase.trayReady, got, testCase.want)
			}
		})
	}
}

func TestWindowTitleShowsRunningCount(t *testing.T) {
	cases := map[int]string{
		0: "FileForge Desktop",
		1: "FileForge Desktop — 1 task running",
		3: "FileForge Desktop — 3 tasks running",
	}

	for running, want := range cases {
		if got := windowTitle(running); got != want {
			t.Fatalf("windowTitle(%d) = %q, want %q", running, got, want)
		}
		if got := trayTooltip(running); got != want {
			t.Fatalf("trayTooltip(%d) = %q, want %q", running, got, want)
		}
	}
}
