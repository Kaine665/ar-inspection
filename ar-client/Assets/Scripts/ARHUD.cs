using UnityEngine;
using UnityEngine.XR.ARFoundation;

// 屏幕上的一行调试信息：会话状态 + 已检测平面数 + 一句引导。
// L1 的验收就是"看到 Planes 从 0 变成 >0"。
public class ARHUD : MonoBehaviour
{
    ARPlaneManager m_PlaneManager;
    GUIStyle m_Style;

    void Awake()
    {
        m_PlaneManager = FindObjectOfType<ARPlaneManager>();
    }

    void OnGUI()
    {
        if (m_Style == null)
        {
            m_Style = new GUIStyle(GUI.skin.label)
            {
                fontSize = Mathf.RoundToInt(Screen.height * 0.028f),
                normal = { textColor = Color.white }
            };
        }

        int planes = m_PlaneManager != null ? m_PlaneManager.trackables.count : -1;
        var state = ARSession.state;

        float y = Screen.height * 0.05f;
        float line = Screen.height * 0.045f;

        GUI.Label(new Rect(Screen.width * 0.03f, y, Screen.width * 0.94f, line), $"Session: {state}", m_Style);
        GUI.Label(new Rect(Screen.width * 0.03f, y + line, Screen.width * 0.94f, line), $"Planes: {planes}", m_Style);

        if (planes <= 0)
            GUI.Label(new Rect(Screen.width * 0.03f, y + line * 2, Screen.width * 0.94f, line * 2),
                "把手机对准地面，慢慢左右移动", m_Style);
    }
}
