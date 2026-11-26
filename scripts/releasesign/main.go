// Command releasesign creates and verifies the detached ed25519 signatures
// that protect FileForge release artifacts.
//
// Usage:
//
//	releasesign -keygen -out <prefix>
//	releasesign -sign <artifact>... -key <private-key>
//	releasesign -verify <artifact> -sig <signature-file> -pubkey <base64>
//
// -keygen writes <prefix>.key (mode 0600) and <prefix>.pub, and prints the
// base64 public key on stdout for build-time injection. -sign writes
// <artifact>.sig next to every artifact and prints each artifact it signed.
// -verify exits non-zero when the artifact, the signature or the key does not
// match.
//
// The signature is always ed25519 over the artifact's raw SHA-256 digest: the
// same digest the Wails updater computes during download.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	modeKeygen = "keygen"
	modeSign   = "sign"
	modeVerify = "verify"
)

type options struct {
	mode      string
	out       string
	key       string
	sig       string
	pubkey    string
	artifacts []string
}

func main() {
	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "releasesign: %v\n\n", err)
		usage(os.Stderr)
		os.Exit(2)
	}

	if err := run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "releasesign: %v\n", err)
		os.Exit(1)
	}
}

func run(opts options) error {
	switch opts.mode {
	case modeKeygen:
		return runKeygen(opts)
	case modeSign:
		return runSign(opts)
	case modeVerify:
		return runVerify(opts)
	default:
		return errors.New("choose exactly one mode: -keygen, -sign or -verify")
	}
}

func runKeygen(opts options) error {
	if len(opts.artifacts) > 0 {
		return errors.New("-keygen does not take artifact arguments")
	}
	if strings.TrimSpace(opts.out) == "" {
		return errors.New("-keygen requires -out <prefix>")
	}
	for _, suffix := range []string{privateKeySuffix, publicKeySuffix} {
		if err := ensureOutsideRepository(opts.out + suffix); err != nil {
			return err
		}
	}

	publicKey, err := generateKeyPair(opts.out)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "releasesign: private key %s (mode 0600)\n", opts.out+privateKeySuffix)
	fmt.Fprintf(os.Stderr, "releasesign: public key %s\n", opts.out+publicKeySuffix)
	fmt.Fprintln(os.Stdout, publicKey)

	return nil
}

func runSign(opts options) error {
	if strings.TrimSpace(opts.key) == "" {
		return errors.New("-sign requires -key <private-key>")
	}
	if len(opts.artifacts) == 0 {
		return errors.New("-sign requires at least one artifact")
	}
	if err := ensureOutsideRepository(opts.key); err != nil {
		return err
	}

	key, err := loadPrivateKey(opts.key)
	if err != nil {
		return err
	}

	signaturePaths, err := signArtifacts(key, opts.artifacts)
	if err != nil {
		return err
	}

	for index, artifact := range opts.artifacts {
		fmt.Printf("signed %s -> %s\n", artifact, signaturePaths[index])
	}

	return nil
}

func runVerify(opts options) error {
	if len(opts.artifacts) != 1 {
		return errors.New("-verify takes exactly one artifact")
	}
	if strings.TrimSpace(opts.sig) == "" {
		return errors.New("-verify requires -sig <signature-file>")
	}
	if strings.TrimSpace(opts.pubkey) == "" {
		return errors.New("-verify requires -pubkey <base64>")
	}

	if err := verifyArtifact(opts.artifacts[0], opts.sig, opts.pubkey); err != nil {
		return err
	}

	fmt.Printf("verified %s\n", opts.artifacts[0])
	return nil
}

func parseArgs(args []string) (options, error) {
	var opts options

	for index := 0; index < len(args); index++ {
		argument := args[index]

		switch argument {
		case "-h", "--help", "-help":
			return options{}, errors.New("help requested")
		case "-keygen":
			if err := setMode(&opts, modeKeygen); err != nil {
				return options{}, err
			}
		case "-sign":
			if err := setMode(&opts, modeSign); err != nil {
				return options{}, err
			}
		case "-verify":
			if err := setMode(&opts, modeVerify); err != nil {
				return options{}, err
			}
		case "-out":
			value, err := nextValue(args, &index, argument)
			if err != nil {
				return options{}, err
			}
			opts.out = value
		case "-key":
			value, err := nextValue(args, &index, argument)
			if err != nil {
				return options{}, err
			}
			opts.key = value
		case "-sig":
			value, err := nextValue(args, &index, argument)
			if err != nil {
				return options{}, err
			}
			opts.sig = value
		case "-pubkey":
			value, err := nextValue(args, &index, argument)
			if err != nil {
				return options{}, err
			}
			opts.pubkey = value
		default:
			if strings.HasPrefix(argument, "-") {
				return options{}, fmt.Errorf("unknown flag %q", argument)
			}
			opts.artifacts = append(opts.artifacts, argument)
		}
	}

	if opts.mode == "" {
		return options{}, errors.New("choose one mode: -keygen, -sign or -verify")
	}

	return opts, nil
}

func setMode(opts *options, mode string) error {
	if opts.mode != "" && opts.mode != mode {
		return fmt.Errorf("conflicting modes: -%s and -%s", opts.mode, mode)
	}
	opts.mode = mode
	return nil
}

func nextValue(args []string, index *int, flag string) (string, error) {
	*index = *index + 1
	if *index >= len(args) {
		return "", fmt.Errorf("%s requires a value", flag)
	}
	value := args[*index]
	if strings.HasPrefix(value, "-") {
		return "", fmt.Errorf("%s requires a value, got flag %q", flag, value)
	}
	return value, nil
}

func usage(writer io.Writer) {
	fmt.Fprint(writer, `usage:
  releasesign -keygen -out <prefix>
  releasesign -sign <artifact>... -key <private-key>
  releasesign -verify <artifact> -sig <signature-file> -pubkey <base64>

-keygen writes <prefix>.key (mode 0600) and <prefix>.pub, then prints the
base64 public key on stdout. A key inside the repository is refused.
-sign writes <artifact>.sig for every artifact and prints what it signed.
-verify exits non-zero when the artifact, signature or public key mismatches.
`)
}
