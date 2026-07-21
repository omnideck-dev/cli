param(
    [ValidateSet("auto", "docker", "podman")]
    [string]$Engine = $(if ($env:OMNIDECK_HARDWARE_ENGINE) { $env:OMNIDECK_HARDWARE_ENGINE } else { "auto" }),
    [int]$Port = $(if ($env:OMNIDECK_HARDWARE_PORT) { [int]$env:OMNIDECK_HARDWARE_PORT } else { 44000 + ($PID % 1000) }),
    [int]$RegistryPort = $(if ($env:OMNIDECK_HARDWARE_REGISTRY_PORT) { [int]$env:OMNIDECK_HARDWARE_REGISTRY_PORT } else { 46000 + ($PID % 1000) }),
    [string]$OutputDirectory = $env:OMNIDECK_HARDWARE_OUTPUT_DIR,
    [string]$CliUnderTest = $env:OMNIDECK_HARDWARE_CLI,
    [switch]$KeepResources
)

$ErrorActionPreference = "Stop"
$ScriptDirectory = Split-Path -Parent $MyInvocation.MyCommand.Path
$RepoRoot = (Resolve-Path (Join-Path $ScriptDirectory "../..")).Path
$RunId = if ($env:GITHUB_RUN_ID) { "$($env:GITHUB_RUN_ID)-$([DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ'))-$PID" } else { "local-$([DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ'))-$PID" }
$SafeRunId = $RunId -replace '[^A-Za-z0-9._-]', ''
$Instance = "omnideck-hw-$SafeRunId"
if (-not $OutputDirectory) { $OutputDirectory = Join-Path $RepoRoot "artifacts/hardware/windows-x64-$SafeRunId" }
$OutputDirectory = [System.IO.Path]::GetFullPath($OutputDirectory)
$TempDirectory = Join-Path ([System.IO.Path]::GetTempPath()) "omnideck-hardware-$([Guid]::NewGuid().ToString('N'))"
$CliPath = if ($CliUnderTest) { $CliUnderTest } else { Join-Path $TempDirectory "omnideck.exe" }
$LogPath = Join-Path $OutputDirectory "hardware-test.log"
$SummaryPath = Join-Path $OutputDirectory "summary.json"
$JunitPath = Join-Path $OutputDirectory "junit.xml"
$ConfigPath = $null
$FixtureImage = $env:OMNIDECK_HARDWARE_TEST_IMAGE
$LocalFixtureImage = $null
$RegistryContainer = "$Instance-registry"
$BuiltFixture = $false
$CurrentStep = "initialization"
$StartedAt = [DateTime]::UtcNow
$TestPassed = $false
$PreviousRegistriesConfig = $env:CONTAINERS_REGISTRIES_CONF
$PreviousOmnideckConfigDir = $env:OMNIDECK_CONFIG_DIR

New-Item -ItemType Directory -Force -Path $OutputDirectory, $TempDirectory | Out-Null
$env:OMNIDECK_CONFIG_DIR = Join-Path $TempDirectory "config"
Start-Transcript -Path $LogPath -Force | Out-Null

function Test-RuntimeReady([string]$Name) {
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) { return $false }
    & $Name info *> $null
    return $LASTEXITCODE -eq 0
}

function Invoke-External([string]$Program, [string[]]$Arguments) {
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Command failed with exit code $LASTEXITCODE`: $Program $($Arguments -join ' ')"
    }
}

function Invoke-Cli([string[]]$Arguments) {
    & $CliPath --no-color --name $Instance @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Omnideck command failed with exit code $LASTEXITCODE`: $($Arguments -join ' ')"
    }
}

function Wait-WebUI {
    for ($Attempt = 1; $Attempt -le 45; $Attempt++) {
        try {
            $Response = Invoke-WebRequest -UseBasicParsing -TimeoutSec 2 -Uri "http://127.0.0.1:$Port"
            if ($Response.Content -notlike "*omnideck hardware fixture ready*") {
                throw "The web UI returned unexpected content."
            }
            $Response.Content | Set-Content -Path (Join-Path $OutputDirectory "web-ui.html")
            return
        } catch {
            Start-Sleep -Seconds 2
        }
    }
    throw "The fixture web UI did not become ready on port $Port."
}

function Wait-Registry {
    for ($Attempt = 1; $Attempt -le 30; $Attempt++) {
        try {
            Invoke-WebRequest -UseBasicParsing -TimeoutSec 2 -Uri "http://127.0.0.1:$RegistryPort/v2/" | Out-Null
            return
        } catch {
            Start-Sleep -Seconds 1
        }
    }
    throw "The temporary image registry did not become ready on port $RegistryPort."
}

function Remove-TestResources {
    if (-not $Instance.StartsWith("omnideck-hw-")) { return }
    if ($script:Engine -in @("docker", "podman") -and (Get-Command $script:Engine -ErrorAction SilentlyContinue)) {
        & $script:Engine rm -f $Instance *> $null
        & $script:Engine rm -f $RegistryContainer *> $null
        & $script:Engine volume rm "$Instance-home" "$Instance-state" *> $null
        if ($BuiltFixture -and $FixtureImage) {
            & $script:Engine rmi -f $FixtureImage *> $null
            & $script:Engine rmi -f $LocalFixtureImage *> $null
        }
    }
    if ($ConfigPath -and (Split-Path -Leaf $ConfigPath) -eq "$Instance.yaml") {
        Remove-Item -Force -ErrorAction SilentlyContinue $ConfigPath
    }
}

try {
    Write-Host "Omnideck hardware lifecycle test"
    Write-Host "Platform: Windows/$env:PROCESSOR_ARCHITECTURE"
    Write-Host "Artifacts: $OutputDirectory"

    if ($Port -lt 1024 -or $Port -gt 65535) { throw "Port must be from 1024 through 65535." }
    if ($RegistryPort -lt 1024 -or $RegistryPort -gt 65535) { throw "RegistryPort must be from 1024 through 65535." }
    if ($Port -eq $RegistryPort) { throw "The web UI and temporary registry ports must be different." }
    if (-not $CliUnderTest -and -not (Get-Command go -ErrorAction SilentlyContinue)) {
        throw "Go is required to build the CLI. Set OMNIDECK_HARDWARE_CLI to test a prebuilt binary instead."
    }

    if ($Engine -eq "auto") {
        if (Test-RuntimeReady "docker") { $Engine = "docker" }
        elseif (Test-RuntimeReady "podman") { $Engine = "podman" }
        else { throw "Neither Docker nor Podman is installed and ready." }
    }
    if (-not (Test-RuntimeReady $Engine)) { throw "$Engine is not installed and ready. Start it, or choose another engine." }

    if ($Engine -eq "podman" -and -not $FixtureImage) {
        $PodmanRegistryConfig = Join-Path $TempDirectory "registries.conf"
        "[[registry]]`nlocation = `"localhost:$RegistryPort`"`ninsecure = true" | Set-Content -Path $PodmanRegistryConfig
        $env:CONTAINERS_REGISTRIES_CONF = $PodmanRegistryConfig
    }

    $CurrentStep = "record runtime information"
    Invoke-External $Engine @("version")

    $CurrentStep = "build CLI"
    if ($CliUnderTest) {
        if (-not (Test-Path -PathType Leaf $CliUnderTest)) { throw "CliUnderTest must point to a CLI binary." }
        $CliPath = (Resolve-Path $CliUnderTest).Path
    } else {
        Push-Location $RepoRoot
        try { Invoke-External "go" @("build", "-o", $CliPath, ".") } finally { Pop-Location }
    }
    Invoke-Cli @("--version")
    Invoke-Cli @("--help")

    $ConfigOutput = & $CliPath --no-color --name $Instance config path
    if ($LASTEXITCODE -ne 0) { throw "The CLI did not report a configuration path." }
    $ConfigPath = ($ConfigOutput | Select-Object -Last 1).Trim()
    if (Test-Path $ConfigPath) { throw "The generated test configuration already exists: $ConfigPath" }

    $CurrentStep = "build fixture image"
    if (-not $FixtureImage) {
        $LocalFixtureImage = "localhost/omnideck-hardware-fixture:$SafeRunId"
        $FixtureImage = "localhost:${RegistryPort}/omnideck-hardware-fixture:$SafeRunId"
        Invoke-External $Engine @("build", "--file", (Join-Path $ScriptDirectory "fixture/Containerfile"), "--tag", $LocalFixtureImage, (Join-Path $ScriptDirectory "fixture"))
        $BuiltFixture = $true
        Invoke-External $Engine @("run", "-d", "--name", $RegistryContainer, "-p", "127.0.0.1:${RegistryPort}:5000", "docker.io/library/registry:2.8.3")
        Wait-Registry
        Invoke-External $Engine @("tag", $LocalFixtureImage, $FixtureImage)
        Invoke-External $Engine @("push", $FixtureImage)
    }

    $CurrentStep = "setup"
    Invoke-Cli @("setup", "--plain", "--engine", $Engine, "--image", $FixtureImage, "--port", "$Port", "--memory", "512m", "--shm-size", "64m")
    $ConfigText = Get-Content -Raw $ConfigPath
    $SettingsPath = Join-Path $env:OMNIDECK_CONFIG_DIR "settings.yaml"
    $SettingsText = Get-Content -Raw $SettingsPath
    if ($SettingsText -notmatch "(?m)^runtime:\s+$([Regex]::Escape($Engine))$") { throw "The shared settings did not record runtime: $Engine." }
    if ($ConfigText -match "(?m)^engine:") { throw "The instance configuration still contains a per-instance runtime." }
    if ($ConfigText -notmatch "(?m)^container_name:\s+$([Regex]::Escape($Instance))$") { throw "The saved configuration has the wrong container name." }

    $CurrentStep = "verify web UI"
    Wait-WebUI

    $CurrentStep = "status"
    Invoke-Cli @("status")

    $CurrentStep = "logs"
    $ContainerLog = & $CliPath --no-color --name $Instance logs --follow=false --tail 20 2>&1
    if ($LASTEXITCODE -ne 0) { throw "logs failed." }
    $ContainerLog | Tee-Object -FilePath (Join-Path $OutputDirectory "container.log")
    if (($ContainerLog -join "`n") -notlike "*omnideck-hardware-fixture-started*") { throw "Expected fixture startup log was not returned." }

    $CurrentStep = "configuration"
    $ConfigShow = & $CliPath --no-color --name $Instance config show 2>&1
    if ($LASTEXITCODE -ne 0) { throw "config show failed." }
    $ConfigShow | Tee-Object -FilePath (Join-Path $OutputDirectory "config-show.log")

    $CurrentStep = "volume persistence"
    Invoke-External $Engine @("exec", $Instance, "sh", "-c", "echo hardware-volume-marker > /home/omnideck/hardware-marker")

    $CurrentStep = "stop"
    Invoke-Cli @("stop")
    $StoppedStatus = & $CliPath --no-color --name $Instance status 2>&1
    $StoppedExitCode = $LASTEXITCODE
    $StoppedStatus | Set-Content -Path (Join-Path $OutputDirectory "status-while-stopped.log")
    if ($StoppedExitCode -eq 0) { throw "status succeeded even though the container was stopped." }

    $CurrentStep = "start"
    Invoke-Cli @("start")
    Wait-WebUI
    $Marker = & $Engine exec $Instance cat /home/omnideck/hardware-marker
    if ($LASTEXITCODE -ne 0 -or $Marker.Trim() -ne "hardware-volume-marker") { throw "The home volume marker did not survive a stop and start." }

    $CurrentStep = "restart"
    Invoke-Cli @("restart")
    Wait-WebUI
    Invoke-Cli @("status")

    $CurrentStep = "doctor"
    $Doctor = & $CliPath --no-color --name $Instance doctor 2>&1
    if ($LASTEXITCODE -ne 0) { throw "doctor failed." }
    $Doctor | Tee-Object -FilePath (Join-Path $OutputDirectory "doctor.log")
    if (($Doctor -join "`n") -notlike "*Omnideck Doctor Report*") { throw "doctor did not render its report." }

    $CurrentStep = "uninstall"
    @("yes", "yes", "no") | & $CliPath --no-color --name $Instance uninstall
    if ($LASTEXITCODE -ne 0) { throw "uninstall failed." }

    $CurrentStep = "verify cleanup"
    & $Engine container inspect $Instance *> $null
    if ($LASTEXITCODE -eq 0) { throw "The container still exists after uninstall." }
    & $Engine volume inspect "$Instance-home" *> $null
    if ($LASTEXITCODE -eq 0) { throw "The home volume still exists after uninstall." }
    & $Engine volume inspect "$Instance-state" *> $null
    if ($LASTEXITCODE -eq 0) { throw "The state volume still exists after uninstall." }
    if (Test-Path $ConfigPath) { throw "The configuration still exists after uninstall." }

    $CurrentStep = "complete"
    $TestPassed = $true
    Write-Host "PASS: lifecycle completed with $Engine on Windows."
} catch {
    Write-Host -ForegroundColor Red "Hardware test failed during '$CurrentStep': $($_.Exception.Message)"
    $TestPassed = $false
} finally {
    if (-not $KeepResources -and $env:OMNIDECK_HARDWARE_KEEP_RESOURCES -ne "1") { Remove-TestResources }

    $Status = if ($TestPassed) { "passed" } else { "failed" }
    @{
        status = $Status
        last_step = $CurrentStep
        platform = "Windows"
        architecture = $env:PROCESSOR_ARCHITECTURE
        engine = $Engine
        instance = $Instance
        started_at = $StartedAt.ToString("o")
        finished_at = [DateTime]::UtcNow.ToString("o")
    } | ConvertTo-Json | Set-Content -Path $SummaryPath

    $Failures = if ($TestPassed) { "0" } else { "1" }
    $FailureElement = if ($TestPassed) { "" } else { "<failure message=`"See hardware-test.log; failed during $CurrentStep`"/>" }
    "<?xml version=`"1.0`" encoding=`"UTF-8`"?><testsuite name=`"omnideck-hardware`" tests=`"1`" failures=`"$Failures`"><testcase classname=`"hardware.Windows`" name=`"lifecycle-$Engine`">$FailureElement</testcase></testsuite>" | Set-Content -Path $JunitPath

    Stop-Transcript | Out-Null
    if ($null -eq $PreviousRegistriesConfig) {
        Remove-Item Env:CONTAINERS_REGISTRIES_CONF -ErrorAction SilentlyContinue
    } else {
        $env:CONTAINERS_REGISTRIES_CONF = $PreviousRegistriesConfig
    }
    if ($null -eq $PreviousOmnideckConfigDir) {
        Remove-Item Env:OMNIDECK_CONFIG_DIR -ErrorAction SilentlyContinue
    } else {
        $env:OMNIDECK_CONFIG_DIR = $PreviousOmnideckConfigDir
    }
    if ((Split-Path -Leaf $TempDirectory) -like "omnideck-hardware-*") {
        Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $TempDirectory
    }
}

if (-not $TestPassed) { exit 1 }
