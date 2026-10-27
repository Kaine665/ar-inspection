import { Link, NavLink, Outlet } from 'react-router-dom'
import HealthBadge from './HealthBadge'
import RequestLog from './RequestLog'
import type { UseHealthResult } from '../hooks/useHealth'

interface Props {
  health: UseHealthResult
}

/**
 * 最外层共享结构。两个页面都套这一层，切页只换中间的 <Outlet />。
 *
 *  ┌ 顶栏（品牌 + 导航 + health 徽章 + 刷新）—— 页面看不见，只由这里渲染
 *  ├ 内容区 <Outlet />
 *  └ 请求日志（核验用，可折叠）
 */
export default function AppShell({ health }: Props) {
  return (
    <>
      <header className="shell-header">
        <div className="shell-header-inner">
          <div className="brand">
            <Link to="/" style={{ textDecoration: 'none', color: 'inherit' }}>
              <span className="brand-title">AR 现场巡检 · 管理端</span>
            </Link>
            <span className="brand-sub">Unity 上报 → Go 存储 → 这里改状态</span>
            <nav className="nav">
              <NavLink to="/" end>
                问题列表
              </NavLink>
              <NavLink to="/system">契约与状态</NavLink>
            </nav>
          </div>
          <div className="header-right">
            <HealthBadge health={health} />
          </div>
        </div>
      </header>

      <main className="shell-main">
        <Outlet />
        <RequestLog />
      </main>
    </>
  )
}
