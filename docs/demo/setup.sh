# Sourced (hidden) by docs/demo/workspace.tape from the repository root.
# Builds sshctl and points HOME, the sshctl config and PATH at a copy of
# docs/demo/home, with docs/demo/bin/ssh in place of OpenSSH, so the
# recording shows only made-up hosts and never reads this machine's config.

demo=$(mktemp -d)
go build -o "$demo/bin/sshctl" ./cmd/sshctl || return 1
cp docs/demo/bin/ssh "$demo/bin/ssh"
chmod +x "$demo/bin/ssh"
cp -R docs/demo/home "$demo/home"
chmod 700 "$demo/home/.ssh"

export HOME="$demo/home"
export XDG_CONFIG_HOME="$HOME/.config"
export PATH="$demo/bin:$PATH"
export PS1='$ '
unset SSH_AUTH_SOCK SSH_CONNECTION SSH_TTY
cd "$HOME"
