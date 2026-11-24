# PowerShell completion for sshctl. Enable it with, in $PROFILE:
#   sshctl completion powershell | Out-String | Invoke-Expression
Register-ArgumentCompleter -Native -CommandName 'sshctl', 'sshctl.exe' -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)
    $elements = @($commandAst.CommandElements |
        Where-Object { $_.Extent.StartOffset -lt $cursorPosition } |
        ForEach-Object { $_.ToString() })
    $cmd = $elements[0]
    $words = @($elements | Select-Object -Skip 1)
    # The word being completed is the last one, or a new empty one.
    $cword = $words.Count
    if ($wordToComplete -ne '') { $cword = $words.Count - 1 }
    & $cmd __complete $cword @words 2>$null | ForEach-Object {
        $text = $_
        if ($text -match '\s') { $text = "'" + ($text -replace "'", "''") + "'" }
        [System.Management.Automation.CompletionResult]::new($text, $_, 'ParameterValue', $_)
    }
}
