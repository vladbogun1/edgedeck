# Ставит или обновляет скилл Claude Code «edgedeck» из GitHub.
#
#   powershell -ExecutionPolicy Bypass -File skills\sync.ps1            # из main
#   powershell -ExecutionPolicy Bypass -File skills\sync.ps1 -Ref <ветка|тег>
#
# Источник — папка skills/edgedeck приватного репозитория vladbogun1/edgedeck,
# поэтому нужен залогиненный gh (gh auth status). Ставится в
# ~/.claude/skills/edgedeck целиком, прежняя версия заменяется.
param([string]$Ref = "main")
$ErrorActionPreference = "Stop"

$repo = "vladbogun1/edgedeck"
$dest = Join-Path $HOME ".claude\skills\edgedeck"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("edgedeck-skill-" + [guid]::NewGuid().ToString("N"))

try {
    gh repo clone $repo "$tmp" -- --depth 1 --branch $Ref --filter=blob:none --sparse --quiet
    if ($LASTEXITCODE -ne 0) { throw "gh repo clone не удался — проверьте gh auth status" }
    git -C "$tmp" sparse-checkout set skills/edgedeck
    if ($LASTEXITCODE -ne 0) { throw "git sparse-checkout не удался" }
    $src = Join-Path $tmp "skills\edgedeck"
    if (-not (Test-Path (Join-Path $src "SKILL.md"))) { throw "в $repo@$Ref нет skills/edgedeck/SKILL.md" }
    $sha = (git -C "$tmp" rev-parse --short HEAD).Trim()

    New-Item -ItemType Directory -Force (Split-Path $dest) | Out-Null
    if (Test-Path $dest) { Remove-Item -Recurse -Force $dest }
    Copy-Item -Recurse $src $dest
    "$repo@$Ref $sha $(Get-Date -Format s)" | Set-Content -Encoding utf8 (Join-Path $dest ".source")
    Write-Host "скилл edgedeck обновлён: $repo@$Ref ($sha) -> $dest"
}
finally {
    if (Test-Path $tmp) { Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue }
}
