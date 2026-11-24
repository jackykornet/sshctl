# bash completion for sshctl. Enable it with, in ~/.bashrc:
#   eval "$(sshctl completion bash)"
_sshctl() {
	local line=${COMP_LINE:0:COMP_POINT} cur=${COMP_WORDS[COMP_CWORD]}
	[[ $cur == = ]] && cur=
	local -a words
	read -ra words <<<"$line"
	[[ $line == *[[:space:]] ]] && words+=("")
	# bash splits --opt=value at the =, so drop from each candidate what
	# comes before the word bash completes.
	local full=${words[${#words[@]}-1]}
	local strip=${full%"$cur"} c q
	COMPREPLY=()
	while IFS= read -r c; do
		printf -v q '%q' "${c#"$strip"}"
		COMPREPLY+=("$q")
	done < <("${words[0]/#\~/$HOME}" __complete $((${#words[@]} - 2)) "${words[@]:1}" 2>/dev/null)
}
complete -o default -F _sshctl sshctl
