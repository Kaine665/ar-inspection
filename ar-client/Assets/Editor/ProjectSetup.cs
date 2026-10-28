using System;
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

    // 本机已有的 Android 工具链（Unity 自带的 SDK/NDK/OpenJDK 模块没装，改用外部）
    const string SdkPath = @"D:\Android\Sdk";
    const string JdkPath = @"D:\Android\jdk";   // JDK 11，2022.3 推荐的版本

    [MenuItem("AR/1. Setup Project")]
    public static void Setup()
    {
        ConfigureExternalTools();
        ConfigurePlayer();
        ConfigureXR();
        var mat = CreatePlaneMaterial();
        var prefab = CreatePlanePrefab(mat);
        CreateScene(prefab);
        AssetDatabase.SaveAssets();
        AssetDatabase.Refresh();
        Debug.Log("SETUP_OK");
    }

    static void ConfigureExternalTools()
    {
        EditorPrefs.SetString("AndroidSdkRoot", SdkPath);
        EditorPrefs.SetString("JdkPath", JdkPath);
        var ndkRoot = @"D:\Android\Sdk\ndk\23.1.7779620";
        if (System.IO.Directory.Exists(ndkRoot)) EditorPrefs.SetString("AndroidNdkRoot", ndkRoot);
        Debug.Log($"[setup] sdk={SdkPath} jdk={JdkPath}");
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

    static void CreateScene(GameObject planePrefab)
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

        // 3. 平面检测：检出平面后按预制体生成可视化
        var pmGO = new GameObject("AR Plane Manager");
        var pm = pmGO.AddComponent<ARPlaneManager>();
        pm.planePrefab = planePrefab;
        pm.detectionMode = PlaneDetectionMode.Horizontal;   // L1 只开水平面；L2 之后可改 Both

        // 4. 屏幕上的调试信息（跟踪状态 + 平面数量）
        var hud = new GameObject("HUD");
        hud.AddComponent<ARHUD>();

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
