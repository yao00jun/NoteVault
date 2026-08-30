Add-Type @"
using System;
using System.Runtime.InteropServices;
public class WinHelper {
    [StructLayout(LayoutKind.Sequential)]
    public struct Rect { public int Left; public int Top; public int Right; public int Bottom; }
    [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out Rect r);
    [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
    [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
    [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h, int n);
    [DllImport("user32.dll")] public static extern bool MoveWindow(IntPtr h, int x, int y, int w, int h2, bool repaint);
}
"@

$proc = Get-Process -Name "notevault" -ErrorAction SilentlyContinue | Select-Object -First 1
if (-not $proc) { Write-Output "ERROR: Process not found"; exit 1 }

$hWnd = $proc.MainWindowHandle
Write-Output "PID: $($proc.Id)"
Write-Output "Handle: $hWnd"

$rect = New-Object WinHelper+Rect
[WinHelper]::GetWindowRect($hWnd, [ref]$rect) | Out-Null
$visible = [WinHelper]::IsWindowVisible($hWnd)
$width = $rect.Right - $rect.Left
$height = $rect.Bottom - $rect.Top

Write-Output "Visible: $visible"
Write-Output "Position: ($($rect.Left), $($rect.Top))"
Write-Output "Size: ${width}x${height}"

# 检查是否在屏幕外
$screenWidth = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds.Width
$screenHeight = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds.Height
Write-Output "Screen: ${screenWidth}x${screenHeight}"

if ($rect.Left -lt -100 -or $rect.Top -lt -100 -or $rect.Left -gt $screenWidth -or $rect.Top -gt $screenHeight) {
    Write-Output "WARNING: Window may be off-screen, moving to (100,100)..."
    [WinHelper]::MoveWindow($hWnd, 100, 100, 1280, 800, $true) | Out-Null
}

# 激活窗口
Write-Output "Activating window..."
[WinHelper]::ShowWindow($hWnd, 9) | Out-Null  # SW_RESTORE
[WinHelper]::SetForegroundWindow($hWnd) | Out-Null
Write-Output "Done"
