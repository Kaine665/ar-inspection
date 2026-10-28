# 造一批演示数据：全部走真实 HTTP 接口，不直接改库。
#
#   powershell -NoProfile -ExecutionPolicy Bypass -File scripts/seed.ps1
#   powershell ... -File scripts/seed.ps1 -Base http://127.0.0.1:8090
#
# 用途：录演示视频或截图前把库里填成"像真在用的样子"。中文必须用 curl.exe + UTF-8 文件发，
# PowerShell 的 Invoke-RestMethod 传 string body 会按 ASCII 编码，中文变问号。
param(
    [string]$Base = "http://127.0.0.1:8080"
)

$ErrorActionPreference = "Stop"

$records = @(
    @{ title = "B1 入口墙面破损";       description = "左侧墙体现裂缝，长约 40cm，雨天渗水";                 priority = "high";   position = @{ x = 0.42;  y = -1.30; z = 1.26 } },
    @{ title = "3F 强电井配电箱异响";   description = "巡检时听到持续嗡鸣，箱体温度偏高";                   priority = "high";   position = @{ x = 1.84;  y = -1.05; z = 2.31 } },
    @{ title = "2F 东侧消防通道堆物";   description = "转角堆了 6 箱包装纸箱，占用疏散宽度";                 priority = "medium"; position = @{ x = -2.15; y = -1.12; z = 0.86 } },
    @{ title = "B2 车库顶板渗水";       description = "车位 B2-27 上方顶板有水渍，面积约 0.5 平方米";         priority = "medium"; position = @{ x = 0.95;  y = -1.42; z = -1.73 } },
    @{ title = "1F 中庭幕墙玻璃裂纹";   description = "二层幕墙东侧第二块出现放射状裂纹，未脱落";             priority = "high";   position = @{ x = -0.68; y = -0.92; z = 3.05 } },
    @{ title = "4F 茶水间地漏返味";     description = "靠窗地漏有异味，排水明显变慢";                       priority = "low";    position = @{ x = 2.42;  y = -1.08; z = 1.05 } },
    @{ title = "屋顶冷却塔护栏松动";    description = "北侧护栏有 2 处固定螺栓脱出";                         priority = "medium"; position = @{ x = 1.12;  y = -0.74; z = -2.48 } },
    @{ title = "3F 女卫洗手台下方渗漏"; description = "柜内有积水，柜体发胀";                               priority = "low";    position = @{ x = -1.36; y = -1.18; z = 0.34 } }
)

# 造完之后把几条改成处理中 / 已解决，让列表三种状态都有
$afterCreate = @(
    @{ index = 1; status = "in_progress" },
    @{ index = 3; status = "in_progress" },
    @{ index = 6; status = "resolved" }
)

$tmp = Join-Path $env:TEMP ("seed-" + [guid]::NewGuid().ToString("N") + ".json")
$utf8 = New-Object System.Text.UTF8Encoding($false)   # 无 BOM，否则 Go 解析失败

function Send-Json($method, $url, $obj) {
    [System.IO.File]::WriteAllText($tmp, ($obj | ConvertTo-Json -Depth 6 -Compress), $utf8)
    $raw = & curl.exe -s -w "`n%{http_code}" -X $method $url -H "Content-Type: application/json" --data-binary "@$tmp"
    $lines = $raw -split "`n"
    $code = $lines[$lines.Count - 1].Trim()
    $body = ($lines[0..($lines.Count - 2)] -join "`n")
    return @{ code = $code; body = $body }
}

$ids = @()
Write-Host "目标后端: $Base" -ForegroundColor Cyan
foreach ($r in $records) {
    $res = Send-Json "POST" "$Base/api/inspections" $r
    if ($res.code -ne "201") {
        Write-Host ("  失败 {0} → HTTP {1}: {2}" -f $r.title, $res.code, $res.body) -ForegroundColor Red
        continue
    }
    $obj = $res.body | ConvertFrom-Json
    $ids += $obj.id
    Write-Host ("  + {0}  {1}" -f $obj.id, $obj.title)
}

foreach ($u in $afterCreate) {
    if ($u.index -ge $ids.Count) { continue }
    $res = Send-Json "PATCH" "$Base/api/inspections/$($ids[$u.index])" @{ status = $u.status }
    if ($res.code -ne "200") {
        Write-Host ("  改状态失败 {0} → HTTP {1}" -f $ids[$u.index], $res.code) -ForegroundColor Red
        continue
    }
    Write-Host ("  ~ {0} → {1}" -f $ids[$u.index], $u.status)
}

Remove-Item $tmp -Force -ErrorAction SilentlyContinue

$listed = (& curl.exe -s "$Base/api/inspections") | ConvertFrom-Json
$count = if ($listed.items) { $listed.items.Count } else { 0 }
Write-Host ""
Write-Host "现在库里共 $count 条" -ForegroundColor Green
