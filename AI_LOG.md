# AI_LOG

作业交付要求中包含「AI 使用记录」。三条线各自往这里追加，按时间倒序（最新在最上面）。

## 记录格式

```
### YYYY-MM-DD HH:mm · <线：server / admin / ar-client>

**Prompt 原文**
> 原样粘贴，不要润色

**产出**
- 改了哪些文件、做了什么决定
- 与 AI 建议不一致的地方 + 为什么

**踩坑**
- 报错原文 / 怎么绕过的
```

---

## 记录

### 2026-10-27 22:24 · 全局 · 仓库初始化

**Prompt 原文**
> 我接下来要做三方分工。不过，好像还没建仓库。咱们直接建在mino工作区下的workspace吧。叫ar-device

**产出**
- 把既有骨架仓库（原 `mino/ar-inspection/`，含 1 个 commit）迁到 `mino/workspace/ar-device/`，git 历史保留
- README 补「三方分工」一节；新增本文件
