param(
    [Parameter(Mandatory = $true)]
    [string]$CertificatePath,
    [Parameter(Mandatory = $true)]
    [string]$RegistryAuthority,
    [int]$TimeoutSeconds = 300
)

$ErrorActionPreference = "Stop"
$Distro = "podman-omnideck-runtime"
$ResolvedCertificate = (Resolve-Path -Path $CertificatePath).Path
if ($ResolvedCertificate -notmatch '^([A-Za-z]):\\(.*)$') {
    throw "The registry certificate must be on a Windows drive: $ResolvedCertificate"
}
$Drive = $Matches[1].ToLowerInvariant()
$Tail = $Matches[2] -replace '\\', '/'
$LinuxCertificate = "/mnt/$Drive/$Tail"
$Deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)

Write-Host "Waiting for the Podman machine filesystem so the local fixture CA can be installed."
while ([DateTime]::UtcNow -lt $Deadline) {
    & wsl.exe -d $Distro -u root -- sh -c "test -r '$LinuxCertificate'" *> $null
    if ($LASTEXITCODE -eq 0) { break }
    Start-Sleep -Seconds 1
}
if ($LASTEXITCODE -ne 0) {
    throw "The $Distro filesystem did not become available within $TimeoutSeconds seconds."
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
    $Route = (& wsl.exe -d $Distro -u root -- ip -4 route show default 2>$null) -join " "
    if ($Route -match 'default via ([0-9.]+)') {
        $Gateway = $Matches[1]
        $NetworkScript = "grep -v 'host\.containers\.internal' /etc/hosts > /tmp/omnideck-e2e-hosts; cat /tmp/omnideck-e2e-hosts > /etc/hosts; printf '%s host.containers.internal\n' '$Gateway' >> /etc/hosts; curl --fail --silent --max-time 2 --cacert '$LinuxCertificate' 'https://$RegistryAuthority/v2/' >/dev/null 2>&1"
        & wsl.exe -d $Distro -u root -- sh -c $NetworkScript
        if ($LASTEXITCODE -eq 0) {
            $Successes++
            if ($Successes -ge 10) { break }
        } else {
            $Successes = 0
        }
    }
    Start-Sleep -Milliseconds 250
}
if ($Successes -lt 10) {
    throw "The fixture registry never became reachable from $Distro."
}
Write-Host "Installed the local fixture registry CA for $RegistryAuthority."
