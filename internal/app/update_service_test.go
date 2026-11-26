package app

import (
	"strings"
	"testing"

	"fileforge-desktop/internal/update"
)

// TestUpdateServiceContract pins the composition-root wiring: the service is
// bound, reports the running version and a gating reason, and refuses network
// and install operations on a build that cannot update itself.
func TestUpdateServiceContract(t *testing.T) {
	a := newTestApp(t)

	status := a.Update.GetUpdateStatus()
	if !status.Success {
		t.Fatalf("GetUpdateStatus() failed: %s", status.Message)
	}
	if status.CurrentVersion == "" {
		t.Fatal("GetUpdateStatus() returned no current version")
	}
	if status.State != update.StateUnavailable {
		t.Fatalf("state = %q, want %q for the dev test build", status.State, update.StateUnavailable)
	}
	if status.Reason == "" {
		t.Fatal("GetUpdateStatus() returned no gating reason")
	}

	check := a.Update.CheckForUpdates()
	if check.Success || check.State != update.StateUnavailable {
		t.Fatalf("CheckForUpdates() on an unavailable build = %+v", check)
	}

	download := a.Update.DownloadUpdate()
	if download.Success || !strings.Contains(download.Error, "unavailable") {
		t.Fatalf("DownloadUpdate() on an unavailable build = %+v", download)
	}

	install := a.Update.InstallUpdate()
	if install.Success {
		t.Fatalf("InstallUpdate() on an unavailable build = %+v", install)
	}

	consent := a.Update.GrantInstallConsent()
	if consent.Success {
		t.Fatalf("GrantInstallConsent() without a download = %+v", consent)
	}
}
