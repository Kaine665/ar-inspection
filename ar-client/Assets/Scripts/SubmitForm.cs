using System.Collections;
using UnityEngine;

// L3 的录入面：放好方块后弹出表单，标题必填、描述可选、优先级三档，提交到 Go 后端。
// 用 IMGUI 而不是 uGUI，原因很实在：Unity 内置字体在 Android 上能正确显示中文（已实测），
// 而 uGUI 的 Text 要额外处理字体回退。界面丑，但先保证"能填、能提交、有回馈"。
public class SubmitForm : MonoBehaviour
{
    public ARPlacement placement;

    bool m_Open;
    Vector3 m_Position;
    GameObject m_Cube;

    string m_Title = "";
    string m_Desc  = "";
    int    m_Priority = 1;                  // 0=low 1=medium 2=high，默认中
    static readonly string[] k_PriorityNames = { "low", "medium", "high" };
    static readonly string[] k_PriorityLabels = { "低", "中", "高" };

    string m_Message = "";
    bool   m_IsError;
    bool   m_Busy;

    public bool IsOpen => m_Open;

    GUIStyle m_Field, m_Label, m_Button, m_Title2;

    void Awake()
    {
        if (placement == null) placement = FindObjectOfType<ARPlacement>();
    }

    public void Open(Vector3 worldPosition, GameObject cube)
    {
        m_Position = worldPosition;
        m_Cube = cube;
        m_Open = true;
        m_Busy = false;
        m_Message = "";
        m_IsError = false;
        m_Title = "";
        m_Desc  = "";
        m_Priority = 1;
    }

    void Close()
    {
        m_Open = false;
        m_Busy = false;
    }

    // 取消 = 这次上报作废。方块本来就和表单是一体的：它是这条记录的预览，
    // 表单关了它就不该留下（否则屏幕上会堆着永远变不成记录的孤儿方块）。
    void Cancel()
    {
        if (m_Cube != null && placement != null) placement.DiscardMarker(m_Cube);
        m_Cube = null;
        Debug.Log("[submit] 用户取消，丢弃这个标记");
        Close();
    }

    // 提交之后立刻刷新顶栏的后端徽章：成功说明连通，失败多半是 adb reverse 掉了
    void RefreshBackend()
    {
        if (ARHUD.Instance != null) ARHUD.Instance.CheckNow();
    }

    void Submit()
    {
        var title = (m_Title ?? "").Trim();
        if (title.Length == 0) { m_IsError = true; m_Message = "标题不能为空"; return; }
        if (title.Length > 100) { m_IsError = true; m_Message = "标题最多 100 字"; return; }
        if ((m_Desc ?? "").Length > 500) { m_IsError = true; m_Message = "描述最多 500 字"; return; }

        var body = new InspectionCreateDto
        {
            title = title,
            description = m_Desc ?? "",
            priority = k_PriorityNames[m_Priority],
            position = new PositionDto { x = m_Position.x, y = m_Position.y, z = m_Position.z }
        };

        m_Busy = true;
        m_IsError = false;
        m_Message = "提交中…";
        StartCoroutine(Api.CreateInspection(body, OnCreated, OnFailed));
    }

    void OnCreated(InspectionDto dto)
    {
        m_Busy = false;
        m_IsError = false;
        m_Message = dto != null ? $"已上报 {dto.id}" : "已上报（响应未解析）";
        Debug.Log("[submit] 成功: " + m_Message);

        // 方块变绿 = 这条已经落到后端了（视觉上的"已提交"）
        if (m_Cube != null)
        {
            var r = m_Cube.GetComponent<Renderer>();
            if (r != null) r.material.color = new Color(0.20f, 0.85f, 0.35f);
        }

        // 给 1.2 秒看提示，然后关表单，允许放下一个
        StartCoroutine(CloseAfter(1.2f));
        RefreshBackend();
    }

    void OnFailed(string code, string message)
    {
        m_Busy = false;
        m_IsError = true;
        m_Message = code == "network" ? $"连不上后端（{message}）" : message;
        Debug.LogWarning($"[submit] 失败 code={code} msg={message}");
        // 失败不关表单、不清内容 —— 现场网络差是常态（BR-09）
        RefreshBackend();
    }

    IEnumerator CloseAfter(float seconds)
    {
        yield return new WaitForSeconds(seconds);
        Close();
    }

    void EnsureStyles()
    {
        if (m_Field != null) return;
        int baseSize = Mathf.RoundToInt(Screen.height * 0.026f);
        m_Label  = new GUIStyle(GUI.skin.label)  { fontSize = baseSize, normal = { textColor = Color.white } };
        m_Field  = new GUIStyle(GUI.skin.textField) { fontSize = baseSize };
        m_Button = new GUIStyle(GUI.skin.button) { fontSize = baseSize };
        m_Title2 = new GUIStyle(GUI.skin.label)  { fontSize = Mathf.RoundToInt(baseSize * 1.25f), fontStyle = FontStyle.Bold, normal = { textColor = Color.white } };
    }

    void OnGUI()
    {
        if (!m_Open) return;
        EnsureStyles();

        float w = Mathf.Min(Screen.width * 0.86f, Screen.height * 1.2f);
        float h = Screen.height * 0.62f;
        var rect = new Rect((Screen.width - w) * 0.5f, Screen.height - h - Screen.height * 0.04f, w, h);

        GUI.color = new Color(0f, 0f, 0f, 0.78f);
        GUI.DrawTexture(rect, Texture2D.whiteTexture);
        GUI.color = Color.white;

        GUILayout.BeginArea(new Rect(rect.x + 24, rect.y + 20, rect.width - 48, rect.height - 40));
        GUILayout.Label("上报问题", m_Title2);

        GUILayout.Label($"位置 x={m_Position.x:0.00} y={m_Position.y:0.00} z={m_Position.z:0.00} m", m_Label);
        GUILayout.Space(6);

        GUILayout.Label("标题（必填）", m_Label);
        m_Title = GUILayout.TextField(m_Title ?? "", 100, m_Field);

        GUILayout.Space(6);
        GUILayout.Label("描述（可选）", m_Label);
        m_Desc = GUILayout.TextField(m_Desc ?? "", 500, m_Field);

        GUILayout.Space(8);
        GUILayout.Label("优先级", m_Label);
        GUILayout.BeginHorizontal();
        for (int i = 0; i < k_PriorityLabels.Length; i++)
        {
            bool on = m_Priority == i;
            GUI.backgroundColor = on ? new Color(0.95f, 0.55f, 0.15f) : Color.white;
            if (GUILayout.Button(k_PriorityLabels[i], m_Button, GUILayout.Height(48))) m_Priority = i;
            GUI.backgroundColor = Color.white;
        }
        GUILayout.EndHorizontal();

        GUILayout.Space(10);
        if (!string.IsNullOrEmpty(m_Message))
        {
            var old = GUI.color;
            GUI.color = m_IsError ? new Color(1f, 0.55f, 0.5f) : new Color(0.6f, 1f, 0.7f);
            GUILayout.Label(m_Message, m_Label);
            GUI.color = old;
        }

        GUILayout.FlexibleSpace();
        GUILayout.BeginHorizontal();
        GUI.enabled = !m_Busy;
        if (GUILayout.Button("提交", m_Button, GUILayout.Height(56))) Submit();
        if (GUILayout.Button("取消", m_Button, GUILayout.Height(56))) Cancel();
        GUI.enabled = true;
        GUILayout.EndHorizontal();

        GUILayout.EndArea();
    }
}
