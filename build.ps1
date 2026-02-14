# 清理旧的构建文件
Remove-Item -Path "*.exe" -ErrorAction SilentlyContinue
Remove-Item -Path "dist" -Recurse -Force -ErrorAction SilentlyContinue

# 创建发布目录
New-Item -ItemType Directory -Force -Path "dist" | Out-Null

# 定义通用函数来打包
function Package-Variant {
    param (
        [string]$VariantName,
        [string]$ExeName,
        [string]$BuildTags,
        [string]$LdFlags
    )

    Write-Host "正在构建: $VariantName ($ExeName)..." -ForegroundColor Cyan
    
    # 编译
    if ($BuildTags) {
        go build -tags $BuildTags -ldflags $LdFlags -o $ExeName
    } else {
        go build -ldflags $LdFlags -o $ExeName
    }

    if (-not (Test-Path $ExeName)) {
        Write-Host "错误: 编译失败 $ExeName" -ForegroundColor Red
        return
    }

    # 准备临时打包目录
    $tempDir = "dist\temp_$VariantName"
    New-Item -ItemType Directory -Force -Path $tempDir | Out-Null

    # 复制文件
    Copy-Item $ExeName -Destination $tempDir
    Copy-Item "config.json" -Destination $tempDir
    Copy-Item "README.md" -Destination $tempDir
    Copy-Item -Path "Sounds" -Destination $tempDir -Recurse

    # 压缩
    $zipName = "dist\${VariantName}.zip"
    Compress-Archive -Path "$tempDir\*" -DestinationPath $zipName -Force

    # 清理临时目录和exe
    Remove-Item -Path $tempDir -Recurse -Force
    Remove-Item -Path $ExeName

    Write-Host "打包完成: $zipName" -ForegroundColor Green
}

# 1. 纯净窗口版 (GUI Only)
Package-Variant -VariantName "time_clock_gui_only" `
                -ExeName "time_clock_gui_only.exe" `
                -BuildTags "gui" `
                -LdFlags "-s -w -H=windowsgui"

# 2. 窗口+Web版 (GUI + Web)
Package-Variant -VariantName "time_clock_gui_web" `
                -ExeName "time_clock_gui_web.exe" `
                -BuildTags "gui,web" `
                -LdFlags "-s -w -H=windowsgui"

# 3. 隐形 Web 版 (Headless Web)
Package-Variant -VariantName "time_clock_web_headless" `
                -ExeName "time_clock_web_headless.exe" `
                -BuildTags "web" `
                -LdFlags "-s -w -H=windowsgui"

# 4. 终端版 (Terminal Only)
Package-Variant -VariantName "time_clock_term_only" `
                -ExeName "time_clock_term_only.exe" `
                -BuildTags "" `
                -LdFlags "-s -w"

# 5. 终端+Web版 (Terminal + Web)
Package-Variant -VariantName "time_clock_term_web" `
                -ExeName "time_clock_term_web.exe" `
                -BuildTags "web" `
                -LdFlags "-s -w"

Write-Host "`n所有版本构建并打包完成！请查看 dist 目录。" -ForegroundColor Yellow
