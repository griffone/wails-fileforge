# Release and signature flow

FileForge ships GitHub releases whose artifacts are signed with an ed25519 key.
The app pins the matching public key at build time, so an updater can reject any
release it did not sign. This document covers the whole path: bump, tag, build,
sign, verify, publish.

The signer lives in `scripts/releasesign`. The Taskfile wraps it as
`release:keygen`, `release:sign`, `release:verify` and `release:dist`.

## Trust model

- **One key, pinned out of band.** `internal/update.PublicKey`, stamped into
  the binary from `FILEFORGE_UPDATE_PUBKEY`, is the only trust anchor. A
  release cannot choose its own key.
- **Fail closed.** When a key is pinned, a release without a signature is
  rejected. There is no unsigned fallback.
- **Detached signature per artifact.** GitHub's bundled provider only fills a
  digest from `SHA256SUMS`, and Wails' updater assumes it can swap the running
  binary. FileForge ships a small provider of its own
  (`internal/update/provider.go`) that reads `releases/latest`, selects one
  asset for the running platform, fetches `SHA256SUMS` and `<artifact>.sig`,
  and verifies both during a streaming download before any install step runs.
- **No key means no updates.** A build without `FILEFORGE_UPDATE_PUBKEY` has an
  empty `internal/update.PublicKey`. Update checking is disabled and Settings →
  About says so.

## In-app update contract

`internal/update` owns the check, download, verify and install flow;
`internal/app.UpdateService` binds it to Wails. The installability gate is
computed by the app from the build identity and install type, never from the
release feed:

| Build / install | Offered? | Method |
| --- | --- | --- |
| development or non-release build | no: "development build" | — |
| release build without a pinned key | no: "no signing key" | — |
| platform other than linux/amd64 or windows/amd64 | no: "unsupported" | — |
| Linux deb/rpm/Arch (`$APPIMAGE` unset) | no: "use your package manager" | — |
| Linux AppImage (`$APPIMAGE` set) | yes | replace the file atomically and relaunch |
| Windows amd64 | yes | run the verified NSIS installer, then quit |

`InstallUpdate` requires a verified download and a prior
`GrantInstallConsent`; there is no silent install. `FILEFORGE_GITHUB_TOKEN` may
carry a token for private repositories or higher rate limits, and is never
logged.

## Key generation (once, offline)

```sh
task release:keygen OUT=~/.config/fileforge/release
```

This writes:

| File | Contents | Mode |
| --- | --- | --- |
| `<OUT>.key` | PKCS#8 PEM ed25519 private key | `0600` |
| `<OUT>.pub` | base64 of the raw 32-byte public key | `0644` |

It also prints the base64 public key on stdout. The private key must stay
offline: not in the repository, not in the release directory, not in a CI log.
The tool refuses to write a key inside the Git working tree.

Back the private key up somewhere offline. **Never regenerate it casually.** A
new key invalidates every signature made with the old one, and every installed
app that pinned the old key will reject releases signed with the new key.

To rotate, first ship an app release (signed with the old key) that pins the new
public key, wait for adoption, and only then start signing releases with the new
private key.

## Signature format

`<artifact>.sig` contains **standard base64 of the 64-byte ed25519 signature**,
followed by a newline:

```
<base64(ed25519_sign(sha256(artifact)))>\n
```

- The signed message is the artifact's **raw SHA-256 digest** (32 bytes), not
  the file bytes. That is exactly what the Wails updater computes during
  download before it calls the verifier.
- The signature is deterministic: no timestamp, no key id, no JSON envelope.
  Signing the same artifact twice yields byte-identical files.
- `release:verify` accepts standard or unpadded base64 and rejects anything
  that does not decode to exactly 64 bytes.

## Build with the pinned key

`scripts/versionflags` renders every `-X` flag used by the build targets. Set
the environment variable before building so `internal/update.PublicKey` is
stamped in:

```sh
export FILEFORGE_UPDATE_PUBKEY="$(cat ~/.config/fileforge/release.pub)"
go run ./scripts/versionflags   # prints the -X flags, including the key
task package                    # or task build, task build:cli, task build:windows
```

Without the variable the build still succeeds, with an empty key. That build
cannot verify updates and Settings reports the feature as disabled. A malformed
key fails the build instead of producing a binary with an unusable trust anchor.

## Cutting a release

1. **Bump the version.** `wails.json` is the source of truth for the release
   version. Update `version` and `info.version`, then keep `build/config.yml`
   and `build/windows/info.json` in sync (the version tests enforce this).

   ```sh
   # edit wails.json, build/config.yml, build/windows/info.json
   go test ./internal/version/ -count=1
   ```

2. **Verify the tree, then tag.**

   ```sh
   task verify
   git add -A && git commit -m "release: v1.2.0"
   git tag v1.2.0
   git push origin feat/v1.2-stabilization --tags
   ```

3. **Build with the pinned key.**

   ```sh
   export FILEFORGE_UPDATE_PUBKEY="$(cat ~/.config/fileforge/release.pub)"
   task package
   ```

4. **Checksums and signatures.**

   ```sh
   task checksums
   task release:sign KEY=~/.config/fileforge/release.key
   task release:verify PUBKEY="$(cat ~/.config/fileforge/release.pub)"
   ```

   `release:sign` prints each artifact it signed. It refuses a key path inside
   the repository. `release:verify` exits non-zero on the first mismatch.

5. **Assemble the upload directory.**

   ```sh
   task release:dist \
     KEY=~/.config/fileforge/release.key \
     PUBKEY="$(cat ~/.config/fileforge/release.pub)"
   ```

   This regenerates `SHA256SUMS`, signs every existing artifact, verifies every
   signature, and copies the assets into `dist/<version>/` (the version comes
   from `wails.json`). `dist/` is git-ignored.

6. **Publish.**

   ```sh
   gh release create v1.2.0 dist/1.2.0/* \
     --repo griffone/wails-fileforge \
     --title "FileForge 1.2.0" \
     --notes-file RELEASE_NOTES.md
   ```

   The directory is ready as-is: `gh` uploads the artifacts, `SHA256SUMS` and
   every `.sig` sidecar. Do not upload a release that skipped step 4; a pinned
   key makes it uninstallable by design.

## Verifying by hand

```sh
# Sign a single artifact with the offline key.
go run ./scripts/releasesign -sign bin/fileforge-cli -key ~/.config/fileforge/release.key

# Verify it against the pinned key.
go run ./scripts/releasesign -verify bin/fileforge-cli \
  -sig bin/fileforge-cli.sig \
  -pubkey "$(cat ~/.config/fileforge/release.pub)"
```

Flip one byte in the artifact and rerun the verify command: it exits non-zero
and prints `signature ... does not match artifact ...`.

## What is in a release

Artifacts keep platform and arch in the filename:

```
fileforge-desktop-amd64-installer.exe
fileforge-desktop-windows-amd64.zip
fileforge-desktop-x86_64.AppImage
fileforge-desktop.deb
fileforge-desktop.rpm
fileforge-desktop.pkg.tar.zst
fileforge-cli
fileforge-cli.exe
SHA256SUMS
```

Each existing artifact is accompanied by `<name>.sig`. `SHA256SUMS` is not
signed separately: the provider reads it for the digest, and the artifact's own
signature is what the updater verifies.

## Troubleshooting

- `refusing "...": it is inside the repository` — move the private key outside
  the checkout. This is intentional.
- `public key is not valid base64` — use the exact line from `<OUT>.pub` or the
  `-keygen` stdout, without edits.
- `signature ... does not match artifact ...` — the artifact was modified after
  signing, or the key does not match the pinned public key. Rebuild, re-sign and
  re-verify; never publish the mismatched pair.
- Settings → About shows `Not pinned · updates disabled` — the build did not
  receive `FILEFORGE_UPDATE_PUBKEY`. Rebuild with the variable set.
