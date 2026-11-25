# Install sshctl

sshctl is one executable. It runs your installed OpenSSH client, so every system needs `ssh`, `ssh-add` and `ssh-keygen` on `PATH` as well. Pick your system:

- [Supported systems](#supported-systems)
- [Debian and Ubuntu (apt)](#debian-and-ubuntu-apt)
- [Fedora and RHEL (dnf)](#fedora-and-rhel-dnf)
- [Arch Linux (pacman)](#arch-linux-pacman)
- [openSUSE (zypper)](#opensuse-zypper)
- [Alpine Linux (apk)](#alpine-linux-apk)
- [macOS](#macos)
- [Windows](#windows)
- [Building from source](#building-from-source)
- [Manual download on Linux](#manual-download-on-linux)
- [Removing sshctl's files](#removing-sshctls-files)

Once it works, the [Guide](guide.md) shows what to do with it, and [Shell completion](guide.md#shell-completion) how to complete commands and aliases with `Tab`.

## Supported systems

Each [GitHub Release](https://github.com/jackykornet/sshctl/releases) has an executable and an archive for each of these:

| OS | amd64 | arm64 |
| --- | --- | --- |
| Linux | `sshctl_<version>_linux_amd64.tar.gz` | `sshctl_<version>_linux_arm64.tar.gz` |
| macOS | `sshctl_<version>_darwin_amd64.tar.gz` (Intel) | `sshctl_<version>_darwin_arm64.tar.gz` (Apple silicon) |
| Windows | `sshctl_<version>_windows_amd64.exe` (or `.zip`) | `sshctl_<version>_windows_arm64.exe` (or `.zip`) |

`<version>` is the release tag, such as `v0.7.0`. Each archive holds the same executable, named like the archive without its extension. The Linux executable is static (built without cgo), so it runs on glibc and musl systems alike.

The Workspace needs Windows 10 version 1809 or newer, because it uses ConPTY for its pseudo-terminals. The other commands do not.

## Debian and Ubuntu (apt)

Install the OpenSSH client and the tools `install.sh` uses:

```sh
sudo apt update
sudo apt install openssh-client curl tar
```

Install sshctl with `install.sh`. It downloads the latest Linux release for amd64 or arm64 and installs it to `/usr/local/bin/sshctl` with `sudo`:

```sh
curl -fsSL https://raw.githubusercontent.com/jackykornet/sshctl/main/install.sh | bash
```

To read the script before it runs, download it first:

```sh
curl -fsSL -o install.sh https://raw.githubusercontent.com/jackykornet/sshctl/main/install.sh
less install.sh
bash install.sh
```

Or download it by hand, as in [Manual download on Linux](#manual-download-on-linux).

Check that it worked:

```sh
sshctl --help
```

Uninstall:

```sh
sudo rm /usr/local/bin/sshctl
```

This leaves your config; [Removing sshctl's files](#removing-sshctls-files) says what else sshctl wrote.

## Fedora and RHEL (dnf)

Install the OpenSSH client and the tools `install.sh` uses:

```sh
sudo dnf install openssh-clients curl tar
```

Install sshctl with `install.sh` (to `/usr/local/bin/sshctl`, with `sudo`):

```sh
curl -fsSL https://raw.githubusercontent.com/jackykornet/sshctl/main/install.sh | bash
```

To read the script first, download it and run it with `bash install.sh`, as shown for [Debian and Ubuntu](#debian-and-ubuntu-apt), or download sshctl by hand ([Manual download on Linux](#manual-download-on-linux)).

Check that it worked:

```sh
sshctl --help
```

Uninstall:

```sh
sudo rm /usr/local/bin/sshctl
```

This leaves your config; see [Removing sshctl's files](#removing-sshctls-files).

## Arch Linux (pacman)

Install the OpenSSH client (the `openssh` package) and the tools `install.sh` uses:

```sh
sudo pacman -S --needed openssh curl tar sudo
```

Install sshctl with `install.sh` (to `/usr/local/bin/sshctl`, with `sudo`):

```sh
curl -fsSL https://raw.githubusercontent.com/jackykornet/sshctl/main/install.sh | bash
```

To read the script first, download it and run it with `bash install.sh`, as shown for [Debian and Ubuntu](#debian-and-ubuntu-apt), or download sshctl by hand ([Manual download on Linux](#manual-download-on-linux)).

Check that it worked:

```sh
sshctl --help
```

Uninstall:

```sh
sudo rm /usr/local/bin/sshctl
```

This leaves your config; see [Removing sshctl's files](#removing-sshctls-files).

## openSUSE (zypper)

Install the OpenSSH client and the tools `install.sh` uses:

```sh
sudo zypper install openssh-clients curl tar
```

Install sshctl with `install.sh` (to `/usr/local/bin/sshctl`, with `sudo`):

```sh
curl -fsSL https://raw.githubusercontent.com/jackykornet/sshctl/main/install.sh | bash
```

To read the script first, download it and run it with `bash install.sh`, as shown for [Debian and Ubuntu](#debian-and-ubuntu-apt), or download sshctl by hand ([Manual download on Linux](#manual-download-on-linux)).

Check that it worked:

```sh
sshctl --help
```

Uninstall:

```sh
sudo rm /usr/local/bin/sshctl
```

This leaves your config; see [Removing sshctl's files](#removing-sshctls-files).

## Alpine Linux (apk)

`install.sh` needs bash and sudo, which Alpine does not have by default, so download sshctl by hand. The Linux executable is static, so it runs on Alpine's musl. As root (or with `doas`), install the OpenSSH client and curl:

```sh
apk add openssh-client curl tar
```

Then download and install the release for your architecture, as root:

```sh
VERSION=v0.7.0   # the latest tag on the Releases page
ARCH=amd64       # or arm64
curl -fsSLO "https://github.com/jackykornet/sshctl/releases/download/${VERSION}/sshctl_${VERSION}_linux_${ARCH}.tar.gz"
tar -xzf "sshctl_${VERSION}_linux_${ARCH}.tar.gz"
install -m 755 "sshctl_${VERSION}_linux_${ARCH}" /usr/local/bin/sshctl
```

Check that it worked:

```sh
sshctl --help
```

Uninstall, as root:

```sh
rm /usr/local/bin/sshctl
```

This leaves your config; see [Removing sshctl's files](#removing-sshctls-files).

## macOS

macOS ships the OpenSSH client (`ssh`, `ssh-add`, `ssh-keygen`) and `curl`, so there is nothing to install first. `install.sh` is for Linux only.

Download the archive for your Mac, `arm64` for Apple silicon or `amd64` for Intel, and install the executable to `/usr/local/bin`:

```sh
VERSION=v0.7.0   # the latest tag on the Releases page
ARCH=arm64       # or amd64 on an Intel Mac
curl -fsSLO "https://github.com/jackykornet/sshctl/releases/download/${VERSION}/sshctl_${VERSION}_darwin_${ARCH}.tar.gz"
tar -xzf "sshctl_${VERSION}_darwin_${ARCH}.tar.gz"
sudo mkdir -p /usr/local/bin
sudo install -m 755 "sshctl_${VERSION}_darwin_${ARCH}" /usr/local/bin/sshctl
```

The executable is not signed, so Gatekeeper blocks a copy downloaded with a browser ("cannot be opened because the developer cannot be verified"). Clear the quarantine attribute once:

```sh
sudo xattr -d com.apple.quarantine /usr/local/bin/sshctl
```

(`xattr` says `No such xattr` when the file has none, as with a download made with `curl`; that is fine.)

Check that it worked:

```sh
sshctl --help
```

Uninstall:

```sh
sudo rm /usr/local/bin/sshctl
```

This leaves your config; see [Removing sshctl's files](#removing-sshctls-files).

## Windows

Windows 10 and 11 include the OpenSSH client as an optional feature. Check for it in PowerShell:

```powershell
ssh -V
```

If that fails, add it in **Settings > System > Optional features** ("OpenSSH Client"), or in an administrator PowerShell:

```powershell
Add-WindowsCapability -Online -Name OpenSSH.Client~~~~0.0.1.0
```

Download `sshctl_<version>_windows_amd64.exe` (or `_arm64.exe` on an Arm PC) from the [Releases](https://github.com/jackykornet/sshctl/releases) page, and put it in a folder on your `PATH` as `sshctl.exe`. In PowerShell, for your account only:

```powershell
$dir = "$env:LOCALAPPDATA\Programs\sshctl"
New-Item -ItemType Directory -Force $dir | Out-Null
Move-Item "$HOME\Downloads\sshctl_v0.7.0_windows_amd64.exe" "$dir\sshctl.exe"
$path = [Environment]::GetEnvironmentVariable("Path", "User")
[Environment]::SetEnvironmentVariable("Path", "$path;$dir", "User")
```

Use the file name you downloaded. Open a new terminal so it sees the new `PATH`. The Workspace needs Windows 10 version 1809 or newer; the other commands do not.

Check that it worked:

```powershell
sshctl --help
```

Uninstall: delete `sshctl.exe`, and remove its folder from your `PATH` (**Settings > System > About > Advanced system settings > Environment Variables**):

```powershell
Remove-Item "$env:LOCALAPPDATA\Programs\sshctl" -Recurse
```

This leaves your config; see [Removing sshctl's files](#removing-sshctls-files).

## Building from source

Install Go 1.25 or newer, git and the OpenSSH client (the packages above). Then clone the repository and install:

```sh
git clone https://github.com/jackykornet/sshctl.git
cd sshctl
go install ./cmd/sshctl
```

`go install` puts `sshctl` in `$(go env GOPATH)/bin` (usually `~/go/bin`), which must be on your `PATH`.

Check that it worked:

```sh
sshctl --help
```

Uninstall:

```sh
rm "$(go env GOPATH)/bin/sshctl"
```

This leaves your config; see [Removing sshctl's files](#removing-sshctls-files).

## Manual download on Linux

Without `install.sh`, download the archive for your architecture from the [Releases](https://github.com/jackykornet/sshctl/releases) page and install the executable yourself:

```sh
VERSION=v0.7.0   # the latest tag on the Releases page
ARCH=amd64       # or arm64
curl -fsSLO "https://github.com/jackykornet/sshctl/releases/download/${VERSION}/sshctl_${VERSION}_linux_${ARCH}.tar.gz"
tar -xzf "sshctl_${VERSION}_linux_${ARCH}.tar.gz"
sudo install -m 755 "sshctl_${VERSION}_linux_${ARCH}" /usr/local/bin/sshctl
```

Check and uninstall it as in your distribution's section.

## Removing sshctl's files

Uninstalling the executable leaves what sshctl wrote:

- `~/.config/sshctl/` (or `$XDG_CONFIG_HOME/sshctl/`; `%USERPROFILE%\.config\sshctl\` on Windows): `config.json` and `state.json`. `sshctl config show` prints the paths.
- `~/.ssh/sshctl_config`: the Host blocks sshctl wrote, and the `Include sshctl_config` line at the top of `~/.ssh/config`. Removing them removes those aliases from ssh too, so keep them while you still connect to those hosts.

sshctl never writes private keys, except the one Bootstrap creates when you have none, which stays yours like any other key in `~/.ssh`.
