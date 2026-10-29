using System;
using System.Linq;
using System.Reflection;
using UnityEditor;
using UnityEditor.Build;
using UnityEditor.SceneManagement;
using UnityEngine;
using UnityEngine.Rendering;
using UnityEngine.SceneManagement;
using UnityEngine.XR.ARFoundation;
using UnityEngine.XR.ARSubsystems;
using UnityEditor.XR.Management;
using UnityEditor.XR.Management.Metadata;

// 一键把"空项目"配成"能跑 AR 的项目"。
// 全部动作都是幂等的：重复执行只会覆盖，不会重复创建。
public static class ProjectSetup
{
    const string ScenePath  = "Assets/Scenes/Main.unity";
    const string MatPath    = "Assets/Materials/PlaneMat.mat";
    const string PrefabPath = "Assets/Prefabs/ARPlaneVisualizer.prefab";
    const string MarkerMatPath = "Assets/Materials/MarkerMat.mat";
    const string MarkerPrefabPath = "Assets/Prefabs/Marker.prefab";

    // 后端环境宏：有它 = 生产包，没有它 = 开发包（判定逻辑见 AppConfig.cs）
    const string ProdDefine = "AR_PROD";

    // ---------------------------------------------------------------
    // Android 工具链路径：先读系统环境变量，读不到才回退到本机默认位置。
    // 换一台机器只要设好 ANDROID_SDK_ROOT / ANDROID_NDK_ROOT / JAVA_HOME，代码一行不用改。
    // 注意这里不能写成 const —— const 在编译期就被替换成字面量，读不了运行期环境变量。
    // ---------------------------------------------------------------
    static string SdkPath => EnvOr("ANDROID_SDK_ROOT", EnvOr("ANDROID_HOME", @"D:\Android\Sdk"));
    static string JdkPath => EnvOr("JAVA_HOME", @"D:\Android\jdk");
    static string NdkPath => EnvOr("ANDROID_NDK_ROOT", System.IO.Path.Combine(SdkPath, @"ndk\23.1.7779620"));

    static string EnvOr(string key, string fallback)
    {
        var v = Environment.GetEnvironmentVariable(key);
        return string.IsNullOrEmpty(v) ? fallback : v;
    }

    [MenuItem("AR/1. Setup Project")]
    public static void Setup()
    {
        ConfigureExternalTools();
        ConfigurePlayer();
        ConfigureXR();
        var mat = CreatePlaneMaterial();
        var prefab = CreatePlanePrefab(mat);
        var marker = CreateMarkerPrefab();
        CreateScene(prefab, marker);
        AssetDatabase.SaveAssets();
        AssetDatabase.Refresh();
        Debug.Log("SETUP_OK");
    }

    // ---------------------------------------------------------------
    // 后端环境切换：决定打包出来的 App 连哪个地址（判定逻辑在 AppConfig.cs）
    //   DEV  = adb reverse，手机连电脑的 127.0.0.1:8080（演示、日常开发用这个）
    //   PROD = AppConfig 里的 HTTPS 正式地址
    // 实现方式是加/去编译宏 AR_PROD：生产包里连 dev 地址那几个字符都不会被打进去。
    // ---------------------------------------------------------------
    static bool HasProdDefine()
    {
        return PlayerSettings.GetScriptingDefineSymbols(NamedBuildTarget.Android)
            .Split(';').Any(s => s.Trim() == ProdDefine);
    }

    static void SetProdDefine(bool prod)
    {
        var kept = PlayerSettings.GetScriptingDefineSymbols(NamedBuildTarget.Android)
            .Split(';')
            .Select(s => s.Trim())
            .Where(s => s.Length > 0 && s != ProdDefine)
            .ToList();
        if (prod) kept.Add(ProdDefine);
        PlayerSettings.SetScriptingDefineSymbols(NamedBuildTarget.Android, string.Join(";", kept));
        AssetDatabase.SaveAssets();
        Debug.Log($"[setup] 后端环境 = {(prod ? "PROD" : "DEV")}（宏 {ProdDefine} {(prod ? "已加" : "已去")}），重新打包后生效");
    }

    const string DevMenu = "AR/2. Backend = DEV (adb reverse)";
    const string ProdMenu = "AR/3. Backend = PROD (HTTPS)";

    [MenuItem(DevMenu, false, 20)]
    static void UseDevBackend() => SetProdDefine(false);

    [MenuItem(DevMenu, true)]
    static bool UseDevBackendValidate()
    {
        Menu.SetChecked(DevMenu, !HasProdDefine());
        return true;
    }

    [MenuItem(ProdMenu, false, 21)]
    static void UseProdBackend() => SetProdDefine(true);

    [MenuItem(ProdMenu, true)]
    static bool UseProdBackendValidate()
    {
        Menu.SetChecked(ProdMenu, HasProdDefine());
        return true;
    }

    static void ConfigureExternalTools()
    {
        EditorPrefs.SetString("AndroidSdkRoot", SdkPath);
        EditorPrefs.SetString("JdkPath", JdkPath);
        if (System.IO.Directory.Exists(NdkPath)) EditorPrefs.SetString("AndroidNdkRoot", NdkPath);
        else Debug.LogWarning("[setup] NDK 目录不存在：" + NdkPath + "（设 ANDROID_NDK_ROOT 可覆盖）");
        Debug.Log($"[setup] sdk={SdkPath} jdk={JdkPath} ndk={NdkPath}");
    }

    static void ConfigurePlayer()
    {
        PlayerSettings.companyName = "Kaine";
        PlayerSettings.productName = "ARInspection";
        PlayerSettings.SetApplicationIdentifier(NamedBuildTarget.Android, "com.kaine.arinspection");
        PlayerSettings.Android.minSdkVersion  = AndroidSdkVersions.AndroidApiLevel24;
        PlayerSettings.Android.targetSdkVersion = AndroidSdkVersions.AndroidApiLevelAuto;
        // 小米 15 是纯 64 位设备（无 armeabi-v7a 运行时），Mono 只出 32 位库，所以必须 IL2CPP + ARM64
        PlayerSettings.SetScriptingBackend(NamedBuildTarget.Android, ScriptingImplementation.IL2CPP);
        PlayerSettings.Android.targetArchitectures = AndroidArchitecture.ARM64;
        PlayerSettings.defaultInterfaceOrientation = UIOrientation.LandscapeLeft;
        PlayerSettings.allowedAutorotateToPortrait = false;
        PlayerSettings.allowedAutorotateToPortraitUpsideDown = false;
        PlayerSettings.allowedAutorotateToLandscapeLeft = true;
        PlayerSettings.allowedAutorotateToLandscapeRight = true;

        // ARCore 不支持 Vulkan：强制 OpenGLES3，关掉自动图形 API
        // 真机走 http://127.0.0.1:8080（adb reverse），Android 9+ 默认禁明文，必须显式放行
        PlayerSettings.insecureHttpOption = InsecureHttpOption.AlwaysAllowed;

        PlayerSettings.SetUseDefaultGraphicsAPIs(BuildTarget.Android, false);
        PlayerSettings.SetGraphicsAPIs(BuildTarget.Android, new[] { UnityEngine.Rendering.GraphicsDeviceType.OpenGLES3 });

        // AR Foundation 5 依赖 Input System 包；后端同时打开新旧两套，避免输入被吞
        try
        {
            var assets = AssetDatabase.LoadAllAssetsAtPath("ProjectSettings/ProjectSettings.asset");
            if (assets != null && assets.Length > 0)
            {
                var so = new SerializedObject(assets[0]);
                var prop = so.FindProperty("activeInputHandler");
                if (prop != null) { prop.intValue = 2; so.ApplyModifiedProperties(); }
            }
        }
        catch (Exception e) { Debug.LogWarning("[setup] activeInputHandler 设置失败: " + e.Message); }
    }

    static void ConfigureXR()
    {
        // XRGeneralSettingsPerBuildTarget 没有公开的创建入口，用反射拿 GetOrCreate
        var type = Type.GetType("UnityEditor.XR.Management.XRGeneralSettingsPerBuildTarget, Unity.XR.Management.Editor");
        if (type == null) { Debug.LogError("[setup] 找不到 XRGeneralSettingsPerBuildTarget"); return; }

        var getOrCreate = type.GetMethod("GetOrCreate", BindingFlags.NonPublic | BindingFlags.Static);
        var bts = getOrCreate?.Invoke(null, null) as XRGeneralSettingsPerBuildTarget;
        if (bts == null) { Debug.LogError("[setup] 创建设置容器失败"); return; }

        if (!bts.HasSettingsForBuildTarget(BuildTargetGroup.Android))
            bts.CreateDefaultSettingsForBuildTarget(BuildTargetGroup.Android);

        var settings = bts.SettingsForBuildTarget(BuildTargetGroup.Android);
        if (settings == null) { Debug.LogError("[setup] Android 设置缺失"); return; }

        settings.InitManagerOnStart = true;

        // 不走 XRPackageMetadataStore.AssignLoader（它在无 GUI 的批处理里会内部 NRE），
        // 直接把 ARCoreLoader 资产塞进 manager 的 loader 列表。
        if (!bts.HasManagerSettingsForBuildTarget(BuildTargetGroup.Android))
        {
            bts.CreateDefaultManagerSettingsForBuildTarget(BuildTargetGroup.Android);
            AssetDatabase.SaveAssets();
        }
        settings = bts.SettingsForBuildTarget(BuildTargetGroup.Android);
        var manager = settings.Manager;
        if (manager == null) { Debug.LogError("[setup] XRManagerSettings 为 null"); return; }

        UnityEngine.XR.Management.XRLoader loader = null;
        foreach (var guid in AssetDatabase.FindAssets("ARCoreLoader t:XRLoader"))
        {
            var p = AssetDatabase.GUIDToAssetPath(guid);
            if (p.EndsWith("ARCoreLoader.asset")) { loader = AssetDatabase.LoadAssetAtPath<UnityEngine.XR.Management.XRLoader>(p); break; }
        }
        if (loader == null)
        {
            var loaderType = Type.GetType("UnityEngine.XR.ARCore.ARCoreLoader, Unity.XR.ARCore");
            if (loaderType != null)
            {
                EnsureFolder("Assets/XR/Loaders");
                loader = ScriptableObject.CreateInstance(loaderType) as UnityEngine.XR.Management.XRLoader;
                AssetDatabase.CreateAsset(loader, "Assets/XR/Loaders/ARCoreLoader.asset");
            }
        }
        if (loader == null) { Debug.LogError("[setup] 找不到 ARCoreLoader"); return; }

        bool added = manager.TryAddLoader(loader);
        EditorUtility.SetDirty(manager);
        EditorUtility.SetDirty(settings);
        AssetDatabase.SaveAssets();
        Debug.Log($"[setup] ARCoreLoader added={added} loaders={manager.activeLoaders.Count}");
    }

    static Material CreatePlaneMaterial()
    {
        EnsureFolder("Assets/Materials");
        Shader shader = Shader.Find("Sprites/Default");
        if (shader == null) shader = Shader.Find("Unlit/Color");
        if (shader == null) shader = Shader.Find("Standard");
        var mat = new Material(shader) { name = "PlaneMat" };
        mat.color = new Color(0.20f, 0.60f, 1.0f, 0.35f);
        AssetDatabase.DeleteAsset(MatPath);
        AssetDatabase.CreateAsset(mat, MatPath);
        return AssetDatabase.LoadAssetAtPath<Material>(MatPath);
    }

    static GameObject CreatePlanePrefab(Material mat)
    {
        EnsureFolder("Assets/Prefabs");
        var go = new GameObject("ARPlaneVisualizer");
        // ARPlaneMeshVisualizer 带 [RequireComponent(typeof(ARPlane))]，会自动补上 ARPlane
        go.AddComponent<ARPlaneMeshVisualizer>();
        go.AddComponent<MeshFilter>();
        var mr = go.AddComponent<MeshRenderer>();
        mr.sharedMaterial = mat;
        mr.shadowCastingMode = ShadowCastingMode.Off;
        mr.receiveShadows = false;

        AssetDatabase.DeleteAsset(PrefabPath);
        var prefab = PrefabUtility.SaveAsPrefabAsset(go, PrefabPath);
        UnityEngine.Object.DestroyImmediate(go);
        return prefab;
    }

    static GameObject CreateMarkerPrefab()
    {
        EnsureFolder("Assets/Materials");
        EnsureFolder("Assets/Prefabs");

        // 用 Unlit：AR 场景里没有光源（也没有阴影），Standard 会渲染成纯黑
        var shader = Shader.Find("Unlit/Color") ?? Shader.Find("Sprites/Default") ?? Shader.Find("Standard");
        var mat = new Material(shader) { name = "MarkerMat" };
        mat.color = new Color(1.0f, 0.45f, 0.10f);
        AssetDatabase.DeleteAsset(MarkerMatPath);
        AssetDatabase.CreateAsset(mat, MarkerMatPath);

        var go = GameObject.CreatePrimitive(PrimitiveType.Cube);
        go.name = "Marker";
        go.transform.localScale = Vector3.one * 0.12f;   // 12 cm 的方块，够小不挡路、够大看得见
        go.GetComponent<MeshRenderer>().sharedMaterial = AssetDatabase.LoadAssetAtPath<Material>(MarkerMatPath);

        AssetDatabase.DeleteAsset(MarkerPrefabPath);
        var loaded = PrefabUtility.SaveAsPrefabAsset(go, MarkerPrefabPath);
        UnityEngine.Object.DestroyImmediate(go);
        return loaded;
    }

    static void CreateScene(GameObject planePrefab, GameObject markerPrefab)
    {
        EnsureFolder("Assets/Scenes");
        var scene = EditorSceneManager.NewScene(NewSceneSetup.EmptyScene, NewSceneMode.Single);

        // 1. AR Session：会话生命周期 + 输入
        var session = new GameObject("AR Session");
        session.AddComponent<ARSession>();
        session.AddComponent<ARInputManager>();

        // 2. XR Origin：相机所在的世界原点（原点就在这里被定义）
        var origin = new GameObject("XR Origin");
        var xrOrigin = origin.AddComponent<Unity.XR.CoreUtils.XROrigin>();

        var offset = new GameObject("Camera Offset");
        offset.transform.SetParent(origin.transform, false);

        var camGO = new GameObject("Main Camera");
        camGO.tag = "MainCamera";
        camGO.transform.SetParent(offset.transform, false);
        var cam = camGO.AddComponent<Camera>();
        cam.clearFlags = CameraClearFlags.SolidColor;
        cam.backgroundColor = new Color(0f, 0f, 0f, 0f);
        cam.nearClipPlane = 0.05f;
        cam.farClipPlane = 100f;
        camGO.AddComponent<ARCameraManager>();
        camGO.AddComponent<ARCameraBackground>();

        // 相机位姿由设备跟踪驱动（用 legacy 版 TrackedPoseDriver，省掉 InputAction 接线）
        // 默认值已经是「Center / RotationAndPosition / UpdateAndBeforeRender」，不用改
        var tpd = camGO.AddComponent<UnityEngine.SpatialTracking.TrackedPoseDriver>();
        tpd.trackingType = UnityEngine.SpatialTracking.TrackedPoseDriver.TrackingType.RotationAndPosition;
        tpd.updateType   = UnityEngine.SpatialTracking.TrackedPoseDriver.UpdateType.UpdateAndBeforeRender;

        xrOrigin.Camera = cam;
        xrOrigin.CameraFloorOffsetObject = offset;

        // 3. 放置能力：射线命中 + 锚点 + 点击放置
        var mgrGO = new GameObject("AR Manager");
        var raycastManager = mgrGO.AddComponent<ARRaycastManager>();
        var anchorManager  = mgrGO.AddComponent<ARAnchorManager>();
        var placement      = mgrGO.AddComponent<ARPlacement>();
        placement.cubePrefab     = markerPrefab;
        placement.raycastManager = raycastManager;
        placement.anchorManager  = anchorManager;
        placement.arCamera       = cam;

        // 3. 平面检测：检出平面后按预制体生成可视化。
        //    注意：ARPlaneManager 必须和 ARRaycastManager 在同一个 GameObject 上——
        //    ARRaycastManager 内部用 GetComponent<ARPlaneManager>() 解析 hit.trackable，
        //    分开挂会导致 hit.trackable 永远是 null（锚点就挂不上）。
        var pm = mgrGO.AddComponent<ARPlaneManager>();
        pm.planePrefab = planePrefab;
        pm.detectionMode = PlaneDetectionMode.Horizontal;   // L1 只开水平面；L2 之后可改 Both
        placement.planeManager = pm;

        // 4. 屏幕上的调试信息（跟踪状态 + 平面数量）
        var hud = new GameObject("HUD");
        hud.AddComponent<ARHUD>();

        // 5. 上报表单（IMGUI：内置字体在 Android 上能正确显示中文）
        var formGO = new GameObject("Submit Form");
        var submitForm = formGO.AddComponent<SubmitForm>();
        submitForm.placement = placement;
        placement.form = submitForm;

        EditorSceneManager.MarkSceneDirty(scene);
        EditorSceneManager.SaveScene(scene, ScenePath);
        EditorBuildSettings.scenes = new[] { new EditorBuildSettingsScene(ScenePath, true) };
        Debug.Log("[setup] scene saved: " + ScenePath);
    }

    static void EnsureFolder(string path)
    {
        if (AssetDatabase.IsValidFolder(path)) return;
        var parent = System.IO.Path.GetDirectoryName(path).Replace('\\', '/');
        var leaf = System.IO.Path.GetFileName(path);
        if (!AssetDatabase.IsValidFolder(parent)) EnsureFolder(parent);
        AssetDatabase.CreateFolder(parent, leaf);
    }
}
