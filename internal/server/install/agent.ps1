# SPDX-License-Identifier: Apache-2.0
#
# Installs the Sentinel agent on Windows (§11.2). Run the command from the
# server's Add machine page in PowerShell as Administrator:
#
#   & ([scriptblock]::Create((iwr -UseBasicParsing https://<server>/install/agent.ps1).Content)) `
#     -Server https://<server> -Token <token> -Fingerprint <fp>
#
# It downloads the newest agent build the server hosts for this machine,
# checks its Ed25519 release signature before installing anything
# (SEC-34), installs it under Program Files, then runs
# `sentinel-agent.exe install`, which enrolls the machine and starts the
# "Sentinel Agent" service. Windows PowerShell has no Ed25519, so the check
# is written out below.
param(
    [Parameter(Mandatory = $true)][string]$Server,
    [Parameter(Mandatory = $true)][string]$Token,
    [Parameter(Mandatory = $true)][string]$Fingerprint,
    [string]$TlsFingerprint = '',
    [string]$InstallDir = (Join-Path $env:ProgramFiles 'Sentinel'),
    [switch]$DryRun
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2

# The release keys this server trusts, written in when it serves the script.
$ReleaseKeys = '@RELEASE_KEYS@' -split ' ' | Where-Object { $_ -ne '' }

function Fail([string]$Message) {
    throw "sentinel install: $Message"
}

# ---- Ed25519 verification (RFC 8032), on System.Numerics.BigInteger ----

Add-Type -AssemblyName System.Numerics
$script:P = [System.Numerics.BigInteger]::Pow(2, 255) - 19
$script:L = [System.Numerics.BigInteger]::Pow(2, 252) + [System.Numerics.BigInteger]::Parse('27742317777372353535851937790883648493')

function Mod([System.Numerics.BigInteger]$a, [System.Numerics.BigInteger]$m) {
    $r = [System.Numerics.BigInteger]::Remainder($a, $m)
    if ($r.Sign -lt 0) { $r += $m }
    return $r
}
function Inv([System.Numerics.BigInteger]$a) {
    return [System.Numerics.BigInteger]::ModPow((Mod $a $script:P), $script:P - 2, $script:P)
}
$script:D = Mod ([System.Numerics.BigInteger](-121665) * (Inv 121666)) $script:P
$script:SqrtM1 = [System.Numerics.BigInteger]::ModPow([System.Numerics.BigInteger]2, ($script:P - 1) / 4, $script:P)

# FromLittleEndian reads bytes as an unsigned little-endian number.
function FromLittleEndian([byte[]]$bytes) {
    return New-Object System.Numerics.BigInteger -ArgumentList (,([byte[]]($bytes + [byte]0)))
}

# Points are extended coordinates @(X, Y, Z, T).
function PointAdd($p, $q) {
    $P_ = $script:P
    $a = Mod (($p[1] - $p[0]) * ($q[1] - $q[0])) $P_
    $b = Mod (($p[1] + $p[0]) * ($q[1] + $q[0])) $P_
    $c = Mod ($p[3] * $q[3] * $script:D * 2) $P_
    $d = Mod ($p[2] * $q[2] * 2) $P_
    $e = $b - $a; $f = $d - $c; $g = $d + $c; $h = $b + $a
    return @((Mod ($e * $f) $P_), (Mod ($g * $h) $P_), (Mod ($f * $g) $P_), (Mod ($e * $h) $P_))
}
function ScalarMultiply([System.Numerics.BigInteger]$s, $point) {
    $result = @([System.Numerics.BigInteger]::Zero, [System.Numerics.BigInteger]::One, [System.Numerics.BigInteger]::One, [System.Numerics.BigInteger]::Zero)
    while ($s.Sign -gt 0) {
        if (-not $s.IsEven) { $result = PointAdd $result $point }
        $point = PointAdd $point $point
        $s = $s / 2
    }
    return $result
}
function PointEqual($p, $q) {
    $P_ = $script:P
    return ((Mod ($p[0] * $q[2] - $q[0] * $p[2]) $P_).IsZero -and (Mod ($p[1] * $q[2] - $q[1] * $p[2]) $P_).IsZero)
}
# Decompress reads a 32-byte point encoding, or returns $null.
function Decompress([byte[]]$bytes) {
    if ($bytes.Length -ne 32) { return $null }
    $copy = [byte[]]$bytes.Clone()
    $sign = ($copy[31] -shr 7) -band 1
    $copy[31] = $copy[31] -band 0x7f
    $y = FromLittleEndian $copy
    if ($y -ge $script:P) { return $null }
    $P_ = $script:P
    $u = Mod ($y * $y - 1) $P_
    $v = Mod ($script:D * $y * $y + 1) $P_
    $x2 = Mod ($u * (Inv $v)) $P_
    if ($x2.IsZero) {
        if ($sign -eq 1) { return $null }
        $x = [System.Numerics.BigInteger]::Zero
    } else {
        $x = [System.Numerics.BigInteger]::ModPow($x2, ($P_ + 3) / 8, $P_)
        if (-not (Mod ($x * $x - $x2) $P_).IsZero) { $x = Mod ($x * $script:SqrtM1) $P_ }
        if (-not (Mod ($x * $x - $x2) $P_).IsZero) { return $null }
        if ([int]($x % 2) -ne $sign) { $x = $P_ - $x }
    }
    return @($x, $y, [System.Numerics.BigInteger]::One, (Mod ($x * $y) $P_))
}
$script:Base = Decompress ([byte[]](0x58, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66,
        0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66, 0x66))

function Test-Ed25519([byte[]]$PublicKey, [byte[]]$Message, [byte[]]$Signature) {
    if ($Signature.Length -ne 64) { return $false }
    $a = Decompress $PublicKey
    $r = Decompress ([byte[]]$Signature[0..31])
    if ($null -eq $a -or $null -eq $r) { return $false }
    $s = FromLittleEndian ([byte[]]$Signature[32..63])
    if ($s -ge $script:L) { return $false }
    $sha = [System.Security.Cryptography.SHA512]::Create()
    $k = Mod (FromLittleEndian ($sha.ComputeHash([byte[]]($Signature[0..31] + $PublicKey + $Message)))) $script:L
    $left = ScalarMultiply $s $script:Base
    $right = PointAdd $r (ScalarMultiply $k $a)
    return (PointEqual $left $right)
}

# ---- The install ----

if (-not $DryRun) {
    $principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        Fail 'run PowerShell as Administrator'
    }
}
$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { Fail "Sentinel has no agent for $($env:PROCESSOR_ARCHITECTURE) machines" }
}
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
$work = Join-Path ([IO.Path]::GetTempPath()) ('sentinel-install-' + [Guid]::NewGuid())
New-Item -ItemType Directory -Path $work | Out-Null
try {
    $binary = Join-Path $work 'sentinel-agent.exe'
    $signatureFile = Join-Path $work 'sentinel-agent.sig'
    try {
        Invoke-WebRequest -UseBasicParsing -Uri "$Server/install/agent/windows/$arch/sentinel-agent" -OutFile $binary
        Invoke-WebRequest -UseBasicParsing -Uri "$Server/install/agent/windows/$arch/sentinel-agent.sig" -OutFile $signatureFile
    } catch {
        Fail "couldn't download the agent from ${Server}: $($_.Exception.Message). If the server has no build, an admin can fetch the latest release on its Updates page."
    }

    # The signed manifest must name this product and system and the
    # build's SHA-256, and its signature must be valid for a trusted key.
    $lines = [IO.File]::ReadAllText($signatureFile) -split "`n"
    if ($lines.Length -ne 8 -or $lines[7] -ne '' -or $lines[0] -ne 'sentinel-signature v1' -or $lines[1] -ne 'name: sentinel-agent' -or
        $lines[3] -ne 'os: windows' -or $lines[4] -ne "arch: $arch" -or -not $lines[6].StartsWith('signature: ')) {
        Fail "the downloaded agent's release signature doesn't check out; nothing was installed"
    }
    $digest = (Get-FileHash -Algorithm SHA256 -Path $binary).Hash.ToLowerInvariant()
    if ($lines[5] -ne "sha256: $digest") {
        Fail "the downloaded agent's release signature doesn't check out; nothing was installed"
    }
    $manifest = [Text.Encoding]::UTF8.GetBytes(($lines[0..5] -join "`n") + "`n")
    $signature = [Convert]::FromBase64String($lines[6].Substring('signature: '.Length))
    $trusted = $false
    foreach ($key in $ReleaseKeys) {
        if (Test-Ed25519 ([Convert]::FromBase64String($key)) $manifest $signature) { $trusted = $true; break }
    }
    if (-not $trusted) {
        Fail "the downloaded agent's release signature doesn't check out; nothing was installed"
    }
    $version = $lines[2].Substring('version: '.Length)
    Write-Output "Downloaded sentinel-agent $version for windows/$arch; its signature checks out."

    if ($DryRun) {
        Write-Output "Would install it to $InstallDir\sentinel-agent.exe."
        $agent = $binary
    } else {
        New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
        $agent = Join-Path $InstallDir 'sentinel-agent.exe'
        Copy-Item -Force -Path $binary -Destination $agent
        Write-Output "Installed $agent."
    }
    $arguments = @('install', '--server', $Server, '--token', $Token, '--fingerprint', $Fingerprint)
    if ($TlsFingerprint -ne '') { $arguments += @('--tls-fingerprint', $TlsFingerprint) }
    if ($DryRun) { $arguments += '--dry-run' }
    & $agent @arguments
    if ($LASTEXITCODE -ne 0) { Fail "sentinel-agent install failed with exit code $LASTEXITCODE" }
} finally {
    Remove-Item -Recurse -Force -Path $work -ErrorAction SilentlyContinue
}
