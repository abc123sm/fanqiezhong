$ErrorActionPreference = "Stop"

# 确保发布目录存在，且仅清理 dist 下的旧 zip 压缩包（绝不触碰根目录下的 exe 或 dist 下的用户解压目录）
New-Item -ItemType Directory -Force -Path "dist" | Out-Null
Remove-Item -Path "dist\*.zip" -Force -ErrorAction SilentlyContinue

# 复制公共运行与文档资源到打包暂存目录
function Copy-CommonAssets {
    param (
        [string]$TargetDir
    )
    Copy-Item -LiteralPath "config.json" -Destination $TargetDir
    Copy-Item -LiteralPath "README.md" -Destination $TargetDir
    Copy-Item -LiteralPath "docs" -Destination $TargetDir -Recurse
    Copy-Item -LiteralPath "Sounds" -Destination $TargetDir -Recurse
}

# 5 种运行模式的构建配置清单
$variants = @(
    @{
        VariantName = "time_clock_gui_only"
        ExeName     = "time_clock_gui_only.exe"
        BuildTags   = "gui"
        LdFlags     = "-s -w -H=windowsgui"
    },
    @{
        VariantName = "time_clock_gui_web"
        ExeName     = "time_clock_gui_web.exe"
        BuildTags   = "gui,web"
        LdFlags     = "-s -w -H=windowsgui"
    },
    @{
        VariantName = "time_clock_web_headless"
        ExeName     = "time_clock_web_headless.exe"
        BuildTags   = "web"
        LdFlags     = "-s -w -H=windowsgui"
    },
    @{
        VariantName = "time_clock_term_only"
        ExeName     = "time_clock_term_only.exe"
        BuildTags   = ""
        LdFlags     = "-s -w"
    },
    @{
        VariantName = "time_clock_term_web"
        ExeName     = "time_clock_term_web.exe"
        BuildTags   = "web"
        LdFlags     = "-s -w"
    }
)

# 在系统临时目录创建独立构建沙箱，避免污染或误删工作区根目录中的自用可执行文件
$buildSandbox = Join-Path ([System.IO.Path]::GetTempPath()) "time_clock_build_$PID"
$binDir = Join-Path $buildSandbox "bin"
New-Item -ItemType Directory -Force -Path $binDir | Out-Null

try {
    foreach ($v in $variants) {
        $variantName = $v.VariantName
        $exeName = $v.ExeName
        $exePath = Join-Path $binDir $exeName

        Write-Host "正在构建: $variantName ($exeName)..." -ForegroundColor Cyan

        if ($v.BuildTags) {
            go build -tags $v.BuildTags -ldflags $v.LdFlags -o $exePath
        } else {
            go build -ldflags $v.LdFlags -o $exePath
        }

        if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $exePath)) {
            throw "错误: 编译失败 $exeName"
        }

        # 组装并压缩当前单模式发布包
        $stageDir = Join-Path $buildSandbox "stage_$variantName"
        New-Item -ItemType Directory -Force -Path $stageDir | Out-Null

        Copy-Item -LiteralPath $exePath -Destination $stageDir
        Copy-CommonAssets -TargetDir $stageDir

        $zipPath = Join-Path "dist" "${variantName}.zip"
        Compress-Archive -Path "$stageDir\*" -DestinationPath $zipPath -Force
        Write-Host "打包完成: $zipPath" -ForegroundColor Green
    }

    # 组装并压缩包含全部 5 种模式可执行文件的全量合集包
    $allVariantName = "time_clock_all"
    Write-Host "正在汇总打包全模式合集: ${allVariantName}.zip ..." -ForegroundColor Cyan

    $allStageDir = Join-Path $buildSandbox "stage_$allVariantName"
    New-Item -ItemType Directory -Force -Path $allStageDir | Out-Null

    Copy-Item -Path "$binDir\*.exe" -Destination $allStageDir
    Copy-CommonAssets -TargetDir $allStageDir

    $allZipPath = Join-Path "dist" "${allVariantName}.zip"
    Compress-Archive -Path "$allStageDir\*" -DestinationPath $allZipPath -Force
    Write-Host "打包完成: $allZipPath" -ForegroundColor Green

    Write-Host "`n全部 6 个压缩包构建完成（5 个单模式包 + 1 个全模式合集包），请查看 dist 目录。" -ForegroundColor Yellow
}
finally {
    if (Test-Path -LiteralPath $buildSandbox) {
        Remove-Item -LiteralPath $buildSandbox -Recurse -Force -ErrorAction SilentlyContinue
    }
}
