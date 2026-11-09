<#
.SYNOPSIS
    让 WSL2 发行版保持运行，使 NanoPanel 在 Windows 侧随时可访问。

.DESCRIPTION
    WSL2 在没有活动会话/进程时会自动停止整个发行版，systemd 与面板服务会一起停掉，
    此时 Windows 浏览器访问 localhost:8080 会超时。本脚本注册一个「登录时启动」的
    Windows 计划任务，运行 `wsl -d <发行版> -e sleep infinity` 保持发行版常驻，
    由 systemd 自动拉起已 enable 的 nanopanel.service。

    这是可选操作，会修改当前用户的计划任务；如不需要，卸载方式见 -Remove。
    替代方案：手动开一个 WSL 终端窗口，或在 %USERPROFILE%\.wslconfig 中设置
    [wsl2] vmIdleTimeout=-1（该设置影响本机所有 WSL 发行版）。

.EXAMPLE
    powershell -ExecutionPolicy Bypass -File .\wsl-keepalive.ps1
    powershell -ExecutionPolicy Bypass -File .\wsl-keepalive.ps1 -Distro Ubuntu2404
    powershell -ExecutionPolicy Bypass -File .\wsl-keepalive.ps1 -Remove
#>
param(
    [string]$Distro = "Ubuntu2404",
    [switch]$Remove
)

$TaskName = "NanoPanel-WSL-Keepalive"

if ($Remove) {
    $existing = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    if ($null -eq $existing) {
        Write-Host "未找到任务 $TaskName"
        return
    }
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
    Write-Host "已移除任务 $TaskName。WSL 发行版将在空闲后自动停止。"
    return
}

$wslPath = (Get-Command wsl.exe -ErrorAction Stop).Source
$action = New-ScheduledTaskAction -Execute $wslPath -Argument "-d $Distro -e sleep infinity"
$trigger = New-ScheduledTaskTrigger -AtLogOn
$settings = New-ScheduledTaskSettingsSet `
    -AllowStartIfOnBatteries `
    -DontStopIfGoingOnBatteries `
    -ExecutionTimeLimit ([TimeSpan]::Zero) `
    -RestartCount 3 `
    -RestartInterval (New-TimeSpan -Minutes 1)

Register-ScheduledTask `
    -TaskName $TaskName `
    -Action $action `
    -Trigger $trigger `
    -Settings $settings `
    -Description "保持 WSL2 发行版 $Distro 运行，以便 NanoPanel 面板持续可用" `
    -Force | Out-Null

Start-ScheduledTask -TaskName $TaskName
Write-Host "已注册并启动任务 $TaskName"
Write-Host "面板地址: http://localhost:8080"
Write-Host "卸载: powershell -ExecutionPolicy Bypass -File .\wsl-keepalive.ps1 -Remove"
