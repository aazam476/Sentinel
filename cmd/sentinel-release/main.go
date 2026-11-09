// SPDX-License-Identifier: Apache-2.0

// Command sentinel-release makes and checks Sentinel's release artifacts
// (SEC-30, SEC-33). It is a maintainer's tool, run by the release workflow;
// it isn't shipped.
//
//	sentinel-release keygen <private-key-file>
//	sentinel-release sign --name N --version V --os O --arch A <artifact>
//	sentinel-release verify [--name N] [--os O] [--arch A] <artifact>
//	sentinel-release sums <file>...
//	sentinel-release sbom --name N --version V [--npm-lock file] <binary>
//	sentinel-release check-key
//
// sign reads the private key from $SENTINEL_RELEASE_KEY, or from the file
// --key names. verify checks against the keys compiled into this build,
// the same ones the server and agent trust.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/aazam476/sentinel/internal/shared/release"
)

const (
	exitOK         = 0
	exitFailure    = 1
	exitUsageError = 2
)

// keyVariable holds the private key for signing, in the release workflow.
const keyVariable = "SENTINEL_RELEASE_KEY"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// errUsage marks a command line that can't be run.
var errUsage = errors.New("usage")

func run(args []string, stdout, stderr io.Writer) int {
	commands := map[string]func([]string, io.Writer) error{
		"keygen": keygen, "sign": sign, "verify": verify, "sums": sums, "sbom": sbom, "check-key": checkKey,
	}
	if len(args) == 0 || commands[args[0]] == nil {
		_, _ = fmt.Fprintln(stderr, "usage: sentinel-release keygen|sign|verify|sums|sbom|check-key ...")
		return exitUsageError
	}
	err := commands[args[0]](args[1:], stdout)
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, errUsage):
		_, _ = fmt.Fprintln(stderr, "sentinel-release "+args[0]+":", err)
		return exitUsageError
	}
	_, _ = fmt.Fprintln(stderr, "sentinel-release "+args[0]+":", err)
	return exitFailure
}

// parse parses flags and returns the positional arguments, which must
// number at least minimum.
func parse(flags *flag.FlagSet, args []string, minimum int) ([]string, error) {
	flags.SetOutput(io.Discard)
	if err := flags.Parse(args); err != nil {
		return nil, fmt.Errorf("%w: %w", errUsage, err)
	}
	if flags.NArg() < minimum {
		return nil, fmt.Errorf("%w: give the file to work on", errUsage)
	}
	return flags.Args(), nil
}

func keygen(args []string, stdout io.Writer) error {
	files, err := parse(flag.NewFlagSet("keygen", flag.ContinueOnError), args, 1)
	if err != nil {
		return err
	}
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		return fmt.Errorf("make a key: %w", err)
	}
	out, err := os.OpenFile(files[0], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("write the private key: %w", err)
	}
	_, err = io.WriteString(out, release.FormatPrivateKey(private))
	if err := errors.Join(err, out.Close()); err != nil {
		return fmt.Errorf("write the private key: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "Private key written to %s: keep it offline.\nAdd this line to internal/shared/release/keys/production.pub:\n%s\n",
		files[0], release.FormatPublicKey(public))
	return nil
}

func privateKey(file string) (ed25519.PrivateKey, error) {
	text := os.Getenv(keyVariable)
	if file != "" {
		content, err := os.ReadFile(file) //nolint:gosec // G304: the file the maintainer named
		if err != nil {
			return nil, fmt.Errorf("read the private key: %w", err)
		}
		text = string(content)
	}
	if text == "" {
		return nil, fmt.Errorf("%w: set $%s or pass --key", errUsage, keyVariable)
	}
	return release.ParsePrivateKey(text)
}

func sign(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("sign", flag.ContinueOnError)
	keyFile := flags.String("key", "", "the private key file (default: $"+keyVariable+")")
	name, version := flags.String("name", "", "product name"), flags.String("version", "", "version")
	osName, arch := flags.String("os", "any", "operating system"), flags.String("arch", "any", "architecture")
	files, err := parse(flags, args, 1)
	if err != nil {
		return err
	}
	key, err := privateKey(*keyFile)
	if err != nil {
		return err
	}
	for _, file := range files {
		content, err := os.ReadFile(file) //nolint:gosec // G304: an artifact the maintainer named
		if err != nil {
			return fmt.Errorf("read %s: %w", file, err)
		}
		manifest, err := release.ManifestFor(bytes.NewReader(content), *name, *version, *osName, *arch)
		if err != nil {
			return err
		}
		signature, err := release.Sign(manifest, key)
		if err != nil {
			return err
		}
		if err := os.WriteFile(file+".sig", signature, 0o644); err != nil { //nolint:gosec // G306: signatures are public
			return fmt.Errorf("write the signature: %w", err)
		}
		_, _ = fmt.Fprintf(stdout, "signed %s\n", file)
	}
	return nil
}

func verify(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	var want release.Expect
	flags.StringVar(&want.Name, "name", "", "the product it must be")
	flags.StringVar(&want.OS, "os", "", "the operating system it must be for")
	flags.StringVar(&want.Arch, "arch", "", "the architecture it must be for")
	files, err := parse(flags, args, 1)
	if err != nil {
		return err
	}
	signature, err := os.ReadFile(files[0] + ".sig")
	if err != nil {
		return fmt.Errorf("read the signature: %w", err)
	}
	artifact, err := os.Open(files[0])
	if err != nil {
		return fmt.Errorf("read %s: %w", files[0], err)
	}
	defer func() { _ = artifact.Close() }() // read-only
	manifest, err := release.Verify(artifact, signature, want)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "%s: good signature for %s %s %s/%s\n", files[0], manifest.Name, manifest.Version, manifest.OS, manifest.Arch)
	return nil
}

// sums prints a SHA256SUMS file: each file's SHA-256 and base name.
func sums(args []string, stdout io.Writer) error {
	files, err := parse(flag.NewFlagSet("sums", flag.ContinueOnError), args, 1)
	if err != nil {
		return err
	}
	for _, file := range files {
		content, err := os.ReadFile(file) //nolint:gosec // G304: an artifact the maintainer named
		if err != nil {
			return fmt.Errorf("read %s: %w", file, err)
		}
		digest := sha256.Sum256(content)
		_, _ = fmt.Fprintf(stdout, "%s  %s\n", hex.EncodeToString(digest[:]), filepath.Base(file))
	}
	return nil
}

func sbom(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("sbom", flag.ContinueOnError)
	name, version := flags.String("name", "", "product name"), flags.String("version", "", "version")
	npmLock := flags.String("npm-lock", "", "the web UI's package-lock.json, for the server")
	files, err := parse(flags, args, 1)
	if err != nil {
		return err
	}
	binary, err := os.ReadFile(files[0])
	if err != nil {
		return fmt.Errorf("read %s: %w", files[0], err)
	}
	info, err := buildinfo.Read(bytes.NewReader(binary))
	if err != nil {
		return fmt.Errorf("read %s's build information: %w", files[0], err)
	}
	input := release.SBOMInput{Name: *name, Version: *version, Binary: binary, Build: info, Created: created()}
	if *npmLock != "" {
		if input.NPMLock, err = os.ReadFile(*npmLock); err != nil {
			return fmt.Errorf("read %s: %w", *npmLock, err)
		}
	}
	document, err := release.SBOM(input)
	if err != nil {
		return err
	}
	_, err = stdout.Write(append(document, '\n'))
	return err
}

// created is $SOURCE_DATE_EPOCH, for reproducible output, or now.
func created() time.Time {
	if seconds, err := strconv.ParseInt(os.Getenv("SOURCE_DATE_EPOCH"), 10, 64); err == nil {
		return time.Unix(seconds, 0)
	}
	return time.Now()
}

// checkKey fails for a build that trusts only the development key, so the
// release workflow can't publish one.
func checkKey(args []string, stdout io.Writer) error {
	if _, err := parse(flag.NewFlagSet("check-key", flag.ContinueOnError), args, 0); err != nil {
		return err
	}
	if release.IsDevelopmentBuild() {
		return errors.New("no production release key is in internal/shared/release/keys/production.pub; run `sentinel-release keygen` offline and add its public key")
	}
	_, _ = fmt.Fprintf(stdout, "%d production release key(s) compiled in\n", len(release.TrustedKeys()))
	return nil
}
