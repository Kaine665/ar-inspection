# ar-client（Unity 手机端）

当前进度：**L1 · 真机平面检测**。能看到摄像头画面、能检出水平平面、HUD 显示会话状态与平面数量。

## 环境

| 项 | 值 |
|---|---|
| Unity | 2022.3.62f3（LTS） |
| AR Foundation | 5.1.6 · ARCore XR Plugin 5.1.6 · XR Plugin Management 4.7.0 |
| 图形 API | **OpenGLES3**（ARCore 不支持 Vulkan） |
| 脚本后端 | **IL2CPP + ARM64**（Mono 只出 32 位库，小米 15 是纯 64 位设备） |
| Android SDK | `D:\Android\Sdk`（platform 31–35） |
| JDK | `D:\Android\jdk`（11.0.14.1，Unity 2022.3 要求 11） |
| NDK | `D:\Android\Sdk\ndk\23.1.7779620` |

## 一键构建

```powershell
# 1) 装配工程（幂等：挂 ARCore loader、设 Android 参数、建场景与预制体）
& "C:\Program Files\Unity\Hub\Editor\2022.3.62f3\Editor\Unity.exe" -quit -batchmode -nographics `
  -projectPath "<repo>\ar-client" -executeMethod ProjectSetup.Setup `
  -logFile "<log>"

# 2) 出 APK（约 6–7 分钟，IL2CPP）
& "C:\Program Files\Unity\Hub\Editor\2022.3.62f3\Editor\Unity.exe" -quit -batchmode -nographics `
  -projectPath "<repo>\ar-client" -executeMethod BuildScript.Build `
  -logFile "<log>"

# 3) 装到真机
& "D:\Android\Sdk\platform-tools\adb.exe" -s <serial> install -r "<repo>\ar-client\Builds\app.apk"
```

> 若 Unity 是**由脚本/终端派生**启动的，包管理器可能起不来（`Could not connect to IPC stream "Upm-*"`）。
> 解法：用 WMI 派生进程，脱离调用方的作业对象。
> ```powershell
> Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{ CommandLine = '<上面那条命令>' }
> ```

## 场景结构（由 `ProjectSetup` 生成）

```
AR Session        ← ARSession + ARInputManager：会话生命周期
XR Origin         ← XROrigin：世界原点就定义在这里
└─ Camera Offset
   └─ Main Camera ← Camera + ARCameraManager + ARCameraBackground + TrackedPoseDriver
AR Plane Manager  ← ARPlaneManager（DetectionMode = Horizontal）
HUD               ← ARHUD：显示 Session 状态与平面数量
```

## 已知噪声（不影响构建）

- 构建日志里会出现一次 `arcoreimg failed ... TestReferenceImageLibrary.asset` 的异常，来源是 ARCore 包里自带的测试资源；
  构建结果仍是 `Succeeded`。
- ARCore 不支持 Vulkan，Graphic API 必须锁 OpenGLES3，否则构建直接失败。
