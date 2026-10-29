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
    public ARPlaneManager planeManager;
    public Camera arCamera;
    public SubmitForm form;   // 放好之后交给它填表

    static readonly List<ARRaycastHit> s_Hits = new List<ARRaycastHit>();
    readonly List<GameObject> m_Placed = new List<GameObject>();

    public int placedCount => m_Placed.Count;
    public int lastHitKind { get; private set; }   // 0=没命中 1=命中平面

    void Awake()
    {
        if (raycastManager == null) raycastManager = FindObjectOfType<ARRaycastManager>();
        if (anchorManager == null) anchorManager = FindObjectOfType<ARAnchorManager>();
        if (planeManager == null) planeManager = FindObjectOfType<ARPlaneManager>();
    }

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

        // 门控第一关：会话本身没在正常跟踪时，点了也不放（详见下面 BlockWithNotice 的注释）
        if (ARSession.notTrackingReason != NotTrackingReason.None)
        {
            BlockWithNotice("请移动手机重新识别环境（跟踪不稳定）", "notTrackingReason=" + ARSession.notTrackingReason);
            return;
        }

        // 只认平面。加 PlaneEstimated 会在平面还没成形时也命中，位置不稳。
        if (!raycastManager.Raycast(screenPos, s_Hits, TrackableType.PlaneWithinPolygon))
        {
            lastHitKind = 0;
            Debug.Log("[placement] 没有命中平面，忽略这次点击");
            return;
        }

        lastHitKind = 1;

        // 门控第二关：命中的平面自己还在漂（刚检测出来、正在被重算）时也不许放。
        // 会话级正常 ≠ 这块平面正常，两者要分开判。
        var hitPlane = ResolvePlane(s_Hits[0]);
        if (hitPlane != null && hitPlane.trackingState != TrackingState.Tracking)
        {
            BlockWithNotice("这块平面还在识别中，请稍等一秒再点", "命中平面 trackingState=" + hitPlane.trackingState);
            return;
        }

        Place(s_Hits[0]);
    }

    // ---------------------------------------------------------------
    // 跟踪门控：跟踪不可靠时，宁可不放，也不放一个坐标错的
    //
    // 为什么必须有这一层：ARCore 位姿靠"特征点 + 惯性"融合，特征点不够时会退化成惯性推算，
    // 位姿在漂。但射线仍然打得到平面（平面就在那儿），方块也仍然出现在手指下——
    // 因为射线用的是同一个错位姿，所以"看起来完美"。等跟踪恢复，方块会跳走或锚点建不上，
    // 而那条记录的坐标已经错了。这是本项目里最典型的"沉默的失败"。
    //
    // 判定口径（写死，否则"不可靠"没法判）：
    //   ARSession.notTrackingReason != None  → 整个会话没在正常跟踪
    //   命中平面的 trackingState != Tracking → 这块平面还在漂
    // ---------------------------------------------------------------

    static float s_LastBlockAt = -10f;

    void BlockWithNotice(string hint, string reason)
    {
        // 2 秒内只提示一次：连续点击时不要用同一句话刷屏
        if (Time.realtimeSinceStartup - s_LastBlockAt < 2f) return;
        s_LastBlockAt = Time.realtimeSinceStartup;

        ARHUD.Notice(hint, 3f);
        Debug.Log("[placement] 已拦截这次点击（跟踪不可靠）：" + reason);
    }

    void Place(ARRaycastHit hit)
    {
        var plane = ResolvePlane(hit);

        // 锚点优先级（实测结论，只写一次的结论，不留草稿）：
        //   1) 平面锚点 AttachAnchor —— 首选。ARCore 对它支持最稳，方块跟着平面走。
        //   2) 独立 ARAnchor —— 平面锚点失败时退到这里（注意 pending 可能一直为 true，建不上就落到 3）。
        //   3) 直接挂平面节点 —— 最后兜底。没有锚点参与，跟踪微调时方块会相对地面滑动。
        Transform parent = null;
        string kind = "none";

        if (plane != null && anchorManager != null)
        {
            var planeAnchor = anchorManager.AttachAnchor(plane, hit.pose);
            if (planeAnchor != null) { parent = planeAnchor.transform; kind = "plane-anchor"; }
            else kind = "plane-anchor-失败";
        }
        else if (plane == null) kind = "plane-null";

        if (parent == null)
        {
            var free = TryCreateAnchor(hit.pose);
            if (free != null) { parent = free; kind += "→anchor"; }
        }

        if (parent == null && plane != null) { parent = plane.transform; kind += "→plane-parent(未锚定)"; }
        if (parent == null) kind += "→none(未锚定)";

        var cube = Instantiate(cubePrefab, hit.pose.position, hit.pose.rotation, parent);
        cube.name = $"Marker_{m_Placed.Count + 1}";
        // 方块比地面高半个身位，避免和平面重叠导致闪烁
        cube.transform.position += Vector3.up * 0.05f;
        m_Placed.Add(cube);

        Debug.Log($"[placement] #{m_Placed.Count} at {hit.pose.position} anchorKind={kind} plane={(plane != null)}");

        if (form != null) form.Open(cube.transform.position, cube);
    }

    // ARRaycastManager 只会用「和它自己同一个 GameObject 上的 ARPlaneManager」去解析 hit.trackable，
    // 我们的场景里两个 manager 分开挂，所以 hit.trackable 一直是 null。这里自己按 trackableId 查回来。
    ARPlane ResolvePlane(ARRaycastHit hit)
    {
        if (hit.trackable is ARPlane direct) return direct;
        if (planeManager != null) return planeManager.GetPlane(hit.trackableId);
        return null;
    }

    // AR Foundation 5 的推荐建锚方式：把 ARAnchor 组件加在一个位于目标位姿的 GameObject 上。
    // pending == false 表示它已经真正进了 anchor 子系统，不是空壳。
    Transform TryCreateAnchor(Pose pose)
    {
        if (anchorManager == null) return null;

        var go = new GameObject("Marker Anchor");
        var xrOrigin = FindObjectOfType<Unity.XR.CoreUtils.XROrigin>();
        if (xrOrigin != null) go.transform.SetParent(xrOrigin.TrackablesParent, false);
        go.transform.SetPositionAndRotation(pose.position, pose.rotation);

        var anchor = go.AddComponent<ARAnchor>();
        if (anchor != null && !anchor.pending) return go.transform;

        Destroy(go);   // 没进子系统就是个普通空物体，留着没用
        return null;
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

    // ---------------------------------------------------------------
    // 锁屏 / 切后台回来时怎么办
    //
    // 实测：锁屏再解锁，平面和方块每次都漂 >30cm。原因是 ARCore 在失去焦点时暂停会话，
    // 恢复后重新定位，世界坐标系整体挪了——不是某个对象算错，所以修不回来。
    //
    // 处理原则：坐标系不可信时，不留一个"看起来对、其实错"的标记骗人。
    // 恢复后把已放置的全部清掉，并明确告诉用户重新放置——把沉默的失败变成响的。
    // ---------------------------------------------------------------

    bool m_HadMarkersBeforePause;

    void OnApplicationPause(bool paused)
    {
        if (paused)
        {
            // 只记住"进后台时有没有标记"，恢复时才决定要不要清
            m_HadMarkersBeforePause = m_Placed.Count > 0;
            return;
        }
        if (m_HadMarkersBeforePause) StartCoroutine(CoClearAfterResume());
    }

    System.Collections.IEnumerator CoClearAfterResume()
    {
        // 等会话重新进入跟踪（最多 5 秒）。不等的话，提示会贴在"正在初始化"的静止画面上。
        float waited = 0f;
        while (ARSession.state != ARSessionState.SessionTracking && waited < 5f)
        {
            waited += Time.unscaledDeltaTime;
            yield return null;
        }

        int before = m_Placed.Count;
        ClearAll();
        m_HadMarkersBeforePause = false;
        ARHUD.Notice($"环境已重新识别，{before} 个标记已清除，请重新放置");
        Debug.Log($"[placement] 恢复后清除 {before} 个标记（世界坐标已重定位，旧坐标不可信）");
    }
}
