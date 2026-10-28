using System.Collections;
using System.Text;
using UnityEngine;
using UnityEngine.XR.ARFoundation;
using UnityEngine.XR.ARSubsystems;

// 屏幕上的诊断面板：后端连通性、会话状态、平面数量、已放置数量，并逐个列出平面的姿态与面积。
// 目的：看到"墙上有块平面""提交失败"这类疑问时不用猜，直接看数字。
public class ARHUD : MonoBehaviour
{
    public static ARHUD Instance { get; private set; }

    ARPlaneManager m_PlaneManager;
    ARPlacement m_Placement;
    GUIStyle m_Style;
    GUIStyle m_StatusStyle;
    GUIStyle m_ButtonStyle;
    readonly StringBuilder m_Sb = new StringBuilder();

    // 后端状态（由 Api.GetHealth 刷新）
    BackendPhase m_Phase = BackendPhase.Unknown;
    string m_PhaseText = "";
    string m_Version = "";
    string m_CheckedAt = "";
    bool m_Checking;

    // 「重连」按钮的屏幕矩形，用来挡掉"点按钮顺手放了个方块"
    Rect m_ReconnectRect = new Rect(-100, -100, 0, 0);

    public BackendPhase phase => m_Phase;

    void Awake()
    {
        Instance = this;
        m_PlaneManager = FindObjectOfType<ARPlaneManager>();
        m_Placement = FindObjectOfType<ARPlacement>();
    }

    void Start()
    {
        CheckNow();
        // 每隔 20 秒兜底重查一次：手机上 adb reverse 掉了，不用重启 App 也能看出来
        InvokeRepeating(nameof(CheckNow), 20f, 20f);
    }

    void OnDestroy()
    {
        if (Instance == this) Instance = null;
    }

    // 供 SubmitForm 在提交后调用：成功/失败都立刻把徽章刷新成真实状态
    public void CheckNow()
    {
        if (m_Checking) return;
        StartCoroutine(CoCheck());
    }

    IEnumerator CoCheck()
    {
        m_Checking = true;
        m_Phase = BackendPhase.Checking;
        yield return Api.GetHealth(OnHealthOk, OnHealthErr);
        m_Checking = false;
        m_CheckedAt = System.DateTime.Now.ToString("HH:mm:ss");
    }

    void OnHealthOk(HealthDto dto)
    {
        if (dto == null)
        {
            m_Phase = BackendPhase.DbDown;
            m_PhaseText = "响应异常";
            return;
        }
        m_Version = dto.version;
        bool dbOk = dto.db == "ok";
        m_Phase = dbOk ? BackendPhase.Ok : BackendPhase.DbDown;
        m_PhaseText = dbOk ? "" : "库异常";
    }

    void OnHealthErr(string err)
    {
        m_Phase = BackendPhase.Down;
        m_PhaseText = "连不上";
        Debug.LogWarning("[hud] 后端健康检查失败: " + err);
    }

    // 点在「重连」按钮上时，ARPlacement 不要把它当成放置点击
    public static bool IsOverHudUI(Vector2 screenPos)
    {
        if (Instance == null) return false;
        // GUI 的 y 从上往下，触摸坐标的 y 从下往上
        var guiPos = new Vector2(screenPos.x, Screen.height - screenPos.y);
        return Instance.m_ReconnectRect.Contains(guiPos);
    }

    void OnGUI()
    {
        if (m_Style == null)
        {
            int fs = Mathf.RoundToInt(Screen.height * 0.024f);
            m_Style = new GUIStyle(GUI.skin.label)
            {
                fontSize = fs,
                normal = { textColor = Color.white }
            };
            m_StatusStyle = new GUIStyle(m_Style) { fontStyle = FontStyle.Bold, wordWrap = false, clipping = TextClipping.Overflow };
            m_ButtonStyle = new GUIStyle(GUI.skin.button) { fontSize = Mathf.RoundToInt(Screen.height * 0.022f) };
        }

        float line = Screen.height * 0.07f;
        float y = Screen.height * 0.04f;

        // ① 后端状态：一行说完"连没连上、库通不通、什么时候查的"
        string status = StatusLine();
        var old = GUI.color;
        GUI.color = StatusColor();
        GUI.Label(new Rect(Screen.width * 0.03f, y, Screen.width * 0.72f, line), status, m_StatusStyle);
        GUI.color = old;

        // ② 重连按钮（右上角）。点了只做一次健康检查，不改任何业务数据
        var btn = new Rect(Screen.width * 0.76f, y, Screen.width * 0.21f, Screen.height * 0.055f);
        m_ReconnectRect = btn;
        if (GUI.Button(btn, "重连后端", m_ButtonStyle))
            CheckNow();

        // ③ AR 自身状态
        int planes = m_PlaneManager != null ? m_PlaneManager.trackables.count : -1;
        m_Sb.Length = 0;
        m_Sb.Append("Session: ").Append(ARSession.state).Append('\n');
        m_Sb.Append("Planes: ").Append(planes);
        if (m_Placement != null)
            m_Sb.Append("  已放置: ").Append(m_Placement.placedCount);

        if (m_PlaneManager != null && planes > 0)
        {
            int shown = 0;
            foreach (var p in m_PlaneManager.trackables)
            {
                if (shown >= 4) { m_Sb.Append("\n  …"); break; }
                m_Sb.Append("\n  ").Append(Describe(p));
                shown++;
            }
        }
        else if (planes == 0)
        {
            m_Sb.Append("\n把手机对准地面，慢慢左右移动");
        }

        GUI.Label(new Rect(Screen.width * 0.03f, y + line, Screen.width * 0.94f, line * 2.6f), m_Sb.ToString(), m_Style);
    }

    string StatusLine()
    {
        switch (m_Phase)
        {
            case BackendPhase.Checking:
                return "后端 检查中…";
            case BackendPhase.Ok:
                return $"后端 已连接 v{m_Version} · {m_CheckedAt}";
            case BackendPhase.DbDown:
                return "后端 在线 · 库异常（写入会失败）";
            case BackendPhase.Down:
                return "后端 未连接 · 点「重连后端」重试";
            default:
                return "后端 状态未知";
        }
    }

    Color StatusColor()
    {
        switch (m_Phase)
        {
            case BackendPhase.Ok:      return new Color(0.55f, 1f, 0.65f);
            case BackendPhase.DbDown:  return new Color(1f, 0.80f, 0.35f);
            case BackendPhase.Down:    return new Color(1f, 0.45f, 0.40f);
            case BackendPhase.Checking: return new Color(0.80f, 0.80f, 0.80f);
            default:                    return new Color(0.80f, 0.80f, 0.80f);
        }
    }

    // 一块平面的身份信息：朝向 + 法向 + 尺寸
    static string Describe(ARPlane p)
    {
        string align;
        switch (p.alignment)
        {
            case PlaneAlignment.HorizontalUp:   align = "水平↑"; break;
            case PlaneAlignment.HorizontalDown: align = "水平↓"; break;
            case PlaneAlignment.Vertical:       align = "竖直 "; break;
            default:                            align = "斜   "; break;
        }
        var n = p.transform.up;
        string id = p.trackableId.ToString();
        if (id.Length > 8) id = id.Substring(0, 8);   // 取前 8 位，尾部容易撞车
        return $"#{id} {align} 法向({n.x:0.0},{n.y:0.0},{n.z:0.0}) {p.size.x:0.0}×{p.size.y:0.0}m";
    }
}
