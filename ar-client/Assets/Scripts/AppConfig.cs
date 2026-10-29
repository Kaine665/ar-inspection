using UnityEngine;

// 全局运行期配置的唯一出处。
//
// 为什么要有这个文件：后端地址、超时、轮询间隔以前散在 Api.cs / ARHUD.cs 里，
// 改一次地址要翻三个文件、还容易漏。现在统一在这里，其它脚本只读不算。
//
// 环境怎么区分（重点）：
//   DEV  = 开发/演示用。手机走 adb reverse 连电脑的 127.0.0.1:8080，明文 HTTP。
//   PROD = 正式包用。连真实域名（HTTPS），地址不写死在代码里，靠 PlayerPrefs 下发。
//   切换方式：打开 Build Config 场景 → Build Environment 下拉；或菜单 AR/2、AR/3。
//   实现上用编译期宏 AR_PROD —— 生产包里连"dev 地址"那几个字符串都不会被打进去，
//   不存在"发出去的包里还带着 localhost"这种事故。

public enum AppEnv
{
    Dev,
    Prod,
}

public static class AppConfig
{
    // ── 环境判定（编译期，切换靠宏 AR_PROD）────────────────────────────
#if AR_PROD
    public const AppEnv Env = AppEnv.Prod;
#else
    public const AppEnv Env = AppEnv.Dev;
#endif

    public static bool IsProd => Env == AppEnv.Prod;

    /// <summary>给界面显示用：DEV / PROD。演示时一眼看出连的是哪个环境，避免"以为在测生产"。</summary>
    public static string EnvLabel => IsProd ? "PROD" : "DEV";

    // ── 后端地址 ────────────────────────────────────────────────────
    // DEV：手机上的 127.0.0.1 是手机自己，靠 `adb reverse tcp:8080 tcp:8080` 转到电脑。
    const string DevBaseUrl = "http://127.0.0.1:8080";

    // PROD：部署后的真实地址。换域名只改这一行（或打包时用下面的 PlayerPrefs 下发）。
    const string ProdBaseUrl = "https://inspection.example.com";

    // 运行期覆盖（只对 DEV 生效）：真机改用局域网直连时，不必重新打包。
    //   AppConfig.SetDevBaseUrl("http://192.168.1.20:8080")
    public const string BaseUrlPrefKey = "api_base";

    public static string BaseUrl
    {
        get
        {
            if (!IsProd)
            {
                var overridden = PlayerPrefs.GetString(BaseUrlPrefKey, "");
                if (!string.IsNullOrEmpty(overridden)) return overridden.TrimEnd('/');
                return DevBaseUrl;
            }
            // 生产包也允许由安装侧下发（例如灰度环境），没下发就用编译进去的地址
            var prodOverride = PlayerPrefs.GetString(BaseUrlPrefKey, "");
            return string.IsNullOrEmpty(prodOverride) ? ProdBaseUrl : prodOverride.TrimEnd('/');
        }
    }

    /// <summary>真机换局域网地址而不重打包。传空串 = 恢复默认（adb reverse）。仅 DEV 建议使用。</summary>
    public static void SetDevBaseUrl(string url)
    {
        if (string.IsNullOrEmpty(url)) PlayerPrefs.DeleteKey(BaseUrlPrefKey);
        else PlayerPrefs.SetString(BaseUrlPrefKey, url.Trim());
        PlayerPrefs.Save();
    }

    // ── 超时与节奏 ──────────────────────────────────────────────────
    // 单个请求的上限。健康检查要快（它决定顶栏徽章的颜色），提交可以慢一点（现场网络差）。
    public const int HealthTimeoutSeconds = 4;
    public const int CreateTimeoutSeconds = 10;

    // 后端健康检查的兜底轮询间隔：adb reverse 掉了，不用重启 App 也能在两三秒内看出来。
    public const float HealthPollIntervalSeconds = 20f;

    // 一次性提示默认停留多久（"环境已重新识别，N 个标记已清除"这类）
    public const float NoticeDefaultSeconds = 6f;

    // ── 后端约定（改契约时只改这里）──────────────────────────────────
    public const string HealthPath = "/api/health";
    public const string InspectionsPath = "/api/inspections";

    // 状态 / 优先级枚举：和 docs/api.md 逐字对齐，UI 文案另放，值不变。
    public static readonly string[] Priorities = { "low", "medium", "high" };
    public static readonly string[] PriorityLabels = { "低", "中", "高" };
}
