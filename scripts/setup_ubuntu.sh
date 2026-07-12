#!/usr/bin/env bash
#
# Installs/maintains all local build prerequisites for car_monitor on Ubuntu.
# Idempotent: safe to re-run; skips anything already installed at the
# pinned version. See ../DESIGN.md section 10 for why each piece exists.
#
# Env vars:
#   SKIP_ANDROID_STUDIO=1   don't install the Android Studio IDE (CI/headless boxes)
#   INSTALL_ROOT=~/car_monitor-toolchain   base dir for downloaded toolchains (default: $HOME)

set -euo pipefail

GO_VERSION="1.22.5"
NDK_VERSION="26.1.10909125"
ANDROID_PLATFORM="android-34"
ANDROID_BUILD_TOOLS="34.0.0"
CMDLINE_TOOLS_URL="https://dl.google.com/android/repository/commandlinetools-linux-11076708_latest.zip"
ANDROID_STUDIO_URL="https://redirector.gvt1.com/edgedl/android/studio/ide-zips/2024.1.1.12/android-studio-2024.1.1.12-linux.tar.gz"

INSTALL_ROOT="${INSTALL_ROOT:-$HOME}"
GO_ROOT="/usr/local/go"
ANDROID_HOME="${INSTALL_ROOT}/Android/sdk"
ANDROID_STUDIO_DIR="/opt/android-studio"
PROFILE_SNIPPET="${HOME}/.car_monitor_env"

log() { printf '\n[setup] %s\n' "$1"; }

need_apt_packages() {
  log "Installing base apt packages (curl, unzip, git, build-essential)"
  sudo apt-get update -y
  sudo apt-get install -y curl unzip git build-essential ca-certificates
}

install_jdk17() {
  if dpkg -l | grep -q openjdk-17-jdk; then
    log "OpenJDK 17 already installed, skipping"
    return
  fi
  log "Installing OpenJDK 17 (Temurin channel via apt)"
  sudo apt-get install -y openjdk-17-jdk
}

install_go() {
  if command -v go >/dev/null 2>&1 && go version | grep -q "go${GO_VERSION}"; then
    log "Go ${GO_VERSION} already installed, skipping"
    return
  fi
  log "Installing Go ${GO_VERSION} to ${GO_ROOT}"
  local tarball="go${GO_VERSION}.linux-amd64.tar.gz"
  curl -fsSL -o "/tmp/${tarball}" "https://go.dev/dl/${tarball}"
  sudo rm -rf "${GO_ROOT}"
  sudo tar -C /usr/local -xzf "/tmp/${tarball}"
  rm -f "/tmp/${tarball}"
}

install_android_sdk() {
  if [ -x "${ANDROID_HOME}/cmdline-tools/latest/bin/sdkmanager" ]; then
    log "Android cmdline-tools already installed, skipping download"
  else
    log "Installing Android cmdline-tools to ${ANDROID_HOME}"
    mkdir -p "${ANDROID_HOME}/cmdline-tools"
    curl -fsSL -o /tmp/cmdline-tools.zip "${CMDLINE_TOOLS_URL}"
    unzip -q -o /tmp/cmdline-tools.zip -d /tmp/cmdline-tools-extract
    rm -rf "${ANDROID_HOME}/cmdline-tools/latest"
    mv /tmp/cmdline-tools-extract/cmdline-tools "${ANDROID_HOME}/cmdline-tools/latest"
    rm -rf /tmp/cmdline-tools.zip /tmp/cmdline-tools-extract
  fi

  local sdkmanager="${ANDROID_HOME}/cmdline-tools/latest/bin/sdkmanager"
  log "Installing platform-tools, platform ${ANDROID_PLATFORM}, build-tools ${ANDROID_BUILD_TOOLS}, NDK ${NDK_VERSION}"
  yes | "${sdkmanager}" --sdk_root="${ANDROID_HOME}" --licenses >/dev/null || true
  "${sdkmanager}" --sdk_root="${ANDROID_HOME}" \
    "platform-tools" \
    "platforms;${ANDROID_PLATFORM}" \
    "build-tools;${ANDROID_BUILD_TOOLS}" \
    "ndk;${NDK_VERSION}"
}

install_gomobile() {
  export PATH="${GO_ROOT}/bin:${PATH}"
  export GOPATH="${INSTALL_ROOT}/go"
  export PATH="${GOPATH}/bin:${PATH}"

  if command -v gomobile >/dev/null 2>&1; then
    log "gomobile already installed, skipping"
  else
    log "Installing gomobile/gobind"
    go install golang.org/x/mobile/cmd/gomobile@latest
    go install golang.org/x/mobile/cmd/gobind@latest
  fi

  log "Running gomobile init"
  ANDROID_HOME="${ANDROID_HOME}" ANDROID_NDK_HOME="${ANDROID_HOME}/ndk/${NDK_VERSION}" gomobile init
}

install_android_studio() {
  if [ -n "${SKIP_ANDROID_STUDIO:-}" ]; then
    log "SKIP_ANDROID_STUDIO set, skipping Android Studio install"
    return
  fi
  if [ -x "${ANDROID_STUDIO_DIR}/bin/studio.sh" ]; then
    log "Android Studio already installed at ${ANDROID_STUDIO_DIR}, skipping"
    return
  fi
  log "Installing Android Studio to ${ANDROID_STUDIO_DIR}"
  curl -fsSL -o /tmp/android-studio.tar.gz "${ANDROID_STUDIO_URL}"
  sudo mkdir -p /opt
  sudo tar -C /opt -xzf /tmp/android-studio.tar.gz
  rm -f /tmp/android-studio.tar.gz
}

write_profile_snippet() {
  log "Writing environment setup to ${PROFILE_SNIPPET}"
  cat > "${PROFILE_SNIPPET}" <<EOF
# car_monitor build toolchain — sourced from ~/.bashrc
export PATH="${GO_ROOT}/bin:\${PATH}"
export GOPATH="${INSTALL_ROOT}/go"
export PATH="\${GOPATH}/bin:\${PATH}"
export ANDROID_HOME="${ANDROID_HOME}"
export ANDROID_NDK_HOME="${ANDROID_HOME}/ndk/${NDK_VERSION}"
export PATH="\${ANDROID_HOME}/cmdline-tools/latest/bin:\${ANDROID_HOME}/platform-tools:\${PATH}"
EOF

  if ! grep -qF "${PROFILE_SNIPPET}" "${HOME}/.bashrc" 2>/dev/null; then
    echo "[ -f \"${PROFILE_SNIPPET}\" ] && source \"${PROFILE_SNIPPET}\"" >> "${HOME}/.bashrc"
  fi
}

configure_git_hooks() {
  local repo_root
  repo_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
  if [ -z "${repo_root}" ]; then
    log "Not inside a git checkout, skipping git hooks config"
    return
  fi
  log "Configuring git to use githooks/ (gofmt/vet/test/build on every commit)"
  git -C "${repo_root}" config core.hooksPath githooks
}

main() {
  need_apt_packages
  install_jdk17
  install_go
  install_android_sdk
  install_gomobile
  install_android_studio
  write_profile_snippet
  configure_git_hooks

  log "Done. Run 'source ~/.bashrc' (or start a new shell) to pick up the toolchain env vars."
}

main "$@"
