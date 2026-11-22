#!/usr/bin/env bash
# Regression checks for the commands copied from docs/deployment/systemd.md.
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
systemd_doc="$root/docs/deployment/systemd.md"
security_doc="$root/docs/security.md"

require() {
  local needle=$1
  local file=$2
  if ! grep -Fq -- "$needle" "$file"; then
    printf 'missing %q in %s\n' "$needle" "$file" >&2
    exit 1
  fi
}

reject() {
  local needle=$1
  local file=$2
  if grep -Fq -- "$needle" "$file"; then
    printf 'obsolete %q remains in %s\n' "$needle" "$file" >&2
    exit 1
  fi
}

require 'opnsense2otel_Linux_x86_64.tar.gz' "$systemd_doc"
require 'opnsense2otel_Linux_arm64.tar.gz' "$systemd_doc"
require '{{- title .Os }}_' "$root/.goreleaser.yml"
require 'if eq .Arch "amd64" }}x86_64' "$root/.goreleaser.yml"
require 'scripts/systemd/verify-release.sh' "$systemd_doc"
require "git clone --depth 1 --branch \"\$VERSION\"" "$systemd_doc"
require "INSTALL_ROOT=\$(mktemp -d)" "$systemd_doc"
require "UPGRADE_ROOT=\$(mktemp -d)" "$systemd_doc"
require "The verifier accepts signatures from \`https://github.com/rknightion/.github/.github/workflows/binaries.yml\` at any full 40-character lowercase hexadecimal commit SHA." "$systemd_doc"
require "The certificate must identify \`rknightion/opnsense2otel\` as the repository that called the workflow" "$systemd_doc"
require "\`https://token.actions.githubusercontent.com\` as the OIDC issuer." "$systemd_doc"
if LC_ALL=C grep -Eq -- '(^|[^[:xdigit:]])[[:xdigit:]]{40}([^[:xdigit:]]|$)' "$systemd_doc"; then
  printf 'guide %s names a pinned commit SHA\n' "$systemd_doc" >&2
  exit 1
fi
require 'sudo install -d -o root -g opnsense2otel -m 0710' "$systemd_doc"
require 'sudo install -d -o root -g opnsense2otel -m 0710' "$security_doc"
require "printf '%s\\n' 'your-api-key'" "$systemd_doc"
require "printf '%s\\n' 'your-api-secret'" "$systemd_doc"
require "printf '%s\\n' 'your-api-key'" "$security_doc"
require "printf '%s\\n' 'your-api-secret'" "$security_doc"
require 'sudo install -o root -g opnsense2otel -m 0640' "$systemd_doc"
require 'sudo install -o root -g opnsense2otel -m 0640' "$security_doc"
require 'systemctl is-active --quiet opnsense2otel' "$systemd_doc"
require '/-/healthy' "$systemd_doc"
reject 'opnsense2otel_linux_amd64' "$systemd_doc"

for script in \
  "$root/scripts/systemd/verify-release.sh" \
  "$root/scripts/systemd/test_secret_permissions.sh"; do
  if [[ ! -x "$script" ]]; then
    printf 'required script is not executable: %s\n' "$script" >&2
    exit 1
  fi
done

# The verifier must never accept an executable left by an earlier extraction.
# This guard runs before external tool checks or downloads, so it is deterministic.
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
stale_output="$scratch/stale"
mkdir "$stale_output"
touch "$stale_output/opnsense2otel"
if "$root/scripts/systemd/verify-release.sh" v0.0.0 x86_64 "$stale_output" \
    >"$stale_output/stdout" 2>"$stale_output/stderr"; then
  printf 'verifier accepted an existing output binary\n' >&2
  exit 1
fi
require 'refusing to reuse existing output binary' "$stale_output/stderr"

# Run the real verifier with local stand-ins for the downloads and cosign.
mkdir "$scratch/bin" "$scratch/assets" "$scratch/archive"
archive=opnsense2otel_Linux_x86_64.tar.gz
cat >"$scratch/archive/opnsense2otel" <<'EOF'
#!/usr/bin/env bash
[[ $# == 1 && $1 == --version ]] || exit 1
printf 'fixture version\n'
EOF
chmod +x "$scratch/archive/opnsense2otel"
tar -czf "$scratch/assets/$archive" -C "$scratch/archive" opnsense2otel
(cd "$scratch/assets" && sha256sum "$archive" >checksums.txt)
printf '{}\n' >"$scratch/assets/checksums.txt.sigstore.json"

cat >"$scratch/bin/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
asset=${3##*/}
[[ $# == 3 && $1 == -fsSLo && $3 == "https://github.com/rknightion/opnsense2otel/releases/download/v0.0.0/$asset" ]]
case "$asset" in
  checksums.txt|checksums.txt.sigstore.json|opnsense2otel_Linux_x86_64.tar.gz)
    cp -- "$SYSTEMD_VERIFIER_FIXTURES/assets/$asset" "$2" ;;
  *) exit 1 ;;
esac
EOF
chmod +x "$scratch/bin/curl"

cat >"$scratch/folded.yml" <<'EOF'
"jobs":
  "binaries":
    uses: >-
      rknightion/.github/.github/workflows/binaries.yml@0123456789abcdef0123456789abcdef01234567
EOF

# Validate argv and decoded YAML in Go. Shell substitution would drop some
# decoded bytes before the full workflow reference could be checked.
cat >"$scratch/cosign.go" <<'EOF'
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"go.yaml.in/yaml/v2"
)

func workflowRef(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	decoder := yaml.NewDecoder(file)
	decoder.SetStrict(true)
	var document map[interface{}]interface{}
	if err := decoder.Decode(&document); err != nil {
		return "", err
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		return "", fmt.Errorf("expected one YAML document")
	}
	jobs, _ := document["jobs"].(map[interface{}]interface{})
	binaries, _ := jobs["binaries"].(map[interface{}]interface{})
	ref, ok := binaries["uses"].(string)
	if !ok {
		return "", fmt.Errorf("missing jobs.binaries.uses string")
	}
	return ref, nil
}

func check(args []string) error {
	if len(args) != 10 {
		return fmt.Errorf("unexpected cosign argv: %q", args)
	}
	downloadDir := filepath.Dir(args[9])
	want := []string{
		"verify-blob",
		"--bundle", filepath.Join(downloadDir, "checksums.txt.sigstore.json"),
		"--certificate-identity-regexp", `^https://github\.com/rknightion/\.github/\.github/workflows/binaries\.yml@[0-9a-f]{40}$`,
		"--certificate-github-workflow-repository", "rknightion/opnsense2otel",
		"--certificate-oidc-issuer", "https://token.actions.githubusercontent.com",
		filepath.Join(downloadDir, "checksums.txt"),
	}
	if !slices.Equal(args, want) {
		return fmt.Errorf("unexpected cosign argv: %q", args)
	}
	identity := regexp.MustCompile(args[4])
	fixtures := os.Getenv("SYSTEMD_VERIFIER_FIXTURES")
	for _, path := range []string{
		".github/workflows/release-please.yml",
		".github/workflows/auto-rc.yml",
		filepath.Join(fixtures, "folded.yml"),
	} {
		ref, err := workflowRef(path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if !identity.MatchString("https://github.com/" + ref) {
			return fmt.Errorf("%s: verifier rejects workflow ref %q", path, ref)
		}
	}

	const workflowDir = "https://github.com/rknightion/.github/.github/workflows/"
	const pin = "fedcba9876543210fedcba9876543210fedcba98"
	if !identity.MatchString(workflowDir + "binaries.yml@" + pin) {
		return fmt.Errorf("verifier rejects a second full workflow pin")
	}
	for _, invalid := range []string{
		workflowDir + "release.yml@" + pin,
		workflowDir + "binaries.yml@refs/heads/main",
		workflowDir + "binaries.yml@" + pin[:39],
		workflowDir + "binaries.yml@" + pin + "0",
		workflowDir + "binaries.yml@" + pin + "\n",
	} {
		if identity.MatchString(invalid) {
			return fmt.Errorf("verifier accepts invalid workflow identity %q", invalid)
		}
	}
	return os.WriteFile(filepath.Join(fixtures, "cosign-checked"), nil, 0600)
}

func main() {
	if err := check(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
EOF

cd "$root"
go build -mod=vendor -o "$scratch/bin/cosign" "$scratch/cosign.go"
PATH="$scratch/bin:$PATH" SYSTEMD_VERIFIER_FIXTURES="$scratch" \
  "$root/scripts/systemd/verify-release.sh" v0.0.0 x86_64 "$scratch/verified" >"$scratch/stdout"
test -f "$scratch/cosign-checked"
test -x "$scratch/verified/opnsense2otel"
require 'fixture version' "$scratch/stdout"

printf 'systemd documentation regression checks passed\n'
