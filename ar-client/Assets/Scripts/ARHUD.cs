using System.Text;
using UnityEngine;
using UnityEngine.XR.ARFoundation;
using UnityEngine.XR.ARSubsystems;

// 屏幕上的诊断面板：会话状态、平面数量、已放置数量，并逐个列出平面的姿态与面积。
// 目的：看到"墙上有块平面"这类疑问时不用猜，直接看数字。
public class ARHUD : MonoBehaviour
{
    ARPlaneManager m_PlaneManager;
    ARPlacement m_Placement;
    GUIStyle m_Style;
    readonly StringBuilder m_Sb = new StringBuilder();

    void Awake()
    {
        m_PlaneManager = FindObjectOfType<ARPlaneManager>();
        m_Placement = FindObjectOfType<ARPlacement>();
    }

    void OnGUI()
    {
        if (m_Style == null)
        {
            m_Style = new GUIStyle(GUI.skin.label)
            {
                fontSize = Mathf.RoundToInt(Screen.height * 0.024f),
                normal = { textColor = Color.white }
            };
        }

        int planes = m_PlaneManager != null ? m_PlaneManager.trackables.count : -1;
        float line = Screen.height * 0.07f;
        float y = Screen.height * 0.04f;

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

        GUI.Label(new Rect(Screen.width * 0.03f, y, Screen.width * 0.94f, line * 2f), m_Sb.ToString(), m_Style);
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
