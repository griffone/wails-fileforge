package update

import "errors"

// Typed errors for the update path. Callers and tests match them with
// errors.Is; every failure mode the feature has is one of these rather than a
// silent nil, so the UI can tell "no update" apart from "the feed is broken".
var (
	// ErrNetwork reports a transport failure while talking to the release
	// source. The request URL may appear in the wrapped detail; credentials
	// never do.
	ErrNetwork = errors.New("update: network request failed")
	// ErrHTTPStatus reports a non-success HTTP response.
	ErrHTTPStatus = errors.New("update: unexpected HTTP status")
	// ErrMalformedRelease reports release JSON that cannot be parsed or that
	// omits the fields the updater needs.
	ErrMalformedRelease = errors.New("update: malformed release metadata")
	// ErrMalformedChecksums reports a SHA256SUMS file with no usable lines.
	ErrMalformedChecksums = errors.New("update: malformed SHA256SUMS")
	// ErrMalformedSignature reports a .sig that is not base64 of exactly 64
	// bytes.
	ErrMalformedSignature = errors.New("update: malformed signature file")
	// ErrNotStable reports a draft or prerelease on the stable channel.
	ErrNotStable = errors.New("update: release is not a stable release")
	// ErrUnsupportedPlatform reports a platform or architecture the updater
	// does not install on.
	ErrUnsupportedPlatform = errors.New("update: unsupported platform")
	// ErrAssetMissing reports a release with no artifact for this platform.
	ErrAssetMissing = errors.New("update: release has no asset for this platform")
	// ErrChecksumsMissing reports a release with no SHA256SUMS asset.
	ErrChecksumsMissing = errors.New("update: release has no SHA256SUMS asset")
	// ErrChecksumMissing reports a SHA256SUMS file without the selected
	// artifact.
	ErrChecksumMissing = errors.New("update: SHA256SUMS has no entry for the artifact")
	// ErrSignatureMissing reports a release with no detached .sig for the
	// selected artifact.
	ErrSignatureMissing = errors.New("update: release has no detached signature for the artifact")
	// ErrDigestMismatch reports downloaded bytes that do not match SHA256SUMS.
	ErrDigestMismatch = errors.New("update: artifact digest does not match SHA256SUMS")
	// ErrSignatureInvalid reports a signature that does not verify over the
	// downloaded digest with the pinned key.
	ErrSignatureInvalid = errors.New("update: artifact signature is invalid")
	// ErrDownloadFailed reports a truncated or incomplete artifact download.
	ErrDownloadFailed = errors.New("update: artifact download failed")
	// ErrNoRelease reports an operation that needs a checked release first.
	ErrNoRelease = errors.New("update: no release has been checked yet")
	// ErrUnavailable reports an update operation on a build or install where
	// updates are not offered.
	ErrUnavailable = errors.New("update: updates are unavailable")
	// ErrConsentRequired reports an install attempt without explicit consent.
	ErrConsentRequired = errors.New("update: install requires explicit consent")
	// ErrNotDownloaded reports an install attempt without a verified download.
	ErrNotDownloaded = errors.New("update: no verified update has been downloaded")
	// ErrNoInstaller reports a release whose selected artifact is not an
	// installer for the current platform.
	ErrNoInstaller = errors.New("update: release has no installable artifact")
	// ErrBusy reports a second update operation started while one is running.
	ErrBusy = errors.New("update: another update operation is running")
	// ErrNotNewer reports a release that is not newer than the installed
	// version. Downgrades are never offered.
	ErrNotNewer = errors.New("update: release is not newer than the installed version")
	// ErrAppImageMissing reports an AppImage install without a target path.
	ErrAppImageMissing = errors.New("update: APPIMAGE path is not set")
)
