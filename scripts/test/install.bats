#!/usr/bin/env bats
# Tests of scripts/install.sh (PLT-F-043). bats-core 1.x, pinned in CI and in the ticket's verify line:
#   npx --yes bats@1.13.0 scripts/test/install.bats
#
# Nothing here touches the real system or the network. Each test has its own HOME, XDG_CONFIG_HOME
# and install folder under $BATS_TEST_TMPDIR, serves a fake release from a folder through
# LOCAL_SERVER_RELEASE_BASE=file://..., and runs the script with a PATH that holds only stubs and a
# short list of ordinary tools (so a developer's real ollama, tailscale, brew or sudo are never
# found). The stub curl passes file:// and the local Ollama address through to the real curl,
# answers the release URLs from the fake release folder, writes a harmless stub script for the
# Ollama and Tailscale installer URLs, and refuses every other https URL.
#
# Every test sets LOCAL_SERVER_TEST_ANSWERS: one answer per line, consumed in order. Without it the
# script would read the developer's terminal and wait. Questions of a default install on Linux,
# with ollama and tailscale present (the stubs of setup) and the install folder on PATH: first run.

HELPER_TOOLS="awk basename cat chmod cp dirname grep head mkdir mktemp mv rm sed sh sleep tail tr"
HASH_TOOLS="sha256sum shasum openssl"

setup() {
  SCRIPT="$BATS_TEST_DIRNAME/../install.sh"
  ROOT="$BATS_TEST_TMPDIR"
  SH_BIN=${INSTALL_TEST_SH:-$(command -v sh)}
  PYTHON3=$(command -v python3 || true)
  SETSID=$(command -v setsid || true)
  REAL_CURL=$(command -v curl)
  RELEASE="$ROOT/release"
  STUBS="$ROOT/stubs"
  TOOLS="$ROOT/tools"
  export REAL_CURL
  export FAKE_RELEASE_DIR="$RELEASE"

  export HOME="$ROOT/home"
  export XDG_CONFIG_HOME="$HOME/.config"
  export LOCAL_SERVER_PREFIX="$HOME/.local/bin"
  export LOCAL_SERVER_RELEASE_BASE="file://$RELEASE"
  export LOCAL_SERVER_TEST_ANSWERS="$ROOT/answers"
  export LOCAL_SERVER_OLLAMA_WAIT=0
  export OLLAMA_HOST="http://127.0.0.1:1"
  export SHELL=/bin/zsh
  unset LS_DATA_DIR LOCAL_SERVER_VERSION ZDOTDIR SUDO_USER FAKE_UNAME_S FAKE_UNAME_M FAKE_UID

  export FAKE_LS_LOG="$ROOT/fake-ls.log"
  export FAKE_LS_STATE="$ROOT/service-state"
  export FAKE_LS_DEFAULT_DATA="$XDG_CONFIG_HOME/local-server"
  export STUB_LOG="$ROOT/stub.log"

  mkdir -p "$HOME" "$STUBS" "$FAKE_LS_STATE"
  : >"$LOCAL_SERVER_TEST_ANSWERS"
  make_tools "$TOOLS" with-hash
  make_tools "$ROOT/tools-no-hash" no-hash
  write_stubs
  add_stub ollama
  add_stub tailscale
  write_fake_template
  make_release v1.0.0
  # The install folder is on PATH, so the PATH question is not asked unless a test removes it.
  TEST_PATH="$STUBS:$TOOLS:$LOCAL_SERVER_PREFIX"
}

teardown() {
  if [ -n "${SERVER_PID:-}" ]; then
    kill "$SERVER_PID" 2>/dev/null || true
  fi
}

# --- helpers ----------------------------------------------------------------------------------

make_tools() { # DIR with-hash|no-hash
  local dir=$1 t p
  mkdir -p "$dir"
  for t in $HELPER_TOOLS; do
    p=$(command -v "$t" 2>/dev/null) || continue
    ln -sf "$p" "$dir/$t"
  done
  if [ "$2" = with-hash ]; then
    for t in $HASH_TOOLS; do
      p=$(command -v "$t" 2>/dev/null) || continue
      ln -sf "$p" "$dir/$t"
    done
  fi
}

write_stubs() {
  cat >"$STUBS/uname" <<'EOF'
#!/bin/sh
case "$1" in
  -m) echo "${FAKE_UNAME_M:-x86_64}" ;;
  *) echo "${FAKE_UNAME_S:-Linux}" ;;
esac
EOF
  cat >"$STUBS/id" <<'EOF'
#!/bin/sh
echo "${FAKE_UID:-1000}"
EOF
  cat >"$STUBS/sudo" <<'EOF'
#!/bin/sh
echo "sudo $*" >> "$STUB_LOG"
exec "$@"
EOF
  cat >"$STUBS/brew" <<'EOF'
#!/bin/sh
echo "brew $*" >> "$STUB_LOG"
EOF
  cat >"$STUBS/curl" <<'EOF'
#!/bin/sh
# Test stub. Passes file:// and the local Ollama address to the real curl; answers the release
# URLs from FAKE_RELEASE_DIR; writes a harmless stub script for the Ollama and Tailscale installers.
url=""
out=""
prev=""
for a in "$@"; do
  if [ "$prev" = "-o" ]; then out=$a; fi
  case $a in http://* | https://* | file://*) url=$a ;; esac
  prev=$a
done
echo "curl $url" >> "$STUB_LOG"
case $url in
  file://* | http://127.0.0.1:*) exec "$REAL_CURL" "$@" ;;
  https://ollama.com/* | https://tailscale.com/*)
    printf '#!/bin/sh\necho "installer ran: %s" >> "%s"\n' "$url" "$STUB_LOG" > "$out"
    exit 0
    ;;
  https://github.com/inCoderKaka/local-server/releases/*)
    f="$FAKE_RELEASE_DIR/${url##*/}"
    if [ -f "$f" ]; then cp "$f" "$out"; exit 0; fi
    echo "curl: (22) The requested URL returned error: 404" >&2
    exit 22
    ;;
esac
echo "curl stub: network is blocked in tests: $url" >&2
exit 6
EOF
  chmod +x "$STUBS"/*
}

add_stub() { # NAME: a program that exists on PATH and logs that it ran
  printf '#!/bin/sh\necho "%s $*" >> "%s"\n' "$1" "$STUB_LOG" >"$STUBS/$1"
  chmod +x "$STUBS/$1"
}

# The fake local-server: prints its version, records every call with the version that ran it, keeps
# the service state in files, and creates local-server.db in the data folder on `serve`.
write_fake_template() {
  cat >"$ROOT/fake-ls.template" <<'EOF'
#!/bin/sh
echo "@VERSION@ $0 $*" >> "${FAKE_LS_LOG:-/dev/null}"
STATE=${FAKE_LS_STATE:-/nonexistent}
mode=user
case " $* " in *" --mode system "*) mode=system ;; esac
case "${1:-}" in
  --version) echo "local-server @VERSION@" ;;
  serve)
    shift
    dir=${FAKE_LS_DEFAULT_DATA:-}
    while [ $# -gt 0 ]; do
      case $1 in --data-dir) dir=$2; shift ;; esac
      shift
    done
    if [ -z "${FAKE_SERVE_NO_DB:-}" ]; then
      mkdir -p "$dir"
      : > "$dir/local-server.db"
    fi
    echo "local-server: first run. This is your recovery code: FAKE-CODE" >&2
    ;;
  service)
    case "${2:-}" in
      install)
        if [ -n "${FAKE_FAIL_SERVICE_INSTALL:-}" ]; then echo "fake: install failed" >&2; exit 1; fi
        : > "$STATE/$mode.installed" ;;
      start) : > "$STATE/$mode.running" ;;
      stop) rm -f "$STATE/$mode.running" ;;
      uninstall) rm -f "$STATE/$mode.installed" "$STATE/$mode.running" ;;
      status)
        shown=0
        for m in user system; do
          if [ -e "$STATE/$m.installed" ]; then
            shown=1
            if [ -e "$STATE/$m.running" ]; then
              echo "local-server background service, $m mode: installed, running"
            else
              echo "local-server background service, $m mode: installed, stopped"
            fi
          fi
        done
        if [ "$shown" = 0 ]; then echo "local-server background service: not installed (run \`local-server service install\`)"; fi
        ;;
    esac
    ;;
esac
exit 0
EOF
}

sha_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{ print $1 }'
  else
    shasum -a 256 "$1" | awk '{ print $1 }'
  fi
}

# make_release VERSION [OS ARCH]: a release folder like the real one: bare binaries without the
# execute bit (upload-artifact drops it) and SHA256SUMS with one line per binary.
make_release() {
  local v=$1 os=${2:-linux} arch=${3:-amd64} name n
  name="local-server_${os}_${arch}"
  rm -rf "$RELEASE"
  mkdir -p "$RELEASE"
  sed "s/@VERSION@/$v/g" "$ROOT/fake-ls.template" >"$RELEASE/$name"
  chmod 644 "$RELEASE/$name"
  : >"$RELEASE/SHA256SUMS"
  for n in local-server_darwin_arm64 local-server_darwin_amd64 local-server_linux_amd64 \
    local-server_linux_arm64 local-server_windows_amd64.exe local-server_windows_arm64.exe; do
    if [ "$n" = "$name" ]; then
      printf '%s  %s\n' "$(sha_of "$RELEASE/$name")" "$n" >>"$RELEASE/SHA256SUMS"
    else
      printf '%s  %s\n' "0000000000000000000000000000000000000000000000000000000000000000" "$n" >>"$RELEASE/SHA256SUMS"
    fi
  done
}

answers() { printf '%s\n' "$@" >"$LOCAL_SERVER_TEST_ANSWERS"; }

run_install() {
  run env PATH="$TEST_PATH" "$SH_BIN" "$SCRIPT" "$@" </dev/null
}

out_has() {
  if [[ "$output" != *"$1"* ]]; then
    { echo "expected the output to contain: $1"; echo "--- output ---"; echo "$output"; } >&3
    return 1
  fi
}

out_lacks() {
  if [[ "$output" == *"$1"* ]]; then
    { echo "expected the output NOT to contain: $1"; echo "--- output ---"; echo "$output"; } >&3
    return 1
  fi
}

log_has() { grep -Fq -- "$1" "$FAKE_LS_LOG"; }
log_lacks() { ! grep -Fq -- "$1" "$FAKE_LS_LOG" 2>/dev/null; }
stub_has() { grep -Fq -- "$1" "$STUB_LOG"; }
stub_lacks() { ! grep -Fq -- "$1" "$STUB_LOG" 2>/dev/null; }

BIN() { printf '%s' "$LOCAL_SERVER_PREFIX/local-server"; }

# --- install ----------------------------------------------------------------------------------

@test "happy install: verified, executable, service registered through the installed path, no data folder made" {
  [ ! -x "$RELEASE/local-server_linux_amd64" ]
  answers n
  run_install
  [ "$status" -eq 0 ]
  [ -x "$(BIN)" ]
  cmp "$RELEASE/local-server_linux_amd64" "$(BIN)"
  out_has "Checksum verified"
  log_has "$(BIN) service install"
  [ -e "$FAKE_LS_STATE/user.installed" ]
  # The script never creates the data folder; serve does (D-101, D-149).
  [ ! -e "$FAKE_LS_DEFAULT_DATA" ]
  # No first run happened, so the service is not started and the manual steps are printed.
  log_lacks "service start"
  out_has "First run"
  out_has "local-server serve"
  out_has "local-server service start"
  # Never under sudo in user mode.
  stub_lacks "sudo"
}

@test "install into the default folder when LOCAL_SERVER_PREFIX is not set" {
  unset LOCAL_SERVER_PREFIX
  TEST_PATH="$STUBS:$TOOLS:$HOME/.local/bin"
  answers n
  run_install
  [ "$status" -eq 0 ]
  [ -x "$HOME/.local/bin/local-server" ]
}

@test "SHA-256 mismatch aborts, installs nothing and registers nothing" {
  printf '%s  %s\n' "1111111111111111111111111111111111111111111111111111111111111111" local-server_linux_amd64 >"$RELEASE/SHA256SUMS"
  run_install
  [ "$status" -ne 0 ]
  out_has "does not match"
  out_has "Nothing was installed"
  [ ! -e "$(BIN)" ]
  [ ! -d "$LOCAL_SERVER_PREFIX" ]
  [ ! -e "$FAKE_LS_LOG" ]
  [ ! -e "$FAKE_LS_STATE/user.installed" ]
}

@test "a changed binary no longer matches its checksum line" {
  printf '\n# tampered\n' >>"$RELEASE/local-server_linux_amd64"
  run_install
  [ "$status" -ne 0 ]
  out_has "does not match"
  [ ! -e "$(BIN)" ]
  [ ! -e "$FAKE_LS_LOG" ]
}

@test "a checksum in capital letters still matches" {
  local h
  h=$(sha_of "$RELEASE/local-server_linux_amd64" | tr 'a-f' 'A-F')
  printf '%s  %s\n' "$h" local-server_linux_amd64 >"$RELEASE/SHA256SUMS"
  answers n
  run_install
  [ "$status" -eq 0 ]
  [ -x "$(BIN)" ]
}

@test "a missing SHA256SUMS line aborts and installs nothing" {
  grep -v 'local-server_linux_amd64' "$RELEASE/SHA256SUMS" >"$RELEASE/SUMS.new"
  mv "$RELEASE/SUMS.new" "$RELEASE/SHA256SUMS"
  run_install
  [ "$status" -ne 0 ]
  out_has "no line for local-server_linux_amd64"
  [ ! -e "$(BIN)" ]
  [ ! -e "$FAKE_LS_LOG" ]
}

@test "a missing SHA256SUMS file aborts" {
  rm "$RELEASE/SHA256SUMS"
  run_install
  [ "$status" -ne 0 ]
  [ ! -e "$(BIN)" ]
}

@test "no SHA-256 tool aborts before anything is downloaded or installed" {
  TEST_PATH="$STUBS:$ROOT/tools-no-hash"
  run_install
  [ "$status" -ne 0 ]
  out_has "no SHA-256 tool"
  [ ! -e "$(BIN)" ]
}

@test "an unsupported CPU aborts" {
  export FAKE_UNAME_M=riscv64
  run_install
  [ "$status" -ne 0 ]
  out_has "unsupported CPU type: riscv64"
  [ ! -e "$(BIN)" ]
}

@test "an unsupported operating system aborts" {
  export FAKE_UNAME_S=FreeBSD
  run_install
  [ "$status" -ne 0 ]
  out_has "unsupported operating system: FreeBSD"
  [ ! -e "$(BIN)" ]
}

@test "arm64 and aarch64 both pick the arm64 binary" {
  export FAKE_UNAME_M=aarch64
  make_release v1.0.0 linux arm64
  answers n
  run_install
  [ "$status" -eq 0 ]
  out_has "local-server_linux_arm64"
}

@test "refuses to run under sudo" {
  export FAKE_UID=0 SUDO_USER=somebody
  run_install
  [ "$status" -ne 0 ]
  out_has "do not run this with sudo"
  [ ! -e "$(BIN)" ]
}

@test "a service install that fails is reported, exits 1 and keeps the binary" {
  export FAKE_FAIL_SERVICE_INSTALL=1
  answers n
  run_install
  [ "$status" -eq 1 ]
  out_has "could not be registered"
  [ -x "$(BIN)" ]
}

# --- release URLs and --version ----------------------------------------------------------------

@test "without a release folder the latest release URL is used" {
  unset LOCAL_SERVER_RELEASE_BASE
  answers n
  run_install
  [ "$status" -eq 0 ]
  stub_has "curl https://github.com/inCoderKaka/local-server/releases/latest/download/local-server_linux_amd64"
  stub_has "curl https://github.com/inCoderKaka/local-server/releases/latest/download/SHA256SUMS"
}

@test "--version and LOCAL_SERVER_VERSION choose the tag" {
  unset LOCAL_SERVER_RELEASE_BASE
  answers n
  run_install --version v0.1.0-rc.1
  [ "$status" -eq 0 ]
  stub_has "releases/download/v0.1.0-rc.1/local-server_linux_amd64"
  : >"$STUB_LOG"
  rm "$(BIN)"
  answers n
  export LOCAL_SERVER_VERSION=v0.2.0
  run_install
  [ "$status" -eq 0 ]
  stub_has "releases/download/v0.2.0/SHA256SUMS"
}

@test "a 404 says that latest skips pre-releases and points at --version" {
  unset LOCAL_SERVER_RELEASE_BASE
  rm "$RELEASE/local-server_linux_amd64"
  run_install
  [ "$status" -ne 0 ]
  out_has "--version"
  out_has "pre-releases"
  out_has "nothing was installed"
  [ ! -e "$(BIN)" ]
}

@test "a missing file in a release folder also points at --version" {
  rm "$RELEASE/local-server_linux_amd64"
  run_install
  [ "$status" -ne 0 ]
  out_has "--version"
}

@test "a tag with odd characters is a usage error" {
  run_install --version 'v1;rm -rf'
  [ "$status" -eq 2 ]
}

@test "usage: help exits 0 and does not mention the test hook, unknown flags exit 2" {
  run_install --help
  [ "$status" -eq 0 ]
  out_has "Usage:"
  out_lacks "TEST_ANSWERS"
  run_install --nope
  [ "$status" -eq 2 ]
  run_install --system-service uninstall
  [ "$status" -eq 2 ]
}

# --- PATH line --------------------------------------------------------------------------------

@test "PATH question yes: one marker line is appended, and a second install adds no second line" {
  TEST_PATH="$STUBS:$TOOLS"
  printf 'alias a=b' >"$HOME/.zshrc"
  answers y n
  run_install
  [ "$status" -eq 0 ]
  [ "$(grep -c 'added by local-server install.sh' "$HOME/.zshrc")" -eq 1 ]
  # The profile had no final newline: the old line is intact and the new one is on its own line.
  [ "$(sed -n 1p "$HOME/.zshrc")" = "alias a=b" ]
  grep -Fq "export PATH=\"$LOCAL_SERVER_PREFIX:\$PATH\" # added by local-server install.sh" "$HOME/.zshrc"
  # Remove the binary (not the profile) and install again: still one line, and no question for it.
  rm "$(BIN)"
  answers y n
  run_install
  [ "$status" -eq 0 ]
  [ "$(grep -c 'added by local-server install.sh' "$HOME/.zshrc")" -eq 1 ]
}

@test "PATH question no: the profile is not touched and the manual line is shown" {
  TEST_PATH="$STUBS:$TOOLS"
  answers n n
  run_install
  [ "$status" -eq 0 ]
  [ ! -e "$HOME/.zshrc" ]
  out_has "export PATH=\"$LOCAL_SERVER_PREFIX:\$PATH\""
}

@test "PATH question is not asked when the folder is on PATH" {
  answers n
  run_install
  [ "$status" -eq 0 ]
  out_lacks "is not on your PATH"
}

@test "the profile follows the shell: bash on Linux, bash on macOS, zsh with ZDOTDIR, other shells" {
  TEST_PATH="$STUBS:$TOOLS"
  SHELL=/bin/bash answers y n
  SHELL=/bin/bash run_install
  [ "$status" -eq 0 ]
  grep -Fq 'added by local-server install.sh' "$HOME/.bashrc"
  rm "$(BIN)"

  export FAKE_UNAME_S=Darwin FAKE_UNAME_M=arm64
  make_release v1.0.0 darwin arm64
  answers y n
  SHELL=/bin/bash run_install
  [ "$status" -eq 0 ]
  grep -Fq 'added by local-server install.sh' "$HOME/.bash_profile"
  rm "$(BIN)"

  export FAKE_UNAME_S=Linux FAKE_UNAME_M=x86_64
  make_release v1.0.0
  mkdir -p "$ROOT/zdot"
  answers y n
  ZDOTDIR="$ROOT/zdot" SHELL=/usr/bin/zsh run_install
  [ "$status" -eq 0 ]
  grep -Fq 'added by local-server install.sh' "$ROOT/zdot/.zshrc"
  rm "$(BIN)"

  answers y n
  SHELL=/usr/bin/fish run_install
  [ "$status" -eq 0 ]
  grep -Fq 'added by local-server install.sh' "$HOME/.profile"
}

# --- first run (D-101, D-149) -----------------------------------------------------------------

@test "first run yes: serve runs, then the service is started" {
  answers y
  run_install
  [ "$status" -eq 0 ]
  log_has "$(BIN) serve"
  [ -f "$FAKE_LS_DEFAULT_DATA/local-server.db" ]
  [ -e "$FAKE_LS_STATE/user.running" ]
  out_has "recovery code"
  out_has "Ctrl-C"
  # serve ran before service start.
  local serve_line start_line
  serve_line=$(grep -n ' serve' "$FAKE_LS_LOG" | head -1 | cut -d: -f1)
  start_line=$(grep -n 'service start' "$FAKE_LS_LOG" | head -1 | cut -d: -f1)
  [ "$serve_line" -lt "$start_line" ]
}

@test "first run yes but serve created no database: the service is not started and the steps are shown" {
  export FAKE_SERVE_NO_DB=1
  answers y
  run_install
  [ "$status" -eq 1 ]
  log_lacks "service start"
  out_has "did not create the data folder"
  out_has "local-server service start"
}

@test "first run no: the service is not started" {
  answers n
  run_install
  [ "$status" -eq 0 ]
  log_lacks " serve"
  log_lacks "service start"
  [ ! -e "$FAKE_LS_STATE/user.running" ]
}

@test "an existing database skips the first run and starts the service" {
  mkdir -p "$FAKE_LS_DEFAULT_DATA"
  : >"$FAKE_LS_DEFAULT_DATA/local-server.db"
  : >"$LOCAL_SERVER_TEST_ANSWERS"
  run_install
  [ "$status" -eq 0 ]
  out_lacks "Run local-server once now"
  log_lacks " serve"
  log_has "service start"
}

@test "LS_DATA_DIR is passed to service install and serve, and is where the database is looked for" {
  export LS_DATA_DIR="$ROOT/custom data"
  answers y
  run_install
  [ "$status" -eq 0 ]
  log_has "service install --data-dir $ROOT/custom data"
  log_has "serve --data-dir $ROOT/custom data"
  [ -f "$ROOT/custom data/local-server.db" ]
  log_has "service start"
}

@test "macOS: the data folder is under Library/Application Support" {
  export FAKE_UNAME_S=Darwin FAKE_UNAME_M=arm64
  export FAKE_LS_DEFAULT_DATA="$HOME/Library/Application Support/local-server"
  make_release v1.0.0 darwin arm64
  answers y
  run_install
  [ "$status" -eq 0 ]
  [ -f "$HOME/Library/Application Support/local-server/local-server.db" ]
  log_has "service start"
}

# --- system service ---------------------------------------------------------------------------

@test "--system-service shows the command, asks, then runs it with sudo" {
  mkdir -p "$FAKE_LS_DEFAULT_DATA"
  : >"$FAKE_LS_DEFAULT_DATA/local-server.db"
  answers y
  run_install --system-service
  [ "$status" -eq 0 ]
  out_has "sudo $(BIN) service install --mode system --data-dir $FAKE_LS_DEFAULT_DATA"
  stub_has "sudo $(BIN) service install --mode system --data-dir $FAKE_LS_DEFAULT_DATA"
  stub_has "sudo $(BIN) service start --mode system"
  [ -e "$FAKE_LS_STATE/system.installed" ]
  [ ! -e "$FAKE_LS_STATE/user.installed" ]
}

@test "--system-service declined: no sudo, the command is shown for later" {
  answers n n
  run_install --system-service
  [ "$status" -eq 0 ]
  stub_lacks "sudo"
  [ ! -e "$FAKE_LS_STATE/system.installed" ]
  [ ! -e "$FAKE_LS_STATE/user.installed" ]
}

# --- Ollama and Tailscale ---------------------------------------------------------------------

@test "Ollama missing, answer no: the manual steps are shown and nothing is installed" {
  rm "$STUBS/ollama"
  answers n n
  run_install
  [ "$status" -eq 0 ]
  out_has "Install Ollama now?"
  out_has "https://ollama.com/download"
  out_has "ollama pull"
  stub_lacks "ollama.com"
  stub_lacks "installer ran"
}

@test "Ollama missing, answer yes: the official installer runs, after a word about sudo" {
  rm "$STUBS/ollama"
  answers y n
  run_install
  [ "$status" -eq 0 ]
  out_has "uses sudo"
  stub_has "curl https://ollama.com/install.sh"
  stub_has "installer ran: https://ollama.com/install.sh"
}

@test "Ollama is never installed unless asked: with the question unanswered nothing runs" {
  rm "$STUBS/ollama"
  : >"$LOCAL_SERVER_TEST_ANSWERS"
  run_install
  [ "$status" -eq 0 ]
  stub_lacks "installer ran"
}

@test "macOS, Ollama missing, yes, with brew: the cask ollama-app is installed" {
  export FAKE_UNAME_S=Darwin FAKE_UNAME_M=arm64
  export FAKE_LS_DEFAULT_DATA="$HOME/Library/Application Support/local-server"
  make_release v1.0.0 darwin arm64
  rm "$STUBS/ollama"
  # A machine that really has Ollama.app would not ask: this test is for the one that has not.
  if [ -d /Applications/Ollama.app ] || [ -d "$HOME/Applications/Ollama.app" ]; then
    skip "Ollama.app is installed on this machine"
  fi
  add_stub brew
  answers y n
  run_install
  [ "$status" -eq 0 ]
  stub_has "brew install --cask ollama-app"
}

@test "macOS, Ollama missing, yes, without brew: the manual steps are shown" {
  export FAKE_UNAME_S=Darwin FAKE_UNAME_M=arm64
  export FAKE_LS_DEFAULT_DATA="$HOME/Library/Application Support/local-server"
  make_release v1.0.0 darwin arm64
  rm "$STUBS/ollama" "$STUBS/brew"
  if [ -d /Applications/Ollama.app ] || [ -d "$HOME/Applications/Ollama.app" ]; then
    skip "Ollama.app is installed on this machine"
  fi
  answers y n
  run_install
  [ "$status" -eq 0 ]
  out_has "Homebrew was not found"
  out_has "https://ollama.com/download"
}

@test "Ollama installed but not answering: the start step is shown and the install goes on" {
  answers n
  run_install
  [ "$status" -eq 0 ]
  out_has "does not answer at http://127.0.0.1:1"
  out_has "systemctl status ollama"
  [ -x "$(BIN)" ]
}

@test "Ollama installed and answering is reported; OLLAMA_HOST without a scheme gets http://" {
  [ -n "$PYTHON3" ] || skip "python3 is not installed"
  mkdir -p "$ROOT/ollama-site/api"
  printf '{"version":"0.0.0"}\n' >"$ROOT/ollama-site/api/version"
  local port i
  port=$("$PYTHON3" -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')
  "$PYTHON3" -m http.server "$port" --bind 127.0.0.1 --directory "$ROOT/ollama-site" >/dev/null 2>&1 &
  SERVER_PID=$!
  disown "$SERVER_PID"
  for i in 1 2 3 4 5 6 7 8 9 10; do
    if "$REAL_CURL" -fsS --max-time 1 "http://127.0.0.1:$port/api/version" >/dev/null 2>&1; then break; fi
    sleep 0.5
  done
  export OLLAMA_HOST="127.0.0.1:$port"
  answers n
  run_install
  [ "$status" -eq 0 ]
  out_has "Ollama answers at http://127.0.0.1:$port"
  out_lacks "does not answer"
}

@test "Tailscale missing, answer no: the manual steps are shown, nothing runs" {
  rm "$STUBS/tailscale"
  answers n n
  run_install
  [ "$status" -eq 0 ]
  out_has "Install Tailscale now?"
  out_has "https://tailscale.com/download"
  stub_lacks "installer ran"
}

@test "Tailscale missing, answer yes: the official installer runs and tailscale up is never run" {
  rm "$STUBS/tailscale"
  answers y n
  run_install
  [ "$status" -eq 0 ]
  stub_has "installer ran: https://tailscale.com/install.sh"
  stub_lacks "tailscale up"
  out_has "Signing in is your step"
}

# --- no terminal ------------------------------------------------------------------------------

@test "no terminal and no hook: every question counts as no and the manual steps come at the end" {
  [ -n "$SETSID" ] || skip "setsid is not installed (macOS has none; CI Linux does)"
  rm "$STUBS/ollama"
  TEST_PATH="$STUBS:$TOOLS"
  run env -u LOCAL_SERVER_TEST_ANSWERS PATH="$TEST_PATH" "$SETSID" -w "$SH_BIN" "$SCRIPT" </dev/null
  [ "$status" -eq 0 ]
  [ -x "$(BIN)" ]
  out_has "There was no terminal to ask in"
  out_has "First run"
  out_has "export PATH="
  out_has "https://ollama.com/download"
  stub_lacks "installer ran"
  log_lacks "service start"
  log_lacks " serve"
  [ ! -e "$HOME/.zshrc" ]
}

# --- upgrade ----------------------------------------------------------------------------------

install_v1() {
  answers n
  run_install
  [ "$status" -eq 0 ]
  : >"$FAKE_LS_LOG"
}

@test "upgrade with a new version stops the service, replaces the binary and starts the service" {
  install_v1
  mkdir -p "$FAKE_LS_DEFAULT_DATA"
  : >"$FAKE_LS_DEFAULT_DATA/local-server.db"
  echo keep >"$FAKE_LS_DEFAULT_DATA/notes.txt"
  : >"$FAKE_LS_STATE/user.running"
  make_release v2.0.0
  : >"$LOCAL_SERVER_TEST_ANSWERS"
  run_install upgrade
  [ "$status" -eq 0 ]
  [ "$("$(BIN)" --version)" = "local-server v2.0.0" ]
  [ -x "$(BIN)" ]
  log_has "v1.0.0 $(BIN) service stop"
  log_has "v2.0.0 $(BIN) service start"
  [ -e "$FAKE_LS_STATE/user.running" ]
  # The data folder is untouched, and Ollama and Tailscale are not asked about again.
  [ "$(cat "$FAKE_LS_DEFAULT_DATA/notes.txt")" = keep ]
  out_lacks "Install Ollama now?"
  out_lacks "Install Tailscale now?"
  # No leftover staging file in the install folder.
  [ "$(ls -A "$LOCAL_SERVER_PREFIX")" = "local-server" ]
}

@test "upgrade with the same version is a no-op" {
  install_v1
  : >"$FAKE_LS_STATE/user.running"
  run_install upgrade
  [ "$status" -eq 0 ]
  out_has "Already up to date"
  log_lacks "service stop"
  log_lacks "service start"
}

@test "upgrade whose download fails the checksum changes nothing" {
  install_v1
  make_release v2.0.0
  printf '%s  %s\n' "2222222222222222222222222222222222222222222222222222222222222222" local-server_linux_amd64 >"$RELEASE/SHA256SUMS"
  run_install upgrade
  [ "$status" -ne 0 ]
  out_has "Nothing was installed or changed"
  [ "$("$(BIN)" --version)" = "local-server v1.0.0" ]
}

@test "upgrade without an installed program says so" {
  run_install upgrade
  [ "$status" -ne 0 ]
  out_has "nothing to upgrade"
  [ ! -e "$(BIN)" ]
}

@test "upgrade of a program found elsewhere on PATH refuses to write outside the install folder" {
  mkdir -p "$ROOT/elsewhere"
  cp "$RELEASE/local-server_linux_amd64" "$ROOT/elsewhere/local-server"
  chmod +x "$ROOT/elsewhere/local-server"
  TEST_PATH="$STUBS:$TOOLS:$ROOT/elsewhere"
  run_install upgrade
  [ "$status" -ne 0 ]
  out_has "$ROOT/elsewhere/local-server"
  out_has "LOCAL_SERVER_PREFIX="
  [ ! -e "$(BIN)" ]
}

@test "install over an installed program says it is an upgrade" {
  install_v1
  make_release v2.0.0
  run_install
  [ "$status" -eq 0 ]
  out_has "so this is an upgrade"
  [ "$("$(BIN)" --version)" = "local-server v2.0.0" ]
}

@test "upgrade with a system mode service shows the sudo restart step and runs no sudo" {
  install_v1
  : >"$FAKE_LS_STATE/system.installed"
  make_release v2.0.0
  run_install upgrade
  [ "$status" -eq 0 ]
  out_has "sudo $(BIN) service stop --mode system"
  stub_lacks "sudo"
}

# --- uninstall --------------------------------------------------------------------------------

@test "uninstall removes the binary, the PATH line and the service, and keeps the data folder" {
  TEST_PATH="$STUBS:$TOOLS"
  printf 'alias a=b\n' >"$HOME/.zshrc"
  answers y n
  run_install
  [ "$status" -eq 0 ]
  grep -Fq 'added by local-server install.sh' "$HOME/.zshrc"
  mkdir -p "$FAKE_LS_DEFAULT_DATA"
  : >"$FAKE_LS_DEFAULT_DATA/local-server.db"
  : >"$FAKE_LS_STATE/user.running"
  answers n
  run_install uninstall
  [ "$status" -eq 0 ]
  [ ! -e "$(BIN)" ]
  [ "$(cat "$HOME/.zshrc")" = "alias a=b" ]
  [ ! -e "$FAKE_LS_STATE/user.installed" ]
  [ ! -e "$FAKE_LS_STATE/user.running" ]
  log_has "service stop"
  log_has "service uninstall"
  [ -f "$FAKE_LS_DEFAULT_DATA/local-server.db" ]
  out_has "Ollama and Tailscale were left alone"
}

@test "uninstall deletes the data folder only on a yes" {
  answers n
  run_install
  mkdir -p "$FAKE_LS_DEFAULT_DATA"
  : >"$FAKE_LS_DEFAULT_DATA/local-server.db"
  answers y
  run_install uninstall
  [ "$status" -eq 0 ]
  out_has "This cannot be undone"
  [ ! -e "$FAKE_LS_DEFAULT_DATA" ]
}

@test "uninstall keeps the profile's mode and symlink" {
  TEST_PATH="$STUBS:$TOOLS"
  mkdir -p "$ROOT/dotfiles"
  printf 'alias a=b\n' >"$ROOT/dotfiles/zshrc"
  chmod 640 "$ROOT/dotfiles/zshrc"
  ln -s "$ROOT/dotfiles/zshrc" "$HOME/.zshrc"
  answers y n
  run_install
  [ "$status" -eq 0 ]
  grep -Fq 'added by local-server install.sh' "$ROOT/dotfiles/zshrc"
  answers n
  run_install uninstall
  [ "$status" -eq 0 ]
  [ -L "$HOME/.zshrc" ]
  [ "$(cat "$ROOT/dotfiles/zshrc")" = "alias a=b" ]
  [ "$(ls -l "$ROOT/dotfiles/zshrc" | cut -c1-10)" = "-rw-r-----" ]
}

@test "uninstall works when the binary is already gone" {
  TEST_PATH="$STUBS:$TOOLS"
  answers y n
  run_install
  [ "$status" -eq 0 ]
  rm "$(BIN)"
  answers n
  run_install uninstall
  [ "$status" -eq 0 ]
  out_has "was not there"
  ! grep -Fq 'added by local-server install.sh' "$HOME/.zshrc"
}

@test "uninstall with a system mode service shows the sudo steps and runs no sudo" {
  answers n
  run_install
  : >"$FAKE_LS_STATE/system.installed"
  answers n
  run_install uninstall
  [ "$status" -eq 0 ]
  out_has "sudo $(BIN) service uninstall --mode system"
  stub_lacks "sudo"
  [ ! -e "$(BIN)" ]
}

@test "uninstall with no terminal keeps the data folder and prints its path at the end" {
  [ -n "$SETSID" ] || skip "setsid is not installed (macOS has none; CI Linux does)"
  answers n
  run_install
  mkdir -p "$FAKE_LS_DEFAULT_DATA"
  : >"$FAKE_LS_DEFAULT_DATA/local-server.db"
  run env -u LOCAL_SERVER_TEST_ANSWERS PATH="$TEST_PATH" "$SETSID" -w "$SH_BIN" "$SCRIPT" uninstall </dev/null
  [ "$status" -eq 0 ]
  [ ! -e "$(BIN)" ]
  [ -f "$FAKE_LS_DEFAULT_DATA/local-server.db" ]
  out_has "Data folder kept: $FAKE_LS_DEFAULT_DATA"
}

@test "uninstall refuses to delete a data folder that is the home folder" {
  export LS_DATA_DIR="$HOME"
  answers n
  run_install
  answers y
  run_install uninstall
  [ "$status" -ne 0 ]
  out_has "refusing to delete"
  [ -d "$HOME" ]
}
