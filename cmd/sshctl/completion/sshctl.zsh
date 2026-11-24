#compdef sshctl
# zsh completion for sshctl. Enable it with, in ~/.zshrc after compinit:
#   source <(sshctl completion zsh)
compdef _sshctl sshctl

_sshctl() {
	local -a out
	out=("${(@f)$(${(Q)words[1]} __complete $((CURRENT - 2)) "${(@Q)words[2,CURRENT]}" 2>/dev/null)}")
	out=(${out:#})
	if (( ${#out} )); then
		compadd -- "${out[@]}"
	else
		_files
	fi
}

if [ "$funcstack[1]" = "_sshctl" ]; then
	_sshctl "$@"
fi
