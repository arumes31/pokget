$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$relative = 'scratch/benchmark-corpus-test-' + [guid]::NewGuid().ToString('N')
$directory = Join-Path $root $relative
New-Item -ItemType Directory -Path $directory -Force | Out-Null

# Stop at the first model request: validation must run without contacting Ollama
# or recording results, and accepted corpora must reach this boundary.
function Invoke-RestMethod { throw 'MODEL_ACCESS_REACHED' }
function Test-Corpus($Name, $Corpus, $Valid) {
    $Corpus | ConvertTo-Json -Depth 10 -AsArray | Set-Content (Join-Path $directory 'cases.json')
    $message = ''
    try {
        & "$PSScriptRoot/benchmark-ollama-text.ps1" -Cases "$relative/cases.json" -Output "$relative/output" -Models test
    } catch { $message = $_.Exception.Message }
    if ($Valid) {
        if ($message -ne 'MODEL_ACCESS_REACHED') { throw "${Name}: valid corpus rejected: $message" }
    } elseif ($message -notlike 'Corpus*') { throw "${Name}: invalid corpus reached model access: $message" }
    if (Test-Path (Join-Path $directory 'output/results.json')) { throw "${Name}: validation recorded results" }
    Write-Host "PASS $Name"
}
function New-Corpus([int[]]$Positions) {
    foreach ($position in $Positions) {
        @{ expected = "c$position"; candidates = @(1..5 | ForEach-Object { @{card_id = "c$_"} }) }
    }
}
try {
    Test-Corpus 'insufficient positives' @(New-Corpus @(2)) $false
    Test-Corpus 'unused third position' @(New-Corpus @(1,2,4,5)) $false
    Test-Corpus 'all positions present but imbalanced' @(New-Corpus @(1,1,1,2,3,4,5)) $false
    Test-Corpus 'equal counts' @(New-Corpus @(1,2,3,4,5)) $true
    Test-Corpus 'counts differ by one' @(New-Corpus @(1,1,2,3,4,5)) $true
    Test-Corpus 'checked-in corpus' (Get-Content "$root/testdata/ocr/device-selection.json" -Raw | ConvertFrom-Json -AsHashtable) $true
} finally {
    $resolved = (Resolve-Path -LiteralPath $directory).Path
    $scratch = [IO.Path]::GetFullPath((Join-Path $root 'scratch')) + [IO.Path]::DirectorySeparatorChar
    if (!$resolved.StartsWith($scratch, [StringComparison]::OrdinalIgnoreCase)) { throw 'Unexpected test cleanup path' }
    Remove-Item -LiteralPath $resolved -Recurse -Force
}
