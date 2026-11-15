#!/bin/sh
# local-server installer for macOS and Linux: install, upgrade and uninstall (PLT-F-043; req_001
# D-14, D-23, D-30, D-49, D-62, D-101, D-149, D-154, D-156).
#
#   curl -fsSL https://github.com/inCoderKaka/local-server/releases/latest/download/install.sh | sh
#   curl -fsSL https://github.com/inCoderKaka/local-server/releases/latest/download/install.sh | sh -s -- upgrade
#
# POSIX sh: it must run under dash, the macOS /bin/sh and `curl | sh`. Everything is wrapped in
# functions and `main` is called on the last line, so a truncated download cannot run half a script.
#
# Rules this script keeps:
#   - F-07: every download is fetched with curl only, so no quarantine attribute is set, and the
#     binary is never stripped, re-signed or otherwise changed after the download.
#   - The SHA-256 of the binary is checked against the release's SHA256SUMS before anything is
#     installed or changed. A mismatch, a missing line or a missing hash tool stops the script.
#   - It asks before it changes anything outside the install folder (PATH line, Ollama, Tailscale,
#     system service, data folder). Every question defaults to no. Questions are read from the
#     terminal (/dev/tty), never from stdin, because stdin is the script itself under `curl | sh`.
#     With no terminal every question counts as no and the manual steps are printed at the end.
#   - It writes only under $HOME, the install folder and the data folder (the Ollama and Tailscale
#     installers that the user agreed to run are the exception: they use sudo themselves).
#   - It never creates the data folder: `local-server serve` does, in the user's terminal, because
#     the first run prints a one-time recovery code that must be seen by a person (D-101, D-149).
#
# Environment (all optional):
#   LOCAL_SERVER_VERSION        release tag to install, like --version (default: the latest release)
#   LOCAL_SERVER_PREFIX         install folder (default: $HOME/.local/bin)
#   LOCAL_SERVER_RELEASE_BASE   folder URL that holds the release files; any URL curl reads,
#                               including file:// (default: built from the version)
#   LOCAL_SERVER_OLLAMA_WAIT    seconds to wait for Ollama after it was installed (default: 15)
#   LS_DATA_DIR                 data folder, as for `local-server serve`
#   LOCAL_SERVER_TEST_ANSWERS   test hook of scripts/test/install.bats: a file with one answer per
#                               line, used instead of the terminal.
#
# URLs this script relies on, checked with `curl -sI` on 2026-11-15 (not run, only asked):
#   https://ollama.com/install.sh                    307 -> github.com/ollama/ollama/releases/latest/download/install.sh (200 when followed)
#   https://ollama.com/download                      200 (download page, shown when nothing can be automated)
#   https://tailscale.com/install.sh                 200
#   https://tailscale.com/download                   200 (download page)
#   https://apps.apple.com/app/tailscale/id1475387142   301 -> apps.apple.com/us/app/tailscale/id1475387142 (Mac App Store)
#   https://formulae.brew.sh/api/cask/ollama-app.json   200 (the cask "ollama-app" installs Ollama.app; the token "ollama" is 404 there)
# Not verified: that an unsigned binary fetched by curl passes Gatekeeper (D-30 asks to test it
# before the first release), and the Linux Ollama and Tailscale scripts themselves (never run here).

set -eu

APP=local-server
RELEASES_URL="https://github.com/inCoderKaka/local-server/releases"
OLLAMA_SCRIPT_URL="https://ollama.com/install.sh"
OLLAMA_DOWNLOAD_URL="https://ollama.com/download"
TAILSCALE_SCRIPT_URL="https://tailscale.com/install.sh"
TAILSCALE_DOWNLOAD_URL="https://tailscale.com/download"
TAILSCALE_MAC_APP_URL="https://apps.apple.com/app/tailscale/id1475387142"
PATH_MARKER="# added by local-server install.sh"
DB_FILE="local-server.db"

ACTION=install
VERSION=""
SYSTEM_SERVICE=0
OS=""
ARCH=""
PREFIX=""
BIN=""
DATA_DIR=""
PROFILE=""
BASE=""
HASH_TOOL=""
TMP_DIR=""
STAGED=""
CMD=$APP
SVC_MODE=user
SVC_READY=0
FAILED=0
ANSWER_N=0
ASK_SKIPPED=0
SKIPPED_KEYS=""

say() { printf '%s\n' "$*"; }
warn() { printf '%s\n' "$*" >&2; }
die() {
  warn "install.sh: $*"
  exit 1
}

usage() {
  cat <<'EOF'
Usage: sh install.sh [install|upgrade|uninstall] [--version TAG] [--system-service]

  install      download local-server, verify it, install it and set it up (the default)
  upgrade      replace the installed program with a newer one; your data is not touched
  uninstall    remove the program, its background service and its PATH line

  --version TAG      install this release tag (for example one that is a pre-release:
                     the "latest" release skips pre-releases)
  --system-service   register the background service for the whole machine (starts at boot,
                     needs sudo) instead of for your user (starts at login)
  -h, --help         show this text

Run it from the web:
  curl -fsSL https://github.com/inCoderKaka/local-server/releases/latest/download/install.sh | sh
  curl -fsSL https://github.com/inCoderKaka/local-server/releases/latest/download/install.sh | sh -s -- upgrade
  curl -fsSL https://github.com/inCoderKaka/local-server/releases/download/TAG/install.sh | sh -s -- --version TAG

Environment: LOCAL_SERVER_VERSION, LOCAL_SERVER_PREFIX (default $HOME/.local/bin),
LOCAL_SERVER_RELEASE_BASE, LOCAL_SERVER_OLLAMA_WAIT and LS_DATA_DIR.
Exit codes: 0 done, 1 failed, 2 wrong command line.
EOF
}

usage_error() {
  warn "install.sh: $*"
  usage >&2
  exit 2
}

cleanup() {
  if [ -n "$STAGED" ]; then rm -f "$STAGED" || :; fi
  if [ -n "$TMP_DIR" ]; then rm -rf "$TMP_DIR" || :; fi
}

parse_args() {
  VERSION=${LOCAL_SERVER_VERSION:-}
  while [ $# -gt 0 ]; do
    case $1 in
      install | upgrade | uninstall) ACTION=$1 ;;
      --version)
        [ $# -ge 2 ] || usage_error "--version needs a release tag"
        VERSION=$2
        shift
        ;;
      --version=*) VERSION=${1#--version=} ;;
      --system-service) SYSTEM_SERVICE=1 ;;
      -h | --help)
        usage
        exit 0
        ;;
      *) usage_error "unknown argument: $1" ;;
    esac
    shift
  done
  # The tag goes into a URL: keep it to the characters a tag has.
  case $VERSION in
    *[!A-Za-z0-9._-]*) usage_error "the release tag may only contain letters, digits, dots, dashes and underscores" ;;
  esac
  if [ "$SYSTEM_SERVICE" = 1 ] && [ "$ACTION" != install ]; then
    usage_error "--system-service only goes with install"
  fi
}

# --- terminal and questions -------------------------------------------------------------------

have_tty() { ( : </dev/tty ) 2>/dev/null; }

# run_interactive CMD ARGS...: run a command that may want a terminal, with stdin from the terminal
# when there is one and from /dev/null otherwise. Never from this script's own stdin.
run_interactive() {
  if have_tty; then
    "$@" </dev/tty
  else
    "$@" </dev/null
  fi
}

# ask PROMPT: 0 for yes, 1 for no. Every question defaults to no. The answer comes from the test
# hook file, else from /dev/tty. With neither, it is no, and ASK_SKIPPED says so (see decline).
ask() {
  ASK_SKIPPED=0
  ask_answer=""
  if [ -n "${LOCAL_SERVER_TEST_ANSWERS:-}" ]; then
    printf '%s [y/N] ' "$1" >&2
    ANSWER_N=$((ANSWER_N + 1))
    ask_answer=$(sed -n "${ANSWER_N}p" "$LOCAL_SERVER_TEST_ANSWERS" 2>/dev/null) || ask_answer=""
    printf '%s\n' "$ask_answer" >&2
  elif have_tty; then
    printf '%s [y/N] ' "$1" >&2
    IFS= read -r ask_answer </dev/tty || ask_answer=""
  else
    ASK_SKIPPED=1
    return 1
  fi
  case $ask_answer in
    y | Y | yes | YES | Yes) return 0 ;;
  esac
  return 1
}

# decline KEY: the answer was no. With a terminal, show the manual steps now. Without one, remember
# the key and show them at the end of the run, together.
decline() {
  if [ "$ASK_SKIPPED" = 1 ]; then
    SKIPPED_KEYS="$SKIPPED_KEYS $1"
  else
    manual_steps "$1"
  fi
}

print_skipped() {
  if [ -z "$SKIPPED_KEYS" ]; then return 0; fi
  say ""
  say "There was no terminal to ask in, so these questions counted as no. Do these steps by hand:"
  for ps_key in $SKIPPED_KEYS; do
    manual_steps "$ps_key"
  done
}

manual_steps() {
  case $1 in
    path)
      say ""
      say "PATH: $PREFIX is not on your PATH. To add it, put this line in $PROFILE and open a new terminal:"
      say "  export PATH=\"$PREFIX:\$PATH\""
      ;;
    systemservice)
      say ""
      say "System service: to register it for the whole machine (starts at boot), run:"
      say "  sudo $BIN service install --mode system --data-dir $DATA_DIR"
      ;;
    firstrun)
      say ""
      say "First run (do this once, in this terminal window; the recovery code is only shown on a terminal):"
      say "  1. $CMD serve"
      say "     It creates your data folder and shows a one-time recovery code."
      say "  2. Write the recovery code down and keep it safe, then press Ctrl-C to stop it."
      say "  3. $CMD service start"
      say "     It starts the background service."
      ;;
    ollama)
      say ""
      say "Ollama: local-server shares your Ollama, so set it up first:"
      say "  1. Install it from $OLLAMA_DOWNLOAD_URL"
      say "  2. Start it (open the Ollama app, or run: ollama serve)"
      say "  3. Pull a model to share: ollama pull <model>"
      ;;
    tailscale)
      say ""
      say "Tailscale: it carries the encrypted connection between you and the people you invite."
      if [ "$OS" = darwin ]; then
        say "  1. Install it from the Mac App Store ($TAILSCALE_MAC_APP_URL) or the standalone package ($TAILSCALE_DOWNLOAD_URL)"
        say "  2. Open the Tailscale app and sign in. This script never signs in for you."
      else
        say "  1. Install it: $TAILSCALE_DOWNLOAD_URL"
        say "  2. Sign in with: sudo tailscale up (this script never runs it for you)"
      fi
      ;;
    datadir)
      say ""
      say "Data folder kept: $DATA_DIR"
      say "To delete it (database and keys, this cannot be undone) run: rm -rf \"$DATA_DIR\""
      ;;
  esac
}

# --- platform, download, checksum -------------------------------------------------------------

detect_os() {
  do_s=$(uname -s)
  case $do_s in
    Darwin) OS=darwin ;;
    Linux) OS=linux ;;
    *) die "unsupported operating system: $do_s. This installer supports macOS and Linux (on Windows use install.ps1). Nothing was installed." ;;
  esac
}

detect_arch() {
  da_m=$(uname -m)
  case $da_m in
    x86_64 | amd64) ARCH=amd64 ;;
    arm64 | aarch64) ARCH=arm64 ;;
    *) die "unsupported CPU type: $da_m. Releases exist for amd64 and arm64. Nothing was installed." ;;
  esac
}

make_tmp() {
  TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/local-server-install.XXXXXX") || die "cannot create a temporary folder"
}

# fetch_https URL DEST: for the pages that are not ours (Ollama, Tailscale): https only.
fetch_https() {
  curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 --retry 2 --connect-timeout 20 -o "$2" "$1"
}

# fetch_release URL DEST: for our release files. LOCAL_SERVER_RELEASE_BASE may be any URL curl
# reads, including file://, so the https-only rule applies only to the default place.
fetch_release() {
  if [ -n "${LOCAL_SERVER_RELEASE_BASE:-}" ]; then
    curl -fsSL --retry 2 --connect-timeout 20 -o "$2" "$1"
  else
    fetch_https "$1" "$2"
  fi
}

resolve_base() {
  if [ -n "${LOCAL_SERVER_RELEASE_BASE:-}" ]; then
    BASE=${LOCAL_SERVER_RELEASE_BASE%/}
  elif [ -n "$VERSION" ]; then
    BASE="$RELEASES_URL/download/$VERSION"
  else
    BASE="$RELEASES_URL/latest/download"
  fi
}

# fetch_asset NAME DEST
fetch_asset() {
  fa_rc=0
  fetch_release "$BASE/$1" "$2" || fa_rc=$?
  if [ "$fa_rc" -ne 0 ]; then
    warn "install.sh: could not download $BASE/$1 (curl exit code $fa_rc)."
    # 22: the server answered 404 (or another error status); 37: a file:// file that is not there.
    if [ "$fa_rc" = 22 ] || [ "$fa_rc" = 37 ]; then
      warn "The release or the file was not found. The \"latest\" release skips pre-releases, so"
      warn "when only a pre-release exists, name its tag: sh -s -- --version TAG (or set LOCAL_SERVER_VERSION)."
      warn "Tags are listed at $RELEASES_URL"
    fi
    die "nothing was installed or changed."
  fi
}

pick_hash_tool() {
  if command -v sha256sum >/dev/null 2>&1; then
    HASH_TOOL=sha256sum
  elif command -v shasum >/dev/null 2>&1; then
    HASH_TOOL=shasum
  elif command -v openssl >/dev/null 2>&1; then
    HASH_TOOL=openssl
  else
    die "no SHA-256 tool found (sha256sum, shasum or openssl), so the download cannot be verified. Nothing was installed."
  fi
}

hash_of() {
  case $HASH_TOOL in
    sha256sum) sha256sum "$1" | awk '{ print $1 }' ;;
    shasum) shasum -a 256 "$1" | awk '{ print $1 }' ;;
    openssl) openssl dgst -sha256 "$1" | awk '{ print $NF }' ;;
  esac
}

# verify_checksum FILE NAME SUMSFILE: stop unless the file's SHA-256 equals the SUMSFILE line for NAME.
verify_checksum() {
  vc_want=$(awk -v n="$2" '$2 == n || $2 == "*" n { print $1; exit }' "$3") || vc_want=""
  if [ -z "$vc_want" ]; then
    die "SHA256SUMS has no line for $2, so the download cannot be verified. Nothing was installed or changed."
  fi
  vc_got=$(hash_of "$1") || vc_got=""
  vc_want=$(printf '%s' "$vc_want" | tr 'A-F' 'a-f')
  vc_got=$(printf '%s' "$vc_got" | tr 'A-F' 'a-f')
  if [ -z "$vc_got" ] || [ "$vc_got" != "$vc_want" ]; then
    die "the SHA-256 of $2 does not match SHA256SUMS (expected $vc_want, got ${vc_got:-nothing}). The file may be damaged or changed on the way. Nothing was installed or changed."
  fi
  say "Checksum verified (SHA-256 $vc_got)."
}

# replace_binary SRC: put SRC at $BIN atomically (copy beside it, then rename over it). A rename gives
# the program a new file, so a running copy keeps running and macOS never sees a half-written file.
replace_binary() {
  mkdir -p "$PREFIX" || die "cannot create $PREFIX. Nothing was changed."
  STAGED="$PREFIX/.$APP.new.$$"
  cp "$1" "$STAGED" || die "cannot write into $PREFIX. Nothing was changed."
  chmod +x "$STAGED" || die "cannot make $STAGED executable. Nothing was changed."
  mv -f "$STAGED" "$BIN" || die "cannot replace $BIN. The old program, if any, is still there."
  STAGED=""
}

# --- paths ------------------------------------------------------------------------------------

resolve_prefix() {
  PREFIX=${LOCAL_SERVER_PREFIX:-$HOME/.local/bin}
  case $PREFIX in
    /*) ;;
    *) die "LOCAL_SERVER_PREFIX must be an absolute path (got $PREFIX)" ;;
  esac
  if [ "$PREFIX" != / ]; then PREFIX=${PREFIX%/}; fi
  BIN="$PREFIX/$APP"
}

# The same place `local-server serve` uses (os.UserConfigDir + local-server, or LS_DATA_DIR).
resolve_data_dir() {
  if [ -n "${LS_DATA_DIR:-}" ]; then
    DATA_DIR=$LS_DATA_DIR
  elif [ "$OS" = darwin ]; then
    DATA_DIR="$HOME/Library/Application Support/$APP"
  else
    DATA_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/$APP"
  fi
}

dir_on_path() {
  case ":$PATH:" in
    *":$1:"*) return 0 ;;
  esac
  return 1
}

pick_profile() {
  pp_shell=${SHELL:-}
  pp_shell=${pp_shell##*/}
  case $pp_shell in
    zsh) PROFILE="${ZDOTDIR:-$HOME}/.zshrc" ;;
    bash)
      if [ "$OS" = darwin ]; then
        PROFILE="$HOME/.bash_profile"
      else
        PROFILE="$HOME/.bashrc"
      fi
      ;;
    *) PROFILE="$HOME/.profile" ;;
  esac
}

# The one line that goes into the profile. The $PATH stays literal on purpose: the shell expands it
# when it reads the profile.
path_line() {
  # shellcheck disable=SC2016
  printf 'export PATH="%s:$PATH" %s' "$PREFIX" "$PATH_MARKER"
}

step_path() {
  if dir_on_path "$PREFIX"; then
    CMD=$APP
    return 0
  fi
  CMD=$BIN
  pick_profile
  sp_line=$(path_line)
  if grep -Fxq -- "$sp_line" "$PROFILE" 2>/dev/null; then
    say "$PREFIX is on the PATH line already in $PROFILE; open a new terminal to use it."
    return 0
  fi
  if ask "$PREFIX is not on your PATH. Add one line to $PROFILE so it is?"; then
    if [ -s "$PROFILE" ] && [ -n "$(tail -c 1 "$PROFILE")" ]; then
      printf '\n' >>"$PROFILE" || :
    fi
    if printf '%s\n' "$sp_line" >>"$PROFILE"; then
      say "Added the PATH line to $PROFILE. Open a new terminal to use it."
    else
      warn "Could not write to $PROFILE."
      manual_steps path
    fi
  else
    decline path
  fi
}

# remove_marker_lines FILE: delete the lines this script added (the ones that carry the marker),
# keeping the file's mode and any symlink by rewriting through `cat >`.
remove_marker_lines() {
  if [ ! -f "$1" ]; then return 0; fi
  if ! grep -Fq -- "$PATH_MARKER" "$1"; then return 0; fi
  rm_new="$TMP_DIR/profile.new"
  # grep -v exits 1 when no line is left, which is fine.
  grep -Fv -- "$PATH_MARKER" "$1" >"$rm_new" || [ $? -eq 1 ]
  cat "$rm_new" >"$1"
  say "Removed the PATH line from $1."
}

# --- background service -----------------------------------------------------------------------

# service_status_text: the output of `service status`, empty when it cannot be asked.
service_status_text() {
  "$BIN" service status 2>/dev/null || :
}

step_service() {
  SVC_READY=0
  if [ "$SYSTEM_SERVICE" = 1 ]; then
    say ""
    say "System service: it starts at boot for the whole machine and needs administrator rights. The command is:"
    say "  sudo $BIN service install --mode system --data-dir $DATA_DIR"
    if ! command -v sudo >/dev/null 2>&1; then
      warn "sudo was not found, so this cannot run here."
      FAILED=1
      return 0
    fi
    if ask "Run it now with sudo?"; then
      if run_interactive sudo "$BIN" service install --mode system --data-dir "$DATA_DIR"; then
        SVC_MODE=system
        SVC_READY=1
      else
        warn "The system service could not be registered. $BIN is installed; fix the problem above and run the command again."
        FAILED=1
      fi
    else
      decline systemservice
    fi
    return 0
  fi
  say ""
  say "Registering the background service (for your user, starts at login)..."
  if [ -n "${LS_DATA_DIR:-}" ]; then
    if "$BIN" service install --data-dir "$LS_DATA_DIR"; then SVC_READY=1; fi
  else
    if "$BIN" service install; then SVC_READY=1; fi
  fi
  if [ "$SVC_READY" != 1 ]; then
    warn "The background service could not be registered. $BIN is installed; fix the problem above, then run: $BIN service install"
    FAILED=1
  fi
}

start_service() {
  if [ "$SVC_READY" != 1 ]; then
    say "The background service is not registered, so it was not started."
    return 0
  fi
  if [ "$SVC_MODE" = system ]; then
    if ! run_interactive sudo "$BIN" service start --mode system; then
      warn "The system service did not start. Try: sudo $BIN service start --mode system"
      FAILED=1
    fi
  elif ! "$BIN" service start; then
    warn "The background service did not start. Try: $BIN service start"
    FAILED=1
  fi
}

# The first run (D-101, D-149): only a terminal may see the one-time recovery code, so this runs
# serve in the foreground here, and starts the service only after it created the database.
step_first_run() {
  if [ -f "$DATA_DIR/$DB_FILE" ]; then
    say ""
    say "Found your data folder ($DATA_DIR); the first run is not needed."
    start_service
    return 0
  fi
  say ""
  if ask "Run local-server once now to create your data folder and show your recovery code?"; then
    say "local-server will now run in this window. It shows a one-time recovery code."
    say "Write the recovery code down and keep it safe, then press Ctrl-C to stop it."
    # A no-op handler, not an ignore: an ignored signal stays ignored in the child, and serve needs
    # the Ctrl-C. With a handler, only the child stops and this script goes on.
    trap ':' INT
    fr_rc=0
    if [ -n "${LS_DATA_DIR:-}" ]; then
      run_interactive "$BIN" serve --data-dir "$LS_DATA_DIR" || fr_rc=$?
    else
      run_interactive "$BIN" serve || fr_rc=$?
    fi
    trap 'exit 130' INT
    say ""
    if [ -f "$DATA_DIR/$DB_FILE" ]; then
      say "Your data folder was created at $DATA_DIR."
      start_service
    else
      warn "The first run did not create the data folder (exit code $fr_rc), so the service was not started."
      manual_steps firstrun
      FAILED=1
    fi
  else
    decline firstrun
  fi
}

# --- Ollama and Tailscale ---------------------------------------------------------------------

ollama_installed() {
  if command -v ollama >/dev/null 2>&1; then return 0; fi
  if [ "$OS" = darwin ]; then
    if [ -d /Applications/Ollama.app ] || [ -d "$HOME/Applications/Ollama.app" ]; then return 0; fi
  fi
  return 1
}

# The address Ollama answers on: OLLAMA_HOST, with http:// added when it has no scheme.
ollama_url() {
  ou_host=${OLLAMA_HOST:-http://127.0.0.1:11434}
  case $ou_host in
    http://* | https://*) ;;
    *) ou_host="http://$ou_host" ;;
  esac
  # 0.0.0.0 is where Ollama listens, not where to call it.
  case $ou_host in
    http://0.0.0.0* | https://0.0.0.0*) ou_host="${ou_host%%//*}//127.0.0.1${ou_host#*//0.0.0.0}" ;;
  esac
  printf '%s' "${ou_host%/}"
}

ollama_answers() { curl -fsS --max-time 3 "$1/api/version" >/dev/null 2>&1; }

# wait_for_ollama URL SECONDS
wait_for_ollama() {
  wo_i=0
  while :; do
    if ollama_answers "$1"; then return 0; fi
    if [ "$wo_i" -ge "$2" ]; then return 1; fi
    sleep 1
    wo_i=$((wo_i + 1))
  done
}

install_ollama() {
  case $OS in
    linux)
      say "Ollama's own installer will run now. It uses sudo, so it may ask for your password."
      if fetch_https "$OLLAMA_SCRIPT_URL" "$TMP_DIR/ollama-install.sh" && run_interactive sh "$TMP_DIR/ollama-install.sh"; then
        say "Ollama's installer finished."
      else
        warn "Ollama's installer did not finish."
        manual_steps ollama
      fi
      ;;
    darwin)
      if command -v brew >/dev/null 2>&1; then
        say "Running: brew install --cask ollama-app"
        if ! run_interactive brew install --cask ollama-app; then
          warn "Homebrew could not install Ollama."
          manual_steps ollama
        fi
      else
        say "Homebrew was not found, so Ollama cannot be installed from here."
        manual_steps ollama
      fi
      ;;
  esac
}

step_ollama() {
  so_wait=0
  say ""
  if ollama_installed; then
    say "Ollama: found."
  else
    say "Ollama: not found. local-server shares the models of your Ollama."
    if ask "Install Ollama now?"; then
      install_ollama
      so_wait=${LOCAL_SERVER_OLLAMA_WAIT:-15}
      case $so_wait in
        '' | *[!0-9]*) so_wait=15 ;;
      esac
    else
      decline ollama
    fi
  fi
  if ollama_installed; then
    so_url=$(ollama_url)
    if wait_for_ollama "$so_url" "$so_wait"; then
      say "Ollama answers at $so_url."
    else
      say "Ollama is installed but does not answer at $so_url. Start it, then carry on:"
      if [ "$OS" = darwin ]; then
        say "  open -a Ollama"
      else
        say "  systemctl status ollama      (or run: ollama serve)"
      fi
    fi
  fi
}

tailscale_installed() {
  if command -v tailscale >/dev/null 2>&1; then return 0; fi
  if [ "$OS" = darwin ] && [ -d /Applications/Tailscale.app ]; then return 0; fi
  return 1
}

step_tailscale() {
  say ""
  if tailscale_installed; then
    say "Tailscale: found."
    return 0
  fi
  say "Tailscale: not found."
  if [ "$OS" = darwin ]; then
    # Not automated on macOS: the app comes from the App Store or a signed package.
    manual_steps tailscale
    return 0
  fi
  if ask "Install Tailscale now?"; then
    say "Tailscale's own installer will run now. It uses sudo, so it may ask for your password."
    if fetch_https "$TAILSCALE_SCRIPT_URL" "$TMP_DIR/tailscale-install.sh" && run_interactive sh "$TMP_DIR/tailscale-install.sh"; then
      say "Tailscale's installer finished. Signing in is your step: run sudo tailscale up (this script never runs it)."
    else
      warn "Tailscale's installer did not finish."
      manual_steps tailscale
    fi
  else
    decline tailscale
  fi
}

# --- actions ----------------------------------------------------------------------------------

check_user() {
  if [ -z "${HOME:-}" ] || [ ! -d "$HOME" ]; then
    die "HOME is not set to a folder"
  fi
  if [ "$(id -u)" = 0 ] && [ -n "${SUDO_USER:-}" ]; then
    die "do not run this with sudo: local-server is installed for your own user, and the script asks for sudo itself when a step needs it"
  fi
  if ! command -v curl >/dev/null 2>&1; then
    die "curl is needed and was not found"
  fi
}

do_install() {
  check_user
  detect_os
  detect_arch
  resolve_prefix
  resolve_data_dir
  pick_hash_tool
  resolve_base
  make_tmp

  di_existing=0
  if [ -x "$BIN" ]; then di_existing=1; fi
  if [ "$ACTION" = upgrade ] && [ "$di_existing" = 0 ]; then
    di_other=$(command -v "$APP" 2>/dev/null) || di_other=""
    if [ -n "$di_other" ]; then
      die "$APP is at $di_other, not in $PREFIX. This script only changes $PREFIX; run it again with LOCAL_SERVER_PREFIX=$(dirname "$di_other") if you put it there."
    fi
    die "$APP is not installed in $PREFIX, so there is nothing to upgrade. Run it without upgrade to install."
  fi
  if [ "$ACTION" = install ] && [ "$di_existing" = 1 ]; then
    say "$APP is already installed at $BIN, so this is an upgrade."
    ACTION=upgrade
  fi

  di_asset="${APP}_${OS}_${ARCH}"
  say "Downloading $di_asset from $BASE ..."
  fetch_asset "$di_asset" "$TMP_DIR/$di_asset"
  fetch_asset SHA256SUMS "$TMP_DIR/SHA256SUMS"
  verify_checksum "$TMP_DIR/$di_asset" "$di_asset" "$TMP_DIR/SHA256SUMS"
  # upload-artifact drops the execute bit.
  chmod +x "$TMP_DIR/$di_asset" || die "cannot make the download executable. Nothing was installed."

  if [ "$ACTION" = upgrade ]; then
    do_upgrade "$TMP_DIR/$di_asset"
  else
    do_fresh_install "$TMP_DIR/$di_asset"
  fi
}

do_fresh_install() {
  replace_binary "$1"
  say "Installed $BIN ($("$BIN" --version 2>/dev/null || printf 'version unknown'))."
  step_path
  step_service
  step_ollama
  step_tailscale
  step_first_run
  print_skipped
  say ""
  if [ "$FAILED" = 1 ]; then
    say "local-server is installed, but a step above failed."
    exit 1
  fi
  say "Done. To remove it later: sh install.sh uninstall"
}

do_upgrade() {
  # The new program ran only now, after its checksum passed. A /tmp that does not allow running
  # programs leaves the version unknown, and then the upgrade goes on.
  du_new=$("$1" --version 2>/dev/null) || du_new=""
  du_old=$("$BIN" --version 2>/dev/null) || du_old=""
  if [ -n "$du_new" ] && [ "$du_new" = "$du_old" ]; then
    say "Already up to date ($du_old)."
    return 0
  fi
  say "Upgrading: ${du_old:-installed version unknown} -> ${du_new:-new version unknown}"
  du_status=$(service_status_text)
  du_running=0
  du_stopped=0
  if printf '%s\n' "$du_status" | grep -q 'user mode: installed, running'; then du_running=1; fi
  if printf '%s\n' "$du_status" | grep -q 'user mode: installed, stopped'; then du_stopped=1; fi
  if [ "$du_running" = 1 ]; then
    say "Stopping the background service..."
    "$BIN" service stop || warn "The background service did not stop; the new program takes over when it restarts."
  fi
  replace_binary "$1"
  say "Installed $BIN ($("$BIN" --version 2>/dev/null || printf 'version unknown'))."
  if [ "$du_running" = 1 ]; then
    if [ -f "$DATA_DIR/$DB_FILE" ]; then
      say "Starting the background service..."
      "$BIN" service start || {
        warn "The background service did not start. Try: $BIN service start"
        FAILED=1
      }
    else
      say "The data folder was not found at $DATA_DIR, so the service was not started. Run the first run: $BIN serve"
    fi
  elif [ "$du_stopped" = 1 ]; then
    say "The background service is stopped; start it with: $BIN service start"
  elif ! printf '%s\n' "$du_status" | grep -q 'mode: installed'; then
    say "No background service is registered. To add it: $BIN service install"
  fi
  if printf '%s\n' "$du_status" | grep -q 'system mode: installed'; then
    say "A system mode service is installed. To run the new program, restart it:"
    say "  sudo $BIN service stop --mode system && sudo $BIN service start --mode system"
  fi
  if [ "$FAILED" = 1 ]; then exit 1; fi
  say "Upgrade done. Your data was not touched."
}

uninstall_service() {
  if [ ! -x "$BIN" ]; then
    say "$BIN is already gone, so the background service cannot be asked. If one is left, it is registered under the name local-server."
    return 0
  fi
  us_rc=0
  us_status=$("$BIN" service status 2>/dev/null) || us_rc=$?
  if [ "$us_rc" != 0 ] || printf '%s\n' "$us_status" | grep -q 'user mode: installed'; then
    say "Stopping and removing the background service..."
    "$BIN" service stop || warn "Could not stop the background service."
    "$BIN" service uninstall || warn "Could not remove the background service; try: $BIN service uninstall"
  else
    say "No user mode background service is registered."
  fi
  if printf '%s\n' "$us_status" | grep -q 'system mode: installed'; then
    say "A system mode service is installed. Removing it needs sudo, so run:"
    say "  sudo $BIN service stop --mode system"
    say "  sudo $BIN service uninstall --mode system"
  fi
}

do_uninstall() {
  check_user
  detect_os
  resolve_prefix
  resolve_data_dir
  make_tmp
  uninstall_service
  if [ -e "$BIN" ] || [ -L "$BIN" ]; then
    rm -f "$BIN" || die "cannot remove $BIN"
    say "Removed $BIN."
  else
    say "$BIN was not there."
  fi
  remove_marker_lines "${ZDOTDIR:-$HOME}/.zshrc"
  remove_marker_lines "$HOME/.zshrc"
  remove_marker_lines "$HOME/.bashrc"
  remove_marker_lines "$HOME/.bash_profile"
  remove_marker_lines "$HOME/.profile"
  say ""
  if [ -d "$DATA_DIR" ]; then
    say "Data folder: $DATA_DIR (database and keys)"
    if ask "Also delete the data folder (database, keys)? This cannot be undone."; then
      case $DATA_DIR in
        '' | / | "$HOME" | "$HOME"/) die "refusing to delete $DATA_DIR" ;;
      esac
      rm -rf "$DATA_DIR" || die "cannot delete $DATA_DIR"
      say "Deleted $DATA_DIR."
    else
      decline datadir
    fi
  else
    say "No data folder at $DATA_DIR."
  fi
  say "Ollama and Tailscale were left alone."
  print_skipped
  say "local-server is uninstalled."
}

main() {
  trap cleanup EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  trap 'exit 129' HUP
  parse_args "$@"
  case $ACTION in
    uninstall) do_uninstall ;;
    *) do_install ;;
  esac
}

main "$@"
