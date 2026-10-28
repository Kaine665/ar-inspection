using System.IO;
using UnityEditor;
using UnityEditor.Build.Reporting;
using UnityEngine;

// 命令行构建入口：Unity.exe -projectPath <proj> -executeMethod BuildScript.Build -quit -batchmode
public static class BuildScript
{
    const string ScenePath = "Assets/Scenes/Main.unity";
    const string OutApk = "Builds/app.apk";

    public static void Build()
    {
        Directory.CreateDirectory(Path.GetDirectoryName(Path.GetFullPath(OutApk)));

        EditorUserBuildSettings.SwitchActiveBuildTarget(BuildTargetGroup.Android, BuildTarget.Android);
        EditorUserBuildSettings.buildAppBundle = false;

        var options = new BuildPlayerOptions
        {
            scenes = new[] { ScenePath },
            locationPathName = OutApk,
            target = BuildTarget.Android,
            targetGroup = BuildTargetGroup.Android,
            options = BuildOptions.None
        };

        var report = BuildPipeline.BuildPlayer(options);
        var s = report.summary;
        Debug.Log($"BUILD_RESULT={s.result} size={s.totalSize} errors={s.totalErrors} time={s.totalTime}");

        if (s.result != BuildResult.Succeeded)
            EditorApplication.Exit(1);
    }
}
