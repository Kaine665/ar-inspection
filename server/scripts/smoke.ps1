<#
.SYNOPSIS
    契约冒烟检查：一条命令验证四个端点 + 四个错误码是否和 docs 里冻结的一致。

.DESCRIPTION
    为什么要用 curl.exe 而不是 Invoke-RestMethod：
    PowerShell 5.1 的 Invoke-RestMethod 传 string body 时会按 ASCII 编码，
    中文标题会变成 ?，测出来的东西不是真实客户端的行为。
    curl.exe 配合「UTF-8 无 BOM 的临时文件」才是手机端真正发的字节。

.EXAMPLE
    powershell -NoProfile -ExecutionPolicy Bypass -File scripts/smoke.ps1
    powershell -NoProfile -ExecutionPolicy Bypass -File scripts/smoke.ps1 -Base http://127.0.0.1:8099
#>
param(
    [string]$Base = "http://127.0.0.1:8080"
)

$ErrorActionPreference = "Continue"
$work = Join-Path $env:TEMP "ar-smoke"
New-Item -ItemType Directory -Path $work -Force | Out-Null
$utf8NoBom = New-Object System.Text.UTF8Encoding($false)

$script:pass = 0
$script:fail = 0

function Write-Body([string]$name, [string]$json) {
    $path = Join-Path $work "$name.json"
    [System.IO.File]::WriteAllText($path, $json, $utf8NoBom)
    return $path
}

# Invoke-Api 发一次请求，返回 @{ Status = 200; Json = <对象或$null>; Raw = "..." }
function Invoke-Api {
    param([string]$Method, [string]$Path, [string]$BodyFile)
    $out = Join-Path $work "resp.txt"
    $argv = @("-s", "-o", $out, "-w", "%{http_code}", "-X", $Method, "$Base$Path")
    if ($BodyFile) {
        $argv += @("-H", "Content-Type: application/json", "--data-binary", "@$BodyFile")
    }
    $code = & curl.exe @argv
    $raw = ""
    if (Test-Path $out) { $raw = [System.IO.File]::ReadAllText($out, $utf8NoBom) }
    $json = $null
    if ($raw.Trim().Length -gt 0) {
        try { $json = $raw | ConvertFrom-Json } catch { $json = $null }
    }
    return @{ Status = [int]$code; Json = $json; Raw = $raw }
}

function Check($name, $ok, $detail) {
    if ($ok -eq $true) {
        $script:pass++
        Write-Host ("  PASS  " + $name) -ForegroundColor Green
    } else {
        $script:fail++
        Write-Host ("  FAIL  " + $name + "  →  " + $detail) -ForegroundColor Red
    }
}

Write-Host "`n冒烟目标: $Base`n" -ForegroundColor Cyan

# ---------------------------------------------------------------- 1 健康
Write-Host "1. GET /api/health"
$r = Invoke-Api GET "/api/health"
Check "返回 200" ($r.Status -eq 200) "实际 $($r.Status)"
Check "status=ok" ($r.Json.status -eq "ok") ($r.Raw)
Check "db=ok" ($r.Json.db -eq "ok") ($r.Raw)
Check "带 version" ([bool]$r.Json.version) ($r.Raw)

# ---------------------------------------------------------------- 2 新建
Write-Host "`n2. POST /api/inspections（中文 + 负 y 坐标）"
$body = Write-Body "create" '{"title":"B1 入口墙面破损","description":"左侧墙体存在裂缝，长约 40cm","priority":"high","position":{"x":0.4212,"y":-1.3001,"z":1.26}}'
$r = Invoke-Api POST "/api/inspections" $body
Check "返回 201" ($r.Status -eq 201) "实际 $($r.Status)：$($r.Raw)"
Check "id 前缀 insp_" ($r.Json.id -like "insp_*") ($r.Raw)
Check "id 长度 31（insp_ + 26 位 ULID）" ($r.Json.id.Length -eq 31) "实际 $($r.Json.id)"
Check "初始状态 open" ($r.Json.status -eq "open") ($r.Raw)
Check "中文标题未被破坏" ($r.Json.title -eq "B1 入口墙面破损") ($r.Raw)
Check "坐标收敛到 3 位小数" ($r.Json.position.x -eq 0.421 -and $r.Json.position.y -eq -1.3) ($r.Raw)
Check "createdAt = updatedAt" ($r.Json.createdAt -eq $r.Json.updatedAt) ($r.Raw)
$newId = $r.Json.id

# ---------------------------------------------------------------- 3 列表
Write-Host "`n3. GET /api/inspections"
$r = Invoke-Api GET "/api/inspections"
Check "返回 200" ($r.Status -eq 200) "实际 $($r.Status)"
Check "包在 items 里" ($null -ne $r.Json.items) ($r.Raw)
Check "包含刚建的那条" (@($r.Json.items | Where-Object { $_.id -eq $newId }).Count -eq 1) ($r.Raw)
$first = @($r.Json.items)[0]
Check "第一条是最新的（createdAt 倒序）" ($first.id -eq $newId) "第一条是 $($first.id)"

# ---------------------------------------------------------------- 4 改状态
Write-Host "`n4. PATCH 改状态：open → in_progress"
Start-Sleep -Milliseconds 1100   # 让 updatedAt 与 createdAt 有机会不同
$body = Write-Body "patch_progress" '{"status":"in_progress"}'
$r = Invoke-Api PATCH "/api/inspections/$newId" $body
Check "返回 200" ($r.Status -eq 200) "实际 $($r.Status)：$($r.Raw)"
Check "状态已变" ($r.Json.status -eq "in_progress") ($r.Raw)
Check "updatedAt 晚于 createdAt" ($r.Json.updatedAt -gt $r.Json.createdAt) "updatedAt=$($r.Json.updatedAt) createdAt=$($r.Json.createdAt)"
$afterFirstPatch = $r.Json.updatedAt

# ---------------------------------------------------------------- 5 幂等
Write-Host "`n5. PATCH 同一状态再改一次（幂等，冻结点 1）"
$r = Invoke-Api PATCH "/api/inspections/$newId" $body
Check "返回 200（不是 409）" ($r.Status -eq 200) "实际 $($r.Status)：$($r.Raw)"
Check "updatedAt 没被刷新" ($r.Json.updatedAt -eq $afterFirstPatch) "开始 $afterFirstPatch 现在 $($r.Json.updatedAt)"

# ---------------------------------------------------------------- 6 只许改 status
Write-Host "`n6. PATCH 传 status 以外的字段（冻结点 5 → 400）"
$body = Write-Body "patch_title" '{"title":"偷偷改标题"}'
$r = Invoke-Api PATCH "/api/inspections/$newId" $body
Check "返回 400" ($r.Status -eq 400) "实际 $($r.Status)：$($r.Raw)"
Check "code=invalid_argument" ($r.Json.error.code -eq "invalid_argument") ($r.Raw)

# ---------------------------------------------------------------- 7 不存在的 id
Write-Host "`n7. PATCH 不存在的 id → 404"
$body = Write-Body "patch_progress2" '{"status":"resolved"}'
$r = Invoke-Api PATCH "/api/inspections/insp_doesnotexist0000000000" $body
Check "返回 404" ($r.Status -eq 404) "实际 $($r.Status)：$($r.Raw)"
Check "code=not_found" ($r.Json.error.code -eq "not_found") ($r.Raw)

# ---------------------------------------------------------------- 8 非法流转
Write-Host "`n8. 状态机：resolved → in_progress 应 409；resolved → open 应 200（冻结点 2）"
$body = Write-Body "patch_resolved" '{"status":"resolved"}'
$r = Invoke-Api PATCH "/api/inspections/$newId" $body
Check "open/in_progress → resolved 允许（含跳级）" ($r.Status -eq 200 -and $r.Json.status -eq "resolved") ($r.Raw)
$r = Invoke-Api PATCH "/api/inspections/$newId" $body
$body = Write-Body "patch_back_progress" '{"status":"in_progress"}'
$r = Invoke-Api PATCH "/api/inspections/$newId" $body
Check "resolved → in_progress 返回 409" ($r.Status -eq 409) "实际 $($r.Status)：$($r.Raw)"
Check "code=invalid_transition" ($r.Json.error.code -eq "invalid_transition") ($r.Raw)
$body = Write-Body "patch_reopen" '{"status":"open"}'
$r = Invoke-Api PATCH "/api/inspections/$newId" $body
Check "resolved → open（复发重开）返回 200" ($r.Status -eq 200 -and $r.Json.status -eq "open") ($r.Raw)

# ---------------------------------------------------------------- 9 校验分支
Write-Host "`n9. POST 的四条校验（冻结点 3：只报第一个错）"
$r = Invoke-Api POST "/api/inspections" (Write-Body "bad_title" '{"title":"   ","priority":"high","position":{"x":0,"y":-1,"z":0}}')
Check "标题全空格 → 400 标题不能为空" ($r.Status -eq 400 -and $r.Json.error.message -eq "标题不能为空") ($r.Raw)

$r = Invoke-Api POST "/api/inspections" (Write-Body "bad_prio" '{"title":"x","priority":"urgent","position":{"x":0,"y":-1,"z":0}}')
Check "优先级 urgent → 400" ($r.Status -eq 400 -and $r.Json.error.message -like "优先级必须*") ($r.Raw)

$r = Invoke-Api POST "/api/inspections" (Write-Body "bad_pos" '{"title":"x","priority":"high"}')
Check "缺坐标 → 400 缺少位置坐标" ($r.Status -eq 400 -and $r.Json.error.message -eq "缺少位置坐标") ($r.Raw)

$r = Invoke-Api POST "/api/inspections" (Write-Body "bad_nan" '{"title":"x","priority":"high","position":{"x":"NaN","y":-1,"z":0}}')
Check "x 是字符串 NaN → 400 坐标不是有效数字" ($r.Status -eq 400 -and $r.Json.error.message -eq "坐标不是有效数字") ($r.Raw)

# ---------------------------------------------------------------- 10 兜底
Write-Host "`n10. 未知路径也要走契约错误体"
$r = Invoke-Api GET "/api/nope"
Check "返回 404" ($r.Status -eq 404) "实际 $($r.Status)：$($r.Raw)"
Check "错误体形状正确" ($r.Json.error.code -eq "not_found") ($r.Raw)

# ---------------------------------------------------------------- 汇总
$total = $script:pass + $script:fail
Write-Host "`n----------------------------------------" -ForegroundColor Cyan
if ($script:fail -eq 0) {
    Write-Host "全部通过：$($script:pass)/$total" -ForegroundColor Green
    exit 0
} else {
    Write-Host "失败 $($script:fail) 项，通过 $($script:pass)/$total" -ForegroundColor Red
    exit 1
}
