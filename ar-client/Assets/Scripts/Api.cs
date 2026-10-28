using System;
using System.Collections;
using System.Text;
using UnityEngine;
using UnityEngine.Networking;

// 三端契约的 C# 侧（对应 docs/api.md）。
// 字段名必须和 Go struct tag / TS type 一字不差，否则后端收到空值。
[Serializable]
public class PositionDto
{
    public float x;
    public float y;
    public float z;
}

[Serializable]
public class InspectionCreateDto
{
    public string title;
    public string description;
    public string priority;   // low / medium / high
    public PositionDto position;
}

[Serializable]
public class InspectionDto
{
    public string id;
    public string title;
    public string description;
    public string priority;
    public string status;
    public PositionDto position;
    public string createdAt;
    public string updatedAt;
}

[Serializable] public class ApiErrorBody { public ApiError error; }
[Serializable] public class ApiError { public string code; public string message; }

// 后端连通性。Unknown = 还没查过；DbDown = 服务活着但库不通（此时写入会失败）
public enum BackendPhase { Unknown, Checking, Ok, DbDown, Down }

[Serializable] public class HealthDto { public string status; public string db; public string version; }

public static class Api
{
    // 走 adb reverse：手机的 127.0.0.1:8080 被转发到电脑的 8080
    public const string BaseUrl = "http://127.0.0.1:8080";

    // 健康检查：只回答"后端活着吗、库通吗"，不碰业务数据（对应 GET /api/health）
    public static IEnumerator GetHealth(Action<HealthDto> onOk, Action<string> onErr)
    {
        using (var req = UnityWebRequest.Get(BaseUrl + "/api/health"))
        {
            req.timeout = 4;
            yield return req.SendWebRequest();

            if (req.result == UnityWebRequest.Result.Success)
            {
                HealthDto dto = null;
                try { dto = JsonUtility.FromJson<HealthDto>(req.downloadHandler.text); }
                catch (Exception e) { Debug.LogWarning("[api] health 响应不是预期的 JSON: " + e.Message); }
                onOk?.Invoke(dto);
                yield break;
            }
            onErr?.Invoke(req.error);
        }
    }

    public static IEnumerator CreateInspection(
        InspectionCreateDto body,
        Action<InspectionDto> onOk,
        Action<string, string> onErr)   // (code, message)
    {
        var json = JsonUtility.ToJson(body);
        var payload = Encoding.UTF8.GetBytes(json);

        using (var req = new UnityWebRequest(BaseUrl + "/api/inspections", "POST"))
        {
            req.uploadHandler = new UploadHandlerRaw(payload);
            req.downloadHandler = new DownloadHandlerBuffer();
            req.SetRequestHeader("Content-Type", "application/json");
            req.timeout = 10;

            yield return req.SendWebRequest();

            if (req.result == UnityWebRequest.Result.Success)
            {
                InspectionDto dto = null;
                try { dto = JsonUtility.FromJson<InspectionDto>(req.downloadHandler.text); }
                catch (Exception e) { Debug.LogWarning("[api] 响应不是预期的 JSON: " + e.Message); }
                onOk?.Invoke(dto);
                yield break;
            }

            // 失败分两种：后端讲了理由（契约错误体），或压根没连上
            string code = "network", msg = req.error;
            var text = req.downloadHandler != null ? req.downloadHandler.text : null;
            if (!string.IsNullOrEmpty(text))
            {
                try
                {
                    var err = JsonUtility.FromJson<ApiErrorBody>(text);
                    if (err?.error != null && !string.IsNullOrEmpty(err.error.code))
                    {
                        code = err.error.code;
                        msg = err.error.message;
                    }
                }
                catch { /* 非契约响应，保留网络错误信息 */ }
            }
            onErr?.Invoke(code, msg);
        }
    }
}
