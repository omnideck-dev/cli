param(
    [Parameter(Mandatory = $true)]
    [string]$CertificatePath,
    [Parameter(Mandatory = $true)]
    [string]$RegistryAuthority,
    [int]$TimeoutSeconds = 90
)

$ErrorActionPreference = "Stop"
$Distro = "podman-omnideck-runtime"
$ResolvedCertificate = (Resolve-Path -Path $CertificatePath).Path
$EncodedCertificate = [Convert]::ToBase64String([IO.File]::ReadAllBytes($ResolvedCertificate))
$LinuxCertificate = "/tmp/omnideck-e2e-registry.crt"
$Deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)

Write-Host "Waiting for the Podman machine so the local fixture CA can be installed."
while ([DateTime]::UtcNow -lt $Deadline) {
    & wsl.exe -d $Distro -u root -- true *> $null
    if ($LASTEXITCODE -eq 0) { break }
    Start-Sleep -Seconds 1
}
if ($LASTEXITCODE -ne 0) {
    throw "The $Distro machine did not become available within $TimeoutSeconds seconds."
}

$StageScript = "umask 077; printf '%s' '$EncodedCertificate' | base64 -d > '$LinuxCertificate'"
& wsl.exe -d $Distro -u root -- sh -c $StageScript
if ($LASTEXITCODE -ne 0) {
    throw "Could not stage the local fixture registry CA in $Distro."
}

$InstallScript = @"
set -eu
install -D -m 0644 '$LinuxCertificate' /etc/pki/ca-trust/source/anchors/omnideck-e2e.crt
update-ca-trust
install -D -m 0644 '$LinuxCertificate' '/etc/containers/certs.d/$RegistryAuthority/ca.crt'
"@
& wsl.exe -d $Distro -u root -- sh -c $InstallScript
if ($LASTEXITCODE -ne 0) {
    throw "Could not install the local fixture registry CA in $Distro."
}

$Successes = 0
for ($Attempt = 1; $Attempt -le 240; $Attempt++) {
    $NetworkScript = "curl --fail --silent --max-time 2 --cacert '$LinuxCertificate' 'https://$RegistryAuthority/v2/' >/dev/null 2>&1"
    & wsl.exe -d $Distro -u root -- sh -c $NetworkScript
    if ($LASTEXITCODE -eq 0) {
        $Successes++
        if ($Successes -ge 10) { break }
    } else {
        $Successes = 0
    }
    Start-Sleep -Milliseconds 250
}
if ($Successes -lt 10) {
    throw "The fixture registry never became reachable from $Distro."
}
Write-Host "Installed the local fixture registry CA for $RegistryAuthority."
