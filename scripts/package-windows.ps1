<#
.SYNOPSIS
    构建并封装 My Podcasts 的 Windows 发行版产物（NSIS 安装包 Setup.exe + 绿色便携版 Portable.zip）。
.EXAMPLE
    .\scripts\package-windows.ps1
    .\scripts\package-windows.ps1 -Version 1.0.0 -Arch amd64
#>
param(
    [string]$Version = "",
    [ValidateSet("amd64", "arm64")]
    [string]$Arch = "amd64",
    [switch]$SkipTests
)

$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $PSScriptRoot
Push-Location $Root
try {
    # 自动补齐本地 Go SDK PATH（若默认 PATH 尚未配置）
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        $sdkBins = Get-ChildItem "$env:USERPROFILE\sdk\go*\go\bin" -Directory -ErrorAction SilentlyContinue | Sort-Object Name -Descending
        if ($sdkBins) {
            $env:PATH = "$($sdkBins[0].FullName);$env:PATH"
        }
    }
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        throw "未找到 Go 编译器，请先安装 Go 1.27+ 并将其加入 PATH。"
    }

    $cfg = Get-Content -Raw -Encoding UTF8 (Join-Path $Root "mygo.json") | ConvertFrom-Json
    if ([string]::IsNullOrWhiteSpace($Version)) {
        $Version = $cfg.version
    }
    $Slug = if ($cfg.slug) { $cfg.slug } else { "my-podcasts" }
    $AppName = if ($cfg.name) { $cfg.name } else { "My Podcasts" }

    $env:CGO_ENABLED = "0"

    if (-not $SkipTests) {
        Write-Host "==> 运行 go vet 与单元测试..." -ForegroundColor Cyan
        go vet ./...
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
        go test -count=1 ./...
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    }

    $MyGoVer = (go list -m -f '{{.Version}}' github.com/egoist/mygo).Trim()
    if ([string]::IsNullOrWhiteSpace($MyGoVer)) {
        $MyGoVer = "latest"
    }

    Write-Host "==> 使用 mygo@$MyGoVer build 构建 Windows GUI 程序与 NSIS Setup 安装包 (windows/$Arch)..." -ForegroundColor Cyan
    go run "github.com/egoist/mygo/cmd/mygo@$MyGoVer" build -platform "windows/$Arch"
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    $PlatformDir = Join-Path $Root "dist\windows-$Arch"
    $ReleaseDir = Join-Path $Root "dist\release"
    $PortableStage = Join-Path $Root "dist\.portable-stage-$Arch"

    if (Test-Path $ReleaseDir) { Remove-Item -Recurse -Force $ReleaseDir }
    if (Test-Path $PortableStage) { Remove-Item -Recurse -Force $PortableStage }
    New-Item -ItemType Directory -Path $ReleaseDir | Out-Null
    New-Item -ItemType Directory -Path $PortableStage | Out-Null

    $VerTag = if ($Version -match '^[0-9]') { "v$Version" } else { $Version }

    $SetupSrc = Join-Path $PlatformDir "$Slug-setup-$Version-$Arch.exe"
    if (-not (Test-Path $SetupSrc)) {
        throw "未找到生成的安装包：$SetupSrc"
    }
    $SetupDest = Join-Path $ReleaseDir "$Slug-setup-$VerTag-windows-$Arch.exe"
    Copy-Item -Force $SetupSrc $SetupDest

    $ExeSrc = Join-Path $PlatformDir "$AppName.exe"
    if (-not (Test-Path $ExeSrc)) {
        throw "未找到生成的主程序：$ExeSrc"
    }

    Write-Host "==> 组装便携版压缩包 (Portable ZIP)..." -ForegroundColor Cyan
    Copy-Item -Force $ExeSrc (Join-Path $PortableStage "$AppName.exe")
    if (Test-Path (Join-Path $Root "README.md")) {
        Copy-Item -Force (Join-Path $Root "README.md") (Join-Path $PortableStage "README.md")
    }
    if (Test-Path (Join-Path $Root "LICENSE")) {
        Copy-Item -Force (Join-Path $Root "LICENSE") (Join-Path $PortableStage "LICENSE")
    }

    $PortableNote = @"
My Podcasts 便携模式标记文件 (Portable Mode)
==========================================
只要本文件与 "$AppName.exe" 位于同一目录，所有本地配置、播放记忆与加密登录凭据
均保存在当前目录的 data\ 文件夹中，不会写入系统 %APPDATA%。

若希望改回系统标准用户目录，直接删除本文件即可。
"@
    Set-Content -Path (Join-Path $PortableStage "portable.txt") -Value $PortableNote -Encoding UTF8

    $ZipDest = Join-Path $ReleaseDir "$Slug-portable-$VerTag-windows-$Arch.zip"
    Compress-Archive -Path (Join-Path $PortableStage "*") -DestinationPath $ZipDest -CompressionLevel Optimal -Force
    Remove-Item -Recurse -Force $PortableStage

    Write-Host "==> 生成 SHA-256 校验文件..." -ForegroundColor Cyan
    $HashLines = @()
    foreach ($file in @($SetupDest, $ZipDest)) {
        $h = (Get-FileHash -Algorithm SHA256 $file).Hash.ToLowerInvariant()
        $HashLines += "$h  $(Split-Path -Leaf $file)"
    }
    $SumFile = Join-Path $ReleaseDir "SHA256SUMS.txt"
    Set-Content -Path $SumFile -Value ($HashLines -join "`r`n") -Encoding ASCII

    Write-Host ""
    Write-Host "打包完成！发行版产物位于 dist\release\：" -ForegroundColor Green
    Get-ChildItem $ReleaseDir | Select-Object Name, Length, LastWriteTime | Format-Table -AutoSize
    Get-Content $SumFile
}
finally {
    Pop-Location
}
