// 骨架阶段的占位入口：这一版只证明工程能起来、契约层能编译。
// 界面（AppShell / 列表页 / 抽屉 / 契约页）在下一个提交里接上，
// 那时这个文件会被换成真正的路由入口。
export default function App() {
  return (
    <main className="shell-main" style={{ paddingTop: 48 }}>
      <h1 className="page-title">admin 骨架就绪</h1>
      <p className="page-sub">
        契约类型、API 客户端、列表状态机、mock 后端已就位。界面在下一个提交里装上。
      </p>
      <div className="notice" style={{ maxWidth: 520 }}>
        已冻结的契约见仓库根 `docs/`；数据流转与核验方式见 `admin/DATAFLOW.md`。
      </div>
    </main>
  )
}
