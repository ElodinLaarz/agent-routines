# Install agent-routines as a Windows Scheduled Task that runs at logon
# and self-heals every 5 minutes.
#
# Usage:
#   powershell -ExecutionPolicy Bypass -File install.ps1
#   powershell -ExecutionPolicy Bypass -File install.ps1 -Uninstall

param(
    [switch]$Uninstall,
    [string]$Exe = "routines"
)

$ErrorActionPreference = "Stop"
$TaskName = "agent-routines"

if ($Uninstall) {
    if (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue) {
        Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
        Write-Host "removed scheduled task '$TaskName'"
    } else {
        Write-Host "scheduled task '$TaskName' not found; nothing to do"
    }
    return
}

# Resolve $Exe to an absolute path if it's just a name on PATH.
$resolved = (Get-Command $Exe -ErrorAction SilentlyContinue).Source
if (-not $resolved) {
    Write-Error "could not find '$Exe' on PATH; pass -Exe with an absolute path"
    return
}

$LogDir = Join-Path $env:USERPROFILE ".routines"
New-Item -ItemType Directory -Path $LogDir -Force | Out-Null
$Log = Join-Path $LogDir "daemon.log"

# Wrap in cmd /c so we can redirect stdout+stderr to the log file —
# Task Scheduler does not natively redirect a task's streams.
$cmdArg = '/c ""' + $resolved + '" daemon >> "' + $Log + '" 2>&1'
$action = New-ScheduledTaskAction `
    -Execute "cmd.exe" `
    -Argument $cmdArg `
    -WorkingDirectory $env:USERPROFILE

$logon  = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
$repeat = New-ScheduledTaskTrigger -Once `
    -At (Get-Date).AddMinutes(1) `
    -RepetitionInterval (New-TimeSpan -Minutes 5)

$settings = New-ScheduledTaskSettingsSet `
    -AllowStartIfOnBatteries `
    -DontStopIfGoingOnBatteries `
    -StartWhenAvailable `
    -ExecutionTimeLimit (New-TimeSpan -Hours 0)

Register-ScheduledTask -TaskName $TaskName `
    -Action $action `
    -Trigger @($logon, $repeat) `
    -Settings $settings `
    -Force | Out-Null

Start-ScheduledTask -TaskName $TaskName
Write-Host "installed scheduled task '$TaskName'. logs: $Log"
