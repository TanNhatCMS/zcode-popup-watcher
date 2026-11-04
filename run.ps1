# run.ps1 -- Icon tray Windows cho deal-checker.ts (chi chay tren Windows).
#
# Khong tu check API, khong claim. No chi lam 3 viec:
#   1. Hien icon tray + menu: "Mo ZCode", "Kiem tra ngay", "Mo log", "Thoat".
#      Menu mo bang bam phai (mac dinh cua NotifyIcon) va ca bam trai
#      (script tu hien menu khi nhan trai, vi ban cu chi co bam phai
#      nen nhieu nguoi bam trai khong thay menu).
#   2. Moi 5s doc file deal-notify.json do daemon Bun ghi; co id moi
#      thi hien BalloonTip. Bam vao bong thong bao thi mo ZCode.
#   3. Nut "Thoat" diet daemon Bun (theo PID file) roi tu thoat.
#      Neu daemon bi dung bang --stop (PID file mat), tray cung tu thoat.
#
# Script tu ghi log khoi dong/thoat/loi vao file LogFile (cung file
# deal-checker.log ma daemon ghi), de biet run.ps1 co chay khong va
# thoat vi ly do gi, thay vi im lang bien mat.
#
# Cach chay (deal-checker.ts --tray se tu goi, khong can goi tay):
#   powershell -NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass `
#     -File run.ps1 -PidFile <pid> -NotifyFile <notify> `
#     -TriggerFile <trigger> -LogFile <log> -TrayPidFile <tray-pid>
# TrayPidFile la tuy chon: file ghi PID cua powershell de lan chay
# sau biet icon dang song, khong tao 2 icon. Script tu xoa file
# nay khi thoat.
#
# Luu y: file nay viet khong dau tieng Viet de tranh loi encoding
# khi chay bang Windows PowerShell 5.1 (UTF-8 khong BOM).

param(
    [Parameter(Mandatory = $true)][string]$PidFile,
    [Parameter(Mandatory = $true)][string]$NotifyFile,
    [Parameter(Mandatory = $true)][string]$TriggerFile,
    [Parameter(Mandatory = $true)][string]$LogFile,
    [Parameter(Mandatory = $false)][string]$TrayPidFile = ""
)

function Write-TrayLog {
    param([string]$Message)
    try {
        if ($LogFile) {
            $stamp = (Get-Date -Format o)
            Add-Content -Path $LogFile -Value "[$stamp] [tray-ui] $Message" -Force -ErrorAction Stop
        }
    } catch {
        # Ghi log hong thi khong duoc lam hong tray.
    }
}

try {
    Add-Type -AssemblyName System.Windows.Forms -ErrorAction Stop
    Add-Type -AssemblyName System.Drawing -ErrorAction Stop
} catch {
    Write-TrayLog "LOI: khong load duoc System.Windows.Forms: $($_.Exception.Message)"
    exit 1
}

$script:LastNotifyId = ""
$script:Exiting = $false
$script:ExitReason = "unknown"
Write-TrayLog "run.ps1 started (powershell PID $PID, PidFile=$PidFile, TrayPidFile=$TrayPidFile)."

function Find-ZCode {
    $candidates = @(
        (Join-Path $env:LOCALAPPDATA "Programs\ZCode\ZCode.exe"),
        (Join-Path $env:LOCALAPPDATA "ZCode\ZCode.exe"),
        (Join-Path $env:ProgramFiles "ZCode\ZCode.exe"),
        (Join-Path ${env:ProgramFiles(x86)} "ZCode\ZCode.exe")
    )
    foreach ($path in $candidates) {
        if ($path -and (Test-Path $path)) { return $path }
    }
    return $null
}

function Open-ZCode {
    $exe = Find-ZCode
    if ($exe) {
        Start-Process -FilePath $exe | Out-Null
    } else {
        $script:Icon.ShowBalloonTip(5000, "ZCode Deal Watcher",
            "Khong tim thay ZCode.exe trong cac duong dan thuong gap. Hay mo ZCode tay.", "Warning")
    }
}

function Exit-Tray {
    if ($script:Exiting) { return }
    $script:Exiting = $true
    $script:ExitReason = "menu Thoat"
    try {
        if (Test-Path $PidFile) {
            $pidText = (Get-Content $PidFile -Raw).Trim()
            $pidValue = 0
            if ([int]::TryParse($pidText, [ref]$pidValue)) {
                $proc = Get-Process -Id $pidValue -ErrorAction SilentlyContinue
                if ($proc) { Stop-Process -Id $pidValue -Force -ErrorAction SilentlyContinue }
            }
            Remove-Item $PidFile -Force -ErrorAction SilentlyContinue
        }
    } catch {
        # Thoat bang moi gia, khong de loi chan viec tat tray.
    }
    $script:Form.Close()
}

$script:Form = New-Object System.Windows.Forms.Form
$script:Form.WindowState = [System.Windows.Forms.FormWindowState]::Hidden
$script:Form.ShowInTaskbar = $false
$script:Form.Visible = $false

$script:Icon = New-Object System.Windows.Forms.NotifyIcon
$script:Icon.Icon = [System.Drawing.SystemIcons]::Information
$script:Icon.Text = "ZCode Deal Watcher"
$script:Icon.Visible = $true

$menu = New-Object System.Windows.Forms.ContextMenuStrip

$itemOpen = $menu.Items.Add("Mo ZCode")
$itemOpen.Add_Click({ Open-ZCode })

$itemCheck = $menu.Items.Add("Kiem tra ngay")
$itemCheck.Add_Click({
    try { Set-Content -Path $TriggerFile -Value (Get-Date -Format o) -Force } catch { }
})

$itemLog = $menu.Items.Add("Mo log")
$itemLog.Add_Click({
    if (Test-Path $LogFile) { Start-Process -FilePath "notepad.exe" -ArgumentList $LogFile | Out-Null }
})

[void]$menu.Items.Add("-")

$itemExit = $menu.Items.Add("Thoat")
$itemExit.Add_Click({ Exit-Tray })

$script:Icon.ContextMenuStrip = $menu
$script:Icon.add_BalloonTipClicked({ Open-ZCode })
# Bam trai vao icon cung mo menu (ban cu chi co bam phai mac dinh,
# nen bam trai khong thay gi, de hieu nham la khong co icon/menu).
$script:Icon.add_MouseClick({
    if ($args[1].Button -eq [System.Windows.Forms.MouseButtons]::Left) {
        $script:Icon.ContextMenuStrip.Show([System.Windows.Forms.Cursor]::Position)
    }
})

$timer = New-Object System.Windows.Forms.Timer
$timer.Interval = 5000
$timer.add_Tick({
    if ($script:Exiting) { return }
    # Daemon bi dung bang --stop: PID file mat -> tray tu thoat.
    if (-not (Test-Path $PidFile)) {
        $script:Exiting = $true
        $script:ExitReason = "PID file mat (daemon da dung bang --stop)"
        $script:Form.Close()
        return
    }
    try {
        $pidText = (Get-Content $PidFile -Raw).Trim()
        $pidValue = 0
        if ([int]::TryParse($pidText, [ref]$pidValue)) {
            $proc = Get-Process -Id $pidValue -ErrorAction SilentlyContinue
            if (-not $proc) {
                $script:Icon.ShowBalloonTip(5000, "ZCode Deal Watcher",
                    "Daemon checker da dung. Tray se thoat.", "Warning")
                $script:Exiting = $true
                $script:ExitReason = "daemon PID chet"
                $script:Form.Close()
                return
            }
        }
    } catch {
        # Doc PID loi: cho vong sau thu lai, khong thoat voi.
    }
    # Co deal moi tu daemon?
    if (Test-Path $NotifyFile) {
        try {
            $note = Get-Content $NotifyFile -Raw | ConvertFrom-Json
            if ($note.id -and $note.id -ne $script:LastNotifyId) {
                $script:LastNotifyId = $note.id
                $script:Icon.ShowBalloonTip(10000, "ZCode - Co deal moi!", $note.message, "Info")
            }
            Remove-Item $NotifyFile -Force -ErrorAction SilentlyContinue
        } catch {
            # File dang ghi do: vong sau doc lai.
        }
    }
})
$timer.Start()

[System.Windows.Forms.Application]::Run($script:Form)
$timer.Stop()
$script:Icon.Visible = $false
$script:Icon.Dispose()
Write-TrayLog "run.ps1 exited (reason: $($script:ExitReason))."
if ($TrayPidFile) {
    Remove-Item $TrayPidFile -Force -ErrorAction SilentlyContinue
}
