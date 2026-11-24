# fish completion for sshctl. Enable it with:
#   sshctl completion fish > ~/.config/fish/completions/sshctl.fish
function __sshctl_complete
    set -l words (commandline -opc)
    set -l cmd $words[1]
    set -e words[1]
    set -l cur (commandline -ct)
    set -l out ($cmd __complete (count $words) $words $cur 2>/dev/null)
    if test (count $out) -gt 0
        printf '%s\n' $out
    else
        __fish_complete_path "$cur"
    end
end
complete -c sshctl -f -a '(__sshctl_complete)'
