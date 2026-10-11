# Ставит или обновляет скиллы Claude Code из GitHub: все папки skills/*/ с SKILL.md
# (edgedeck, cloudflare-dns, …).
#
#   powershell -ExecutionPolicy Bypass -File skills\sync.ps1            # из main
#   powershell -ExecutionPolicy Bypass -File skills\sync.ps1 -Ref <ветка|тег>
#
# Источник — папка skills/ приватного репозитория vladbogun1/edgedeck, поэтому нужен
# залогиненный gh (gh auth status). Каждый скилл ставится в ~/.claude/skills/<имя>
# целиком, прежняя версия заменяется.
param([string]$Ref = "main")
$ErrorActionPreference = "Stop"

$repo = "vladbogun1/edgedeck"
$skillsRoot = Join-Path $HOME ".claude\skills"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("edgedeck-skill-" + [guid]::NewGuid().ToString("N"))

try {
    gh repo clone $repo "$tmp" -- --depth 1 --branch $Ref --filter=blob:none --sparse --quiet
    if ($LASTEXITCODE -ne 0) { throw "gh repo clone не удался — проверьте gh auth status" }
    git -C "$tmp" sparse-checkout set skills
    if ($LASTEXITCODE -ne 0) { throw "git sparse-checkout не удался" }
    $sha = (git -C "$tmp" rev-parse --short HEAD).Trim()
    $found = Get-ChildItem (Join-Path $tmp "skills") -Directory | Where-Object { Test-Path (Join-Path $_.FullName "SKILL.md") }
    if (-not $found) { throw "в $repo@$Ref нет skills/*/SKILL.md" }
    New-Item -ItemType Directory -Force $skillsRoot | Out-Null
    foreach ($d in $found) {
        $dest = Join-Path $skillsRoot $d.Name
        if (Test-Path $dest) { Remove-Item -Recurse -Force $dest }
        Copy-Item -Recurse $d.FullName $dest
        "$repo@$Ref $sha $(Get-Date -Format s)" | Set-Content -Encoding utf8 (Join-Path $dest ".source")
        Write-Host "скилл $($d.Name) обновлён: $repo@$Ref ($sha) -> $dest"
    }
}
finally {
    if (Test-Path $tmp) { Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue }
}
