using System.Collections.Generic;
using UnityEngine;
using UnityEngine.EventSystems;
using UnityEngine.XR.ARFoundation;
using UnityEngine.XR.ARSubsystems;

// L2：点一下，在命中的平面上放一个方块，并挂到锚点上。
// 关键三点：
//   1) 用 ARRaycastManager 打射线，不是 Physics.Raycast（AR 平面没有碰撞体）
//   2) 命中后把方块挂到 ARAnchor 下，否则跟踪微调时方块会相对地面滑动
//   3) 点在 UI 上时不要放方块
public class ARPlacement : MonoBehaviour
{
    [Header("由 ProjectSetup 自动连线")]
    public GameObject cubePrefab;
    public ARRaycastManager raycastManager;
    public ARAnchorManager anchorManager;
    public Camera arCamera;
    public SubmitForm form;   // 放好之后交给它填表

    static readonly List<ARRaycastHit> s_Hits = new List<ARRaycastHit>();
    readonly List<GameObject> m_Placed = new List<GameObject>();

    public int placedCount => m_Placed.Count;
    public int lastHitKind { get; private set; }   // 0=没命中 1=命中平面

    void Update()
    {
        // 表单开着的时候，点击属于表单，不属于 AR（题目 3.1 第 6 条）
        if (form != null && form.IsOpen)
            return;

        if (!TryGetTapPosition(out var screenPos))
            return;

        // 手指落在 HUD 的「重连后端」按钮上时不当作"放置"（题目 3.1 第 6 条）
        if (ARHUD.IsOverHudUI(screenPos))
            return;

        // 手指落在 UI 上时不当作"放置"（题目 3.1 第 6 条）
        if (EventSystem.current != null && EventSystem.current.IsPointerOverGameObject())
            return;

        // 只认平面。加 PlaneEstimated 会在平面还没成形时也命中，位置不稳。
        if (raycastManager.Raycast(screenPos, s_Hits, TrackableType.PlaneWithinPolygon))
        {
            lastHitKind = 1;
            Place(s_Hits[0]);
        }
        else
        {
            lastHitKind = 0;
            Debug.Log("[placement] 没有命中平面，忽略这次点击");
        }
    }

    void Place(ARRaycastHit hit)
    {
        var plane = hit.trackable as ARPlane;

        // 锚点把方块和"环境里被跟踪的那块面"绑在一起
        Transform parent = null;
        if (plane != null && anchorManager != null)
        {
            var anchor = anchorManager.AttachAnchor(plane, hit.pose);
            if (anchor != null) parent = anchor.transform;
        }

        var cube = Instantiate(cubePrefab, hit.pose.position, hit.pose.rotation, parent);
        cube.name = $"Marker_{m_Placed.Count + 1}";
        // 方块比地面高半个身位，避免和平面重叠导致闪烁
        cube.transform.position += Vector3.up * 0.05f;
        m_Placed.Add(cube);

        Debug.Log($"[placement] #{m_Placed.Count} at {hit.pose.position} anchor={(parent != null)}");

        if (form != null) form.Open(cube.transform.position, cube);
    }

    bool TryGetTapPosition(out Vector2 screenPos)
    {
        if (Input.touchCount > 0)
        {
            var t = Input.GetTouch(0);
            if (t.phase == TouchPhase.Began) { screenPos = t.position; return true; }
        }
        else if (Input.GetMouseButtonDown(0))
        {
            screenPos = Input.mousePosition; return true;
        }
        screenPos = default;
        return false;
    }

    // 表单被取消时调用：这个方块是"还没上报的预览"，取消就连锚点一起删掉，
    // 不留一个永远不会变成记录的孤儿方块。
    public void DiscardMarker(GameObject cube)
    {
        if (cube == null) return;

        var parent = cube.transform.parent;
        var anchor = parent != null ? parent.GetComponent<ARAnchor>() : null;

        m_Placed.Remove(cube);
        Destroy(cube);
        if (anchor != null) Destroy(anchor.gameObject);

        Debug.Log($"[placement] 取消上报，丢弃标记；剩余 {m_Placed.Count}");
    }

    public void ClearAll()
    {
        foreach (var go in m_Placed) if (go != null) Destroy(go);
        m_Placed.Clear();
    }
}
