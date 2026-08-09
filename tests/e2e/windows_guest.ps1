param(
    [Parameter(Mandatory = $true)]
    [ValidateSet("Prepare", "Installed", "Final")]
    [string]$Phase,
    [Parameter(Mandatory = $true)]
    [string]$WorkDir,
    [Parameter(Mandatory = $true)]
    [string]$ExpectedVersion,
    [Parameter(Mandatory = $true)]
    [string]$FixtureImage
)

$ErrorActionPreference = "Stop"
$ResultDir = Join-Path $WorkDir "results"
$ConfigDir = Join-Path $WorkDir "config"
$Binary = Join-Path $WorkDir "bin\omnideck.exe"
$Archive = Join-Path $WorkDir "omnideck-windows-amd64.zip"
$ChecksumFile = Join-Path $WorkDir "SHA256SUMS"
$ReleaseContract = Join-Path $WorkDir "releasecontract.exe"
$ContractsArchive = Join-Path $WorkDir "contracts.tar.gz"
$HardwareRun = Join-Path $WorkDir "hardware-run.ps1"
$EvidenceArchive = Join-Path $WorkDir "evidence.zip"
$StartedAt = [DateTime]::UtcNow
$CurrentStep = "initialization"
$Failure = $null

New-Item -ItemType Directory -Force -Path $ResultDir, $ConfigDir | Out-Null

function Invoke-External([string]$Program, [string[]]$Arguments) {
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Command failed with exit code $LASTEXITCODE`: $Program $($Arguments -join ' ')"
    }
}

function Invoke-Cli([string[]]$Arguments) {
    & $Binary --no-color --name omnideck @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Omnideck command failed with exit code $LASTEXITCODE`: $($Arguments -join ' ')"
    }
}

function Write-Inventory([string]$Suffix) {
    $Lines = [System.Collections.Generic.List[string]]::new()
    $Lines.Add("timestamp=$([DateTime]::UtcNow.ToString('o'))")
    $Lines.Add("os=$((Get-CimInstance Win32_OperatingSystem).Caption)")
    $Lines.Add("architecture=$env:PROCESSOR_ARCHITECTURE")
    $WslFeature = Get-WindowsOptionalFeature -Online -FeatureName Microsoft-Windows-Subsystem-Linux
    $VirtualMachineFeature = Get-WindowsOptionalFeature -Online -FeatureName VirtualMachinePlatform
    $Lines.Add("wsl_feature=$($WslFeature.State)")
    $Lines.Add("virtual_machine_platform=$($VirtualMachineFeature.State)")
    if (Get-Command podman.exe -ErrorAction SilentlyContinue) {
        $Lines.Add("podman=$((& podman.exe --version 2>&1) -join ' ')")
        $Lines.AddRange([string[]]@(& podman.exe ps --all --format "container={{.Names}}|{{.Status}}" 2>&1))
        $Lines.AddRange([string[]]@(& podman.exe volume ls --format "volume={{.Name}}" 2>&1))
        $Lines.AddRange([string[]]@(& podman.exe images --format "image={{.Repository}}:{{.Tag}}|{{.ID}}" 2>&1))
    } else {
        $Lines.Add("podman=absent")
    }
    $Lines | Set-Content -Encoding UTF8 -Path (Join-Path $ResultDir "inventory-$Suffix.txt")
}

function Remove-PrimaryResources {
    $PreviousPreference = $ErrorActionPreference
    $ErrorActionPreference = "SilentlyContinue"
    if (Get-Command podman.exe -ErrorAction SilentlyContinue) {
        & podman.exe rm -f omnideck *> $null
        & podman.exe volume rm omnideck-home omnideck-state *> $null
    }
    $PrimaryConfig = Join-Path $ConfigDir "instances\omnideck.yaml"
    Remove-Item -Force -ErrorAction SilentlyContinue $PrimaryConfig
    $ErrorActionPreference = $PreviousPreference
}

function Test-PodmanObjectAbsent([string[]]$Arguments) {
    $PreviousPreference = $ErrorActionPreference
    $ErrorActionPreference = "SilentlyContinue"
    try {
        & podman.exe @Arguments *> $null
        return $LASTEXITCODE -ne 0
    } finally {
        $ErrorActionPreference = $PreviousPreference
    }
}

function Write-SuiteResult([bool]$Passed, [string]$LastStep) {
    $Status = if ($Passed) { "passed" } else { "failed" }
    @{
        status = $Status
        lastStep = $LastStep
        expectedVersion = $ExpectedVersion
        fixtureImage = $FixtureImage
        startedAt = $StartedAt.ToString("o")
        finishedAt = [DateTime]::UtcNow.ToString("o")
        platform = "windows/amd64"
    } | ConvertTo-Json | Set-Content -Encoding UTF8 -Path (Join-Path $ResultDir "summary.json")

    if ($Passed) {
        $Xml = '<?xml version="1.0" encoding="UTF-8"?><testsuite name="omnideck-vm-e2e" tests="3" failures="0"><testcase classname="vm-e2e" name="portable-cli-contract"/><testcase classname="vm-e2e" name="clean-install-and-tui"/><testcase classname="vm-e2e" name="unattended-lifecycle"/></testsuite>'
    } else {
        $SafeStep = [System.Security.SecurityElement]::Escape($LastStep)
        $Xml = "<?xml version=`"1.0`" encoding=`"UTF-8`"?><testsuite name=`"omnideck-vm-e2e`" tests=`"1`" failures=`"1`"><testcase classname=`"vm-e2e`" name=`"$SafeStep`"><failure message=`"See the Windows phase logs and terminal transcripts`"/></testcase></testsuite>"
    }
    $Xml | Set-Content -Encoding UTF8 -Path (Join-Path $ResultDir "junit.xml")
}

function Compress-Evidence {
    Remove-Item -Force -ErrorAction SilentlyContinue $EvidenceArchive
    Compress-Archive -CompressionLevel Optimal -Path (Join-Path $ResultDir "*") -DestinationPath $EvidenceArchive
}

$TranscriptPath = Join-Path $ResultDir "windows-$($Phase.ToLowerInvariant()).log"
Start-Transcript -Path $TranscriptPath -Force | Out-Null
try {
    switch ($Phase) {
        "Prepare" {
            $CurrentStep = "clean-host precondition"
            Write-Inventory "before"
            if (Get-Command podman.exe -ErrorAction SilentlyContinue) {
                throw "The Windows install scenario requires a clean guest with Podman absent."
            }
            if (Test-Path (Join-Path $ConfigDir "instances\omnideck.yaml")) {
                throw "The isolated test configuration unexpectedly contains an existing instance."
            }

            $CurrentStep = "install release archive"
            $ChecksumLine = Get-Content $ChecksumFile | Where-Object { $_ -match 'omnideck-windows-amd64\.zip$' } | Select-Object -First 1
            if (-not $ChecksumLine) { throw "SHA256SUMS does not name the Windows archive." }
            $ExpectedHash = ($ChecksumLine -split '\s+')[0]
            $ActualHash = (Get-FileHash -Algorithm SHA256 -Path $Archive).Hash
            if ($ExpectedHash -ne $ActualHash) { throw "The Windows archive checksum did not match SHA256SUMS." }
            Remove-Item -Recurse -Force -ErrorAction SilentlyContinue (Join-Path $WorkDir "bin")
            Expand-Archive -Force -Path $Archive -DestinationPath (Join-Path $WorkDir "bin")
            $VersionText = (& $Binary --version 2>&1) -join "`n"
            $VersionText | Set-Content -Encoding UTF8 -Path (Join-Path $ResultDir "version.txt")
            if ($LASTEXITCODE -ne 0 -or $VersionText -notlike "*omnideck version $ExpectedVersion*") {
                throw "The packaged CLI did not report the expected version."
            }
            $InstallHelp = (& $Binary install --help 2>&1) -join "`n"
            $InstallHelp | Set-Content -Encoding UTF8 -Path (Join-Path $ResultDir "install-help.txt")
            if ($LASTEXITCODE -ne 0 -or $InstallHelp -notlike '*Walks through setting up one Omnideck instance.*' -or $InstallHelp -notlike '*add, install, setup*') {
                throw "The packaged CLI did not expose the expected install command and aliases."
            }

            $CurrentStep = "portable CLI contract"
            Invoke-External "tar.exe" @("-xzf", $ContractsArchive, "-C", $WorkDir)
            Invoke-External $ReleaseContract @(
                "--binary", $Binary,
                "--mode", "portable",
                "--expected-version", $ExpectedVersion,
                "--expected-os", "windows",
                "--expected-arch", "amd64",
                "--contracts", (Join-Path $WorkDir "contracts"),
                "--report", (Join-Path $ResultDir "portable-contract.json"),
                "--junit", (Join-Path $ResultDir "portable-contract.xml")
            )
        }

        "Installed" {
            $env:OMNIDECK_CONFIG_DIR = $ConfigDir
            $CurrentStep = "installed behavior"
            (& podman.exe info 2>&1) | Set-Content -Encoding UTF8 -Path (Join-Path $ResultDir "podman-info.txt")
            if ($LASTEXITCODE -ne 0) { throw "Podman is not ready after attended installation." }
            $StatusText = (& $Binary --no-color --name omnideck status 2>&1) -join "`n"
            $StatusText | Set-Content -Encoding UTF8 -Path (Join-Path $ResultDir "status.txt")
            if ($LASTEXITCODE -ne 0 -or $StatusText -notlike '*running*') { throw "The attended instance is not running." }
            $WebResponse = Invoke-WebRequest -UseBasicParsing -TimeoutSec 10 -Uri "http://127.0.0.1:2337"
            $WebResponse.Content | Set-Content -Encoding UTF8 -Path (Join-Path $ResultDir "web-ui.html")
            if ($WebResponse.Content -notlike '*omnideck hardware fixture ready*') { throw "The fixture web UI returned unexpected content." }
            (& podman.exe container inspect omnideck 2>&1) | Set-Content -Encoding UTF8 -Path (Join-Path $ResultDir "container-inspect.json")
            if ($LASTEXITCODE -ne 0) { throw "The attended container was not created." }
            (& podman.exe volume inspect omnideck-home omnideck-state 2>&1) | Set-Content -Encoding UTF8 -Path (Join-Path $ResultDir "volume-inspect.json")
            if ($LASTEXITCODE -ne 0) { throw "The attended persistent volumes were not created." }
            $Settings = Get-Content -Raw (Join-Path $ConfigDir "settings.yaml")
            $InstanceConfig = Get-Content -Raw (Join-Path $ConfigDir "instances\omnideck.yaml")
            if ($Settings -notmatch '(?m)^runtime:\s+podman$') { throw "Shared settings did not select Podman." }
            if ($InstanceConfig -notmatch '(?m)^container_name:\s+omnideck$') { throw "The instance config has the wrong container name." }
            if ($InstanceConfig -notlike "*image: $FixtureImage*") { throw "The instance config has the wrong fixture image." }

            $CurrentStep = "unattended command and update contract"
            $ListText = (& $Binary --json list 2>&1) -join "`n"
            if ($LASTEXITCODE -ne 0) { throw "JSON list failed." }
            $ListText | Set-Content -Encoding UTF8 -Path (Join-Path $ResultDir "list.json")
            $Instances = @($ListText | ConvertFrom-Json)
            if ($Instances.Count -ne 1 -or $Instances[0].name -ne "omnideck") { throw "JSON list returned the wrong instance." }
            $StatusJsonText = (& $Binary --json --name omnideck status 2>&1) -join "`n"
            if ($LASTEXITCODE -ne 0) { throw "JSON status failed." }
            $StatusJsonText | Set-Content -Encoding UTF8 -Path (Join-Path $ResultDir "status.json")
            $StatusJson = $StatusJsonText | ConvertFrom-Json
            if ($StatusJson.container -ne "omnideck" -or $StatusJson.status -ne "running") { throw "JSON status returned the wrong state." }
            Invoke-External "podman.exe" @("exec", "omnideck", "sh", "-c", "printf '%s\n' update-volume-marker > /home/omnideck/update-volume-marker")
            $ConfigSet = (& $Binary --no-color --name omnideck config set memory 512m 2>&1) -join "`n"
            $ConfigSet | Set-Content -Encoding UTF8 -Path (Join-Path $ResultDir "config-set.txt")
            if ($LASTEXITCODE -ne 0 -or $ConfigSet -notlike '*Set memory = 512m*') { throw "Unattended config set changed behavior." }
            $UpdateText = (& $Binary --no-color --name omnideck update --plain 2>&1) -join "`n"
            $UpdateText | Set-Content -Encoding UTF8 -Path (Join-Path $ResultDir "update-plain.txt")
            if ($LASTEXITCODE -ne 0 -or $UpdateText -notlike '*Omnideck is up to date: http://localhost:2337*') { throw "Same-image update changed behavior." }
            $Marker = (& podman.exe exec omnideck cat /home/omnideck/update-volume-marker 2>&1) -join "`n"
            if ($LASTEXITCODE -ne 0 -or $Marker.Trim() -ne "update-volume-marker") { throw "The volume marker did not survive update." }
        }

        "Final" {
            $env:OMNIDECK_CONFIG_DIR = $ConfigDir
            $CurrentStep = "removal cleanup contract"
            if (-not (Test-PodmanObjectAbsent @("container", "inspect", "omnideck"))) { throw "The TUI removal left the primary container behind." }
            if (-not (Test-PodmanObjectAbsent @("volume", "inspect", "omnideck-home"))) { throw "The TUI removal left the home volume behind." }
            if (-not (Test-PodmanObjectAbsent @("volume", "inspect", "omnideck-state"))) { throw "The TUI removal left the state volume behind." }
            if (Test-Path (Join-Path $ConfigDir "instances\omnideck.yaml")) { throw "The TUI removal left the instance config behind." }

            $CurrentStep = "unattended CLI lifecycle"
            $env:OMNIDECK_HARDWARE_CLI = $Binary
            $env:OMNIDECK_HARDWARE_ENGINE = "podman"
            $env:OMNIDECK_HARDWARE_TEST_IMAGE = $FixtureImage
            $env:OMNIDECK_HARDWARE_OUTPUT_DIR = Join-Path $ResultDir "unattended"
            & powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File $HardwareRun
            if ($LASTEXITCODE -ne 0) { throw "The unattended Windows lifecycle failed." }
            $HardwareSummary = Get-Content -Raw (Join-Path $ResultDir "unattended\summary.json") | ConvertFrom-Json
            if ($HardwareSummary.status -ne "passed") { throw "The unattended Windows lifecycle did not report passed." }
            Write-Inventory "after"
            $CurrentStep = "complete"
        }
    }
} catch {
    $Failure = $_
    Write-Host -ForegroundColor Red "Windows VM E2E failed during '$CurrentStep': $($_.Exception.Message)"
    if ($env:OMNIDECK_E2E_KEEP_GUEST_STATE -ne "1") { Remove-PrimaryResources }
} finally {
    Stop-Transcript | Out-Null
}

if ($Failure) {
    Write-SuiteResult $false $CurrentStep
    Compress-Evidence
    Write-Error $Failure
    exit 1
}
if ($Phase -eq "Final") {
    Write-SuiteResult $true $CurrentStep
    Compress-Evidence
    Write-Host "PASS: portable, attended terminal, and unattended CLI journeys completed on Windows."
}
